package health

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/alerts"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/anomaly"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/circuit"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/events"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/healing"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/storage"
	"go.uber.org/zap"
)

// StateManager owns the per-service FSMs and routes transitions to
// the alert dispatcher, healer, event bus, and persistent storage.
type StateManager struct {
	mu      sync.RWMutex
	states  map[string]*ServiceState
	bus     *events.Bus
	alerts  *alerts.Dispatcher
	healer  *healing.Healer
	anomaly *anomaly.Detector
	cb      *circuit.Registry
	db      *storage.DB // nil when TimescaleDB is not configured
	logger  *zap.Logger

	// Track healing attempts per service to enforce cooldowns
	healingAttempts map[string]time.Time
}

// NewStateManager creates a StateManager.
// db may be nil — persistence is skipped gracefully when not configured.
func NewStateManager(
	bus *events.Bus,
	alertDispatcher *alerts.Dispatcher,
	healer *healing.Healer,
	anomalyDetector *anomaly.Detector,
	cbRegistry *circuit.Registry,
	db *storage.DB,
	logger *zap.Logger,
) *StateManager {
	return &StateManager{
		states:          make(map[string]*ServiceState),
		bus:             bus,
		alerts:          alertDispatcher,
		healer:          healer,
		anomaly:         anomalyDetector,
		cb:              cbRegistry,
		db:              db,
		logger:          logger,
		healingAttempts: make(map[string]time.Time),
	}
}

// Record processes a CheckResult, runs the FSM, persists metrics and state
// changes to TimescaleDB, emits Redis events, fires alerts, and triggers
// healing when needed.
func (sm *StateManager) Record(ctx context.Context, svc config.ServiceConfig, result CheckResult) {
	sm.mu.Lock()
	state, ok := sm.states[svc.Name]
	if !ok {
		state = NewServiceState(svc.Name)
		sm.states[svc.Name] = state
	}
	sm.mu.Unlock()

	errMsg := ""
	if result.Error != nil {
		errMsg = result.Error.Error()
	}

	newState, changed := state.Transition(result.OK, result.ResponseTime.Milliseconds(), errMsg)

	// ── Persist metric to TimescaleDB (async, fire-and-forget) ────────────────
	// This runs for every poll regardless of state change.  DB latency must
	// never block the polling goroutine, so WriteMetric spawns its own goroutine.
	if sm.db != nil {
		sm.db.WriteMetric(storage.MetricRow{
			Time:       result.Timestamp,
			Service:    svc.Name,
			StatusCode: result.StatusCode,
			LatencyMs:  float64(result.ResponseTime.Milliseconds()),
			IsHealthy:  result.OK,
			Error:      errMsg,
		})
	}

	// ── Publish metric event to Redis (real-time fan-out) ─────────────────────
	sm.bus.PublishMetric(ctx, events.MetricEvent{
		ServiceName:    svc.Name,
		Timestamp:      result.Timestamp,
		OK:             result.OK,
		ResponseTimeMs: result.ResponseTime.Milliseconds(),
		StatusCode:     result.StatusCode,
		State:          string(newState),
	})

	if !changed {
		return
	}

	sm.logger.Sugar().Infow("state transition",
		"service", svc.Name,
		"from", state.PreviousState,
		"to", newState,
	)

	// ── Persist state change + incident logic to TimescaleDB (async) ──────────
	// WriteStateChange internally handles incident open/close so the two writes
	// stay causally consistent inside a single goroutine.
	if sm.db != nil {
		sm.db.WriteStateChange(storage.StateChangeRow{
			Service:   svc.Name,
			FromState: string(state.PreviousState),
			ToState:   string(newState),
			Reason:    errMsg,
		})
	}

	// ── Publish state-change event to Redis (picked up by API / dashboard) ────
	snap := state.Snapshot()
	sm.bus.PublishStateChange(ctx, events.StateChangeEvent{
		ServiceName:   svc.Name,
		PreviousState: string(state.PreviousState),
		NewState:      string(newState),
		Reason:        snap.ErrorMessage,
		Timestamp:     time.Now(),
		Snapshot:      snap,
	})

	// ── Fire alerts for the new state ─────────────────────────────────────────
	sm.alerts.Dispatch(ctx, alerts.Alert{
		ServiceName:    svc.Name,
		State:          string(newState),
		PreviousState:  string(state.PreviousState),
		Message:        alertMessage(svc.Name, newState, snap),
		ResponseTimeMs: result.ResponseTime.Milliseconds(),
		Timestamp:      time.Now(),
	})

	// ── Trigger healing if the service is DEAD ────────────────────────────────
	if newState == StateDead {
		sm.maybeTriggerHealing(ctx, svc, snap)
	}

	// ── Clear the fallback reroute once the service is healthy again ──────────
	if newState == StateHealthy {
		healing.ClearFallback(svc.Name)
	}
}

// maybeTriggerHealing fires healing actions unless a cooldown is active.
func (sm *StateManager) maybeTriggerHealing(ctx context.Context, svc config.ServiceConfig, _ ServiceStateSnapshot) {
	sm.mu.Lock()
	lastAttempt, exists := sm.healingAttempts[svc.Name]
	sm.mu.Unlock()

	if exists && time.Since(lastAttempt) < sm.healer.Cooldown() {
		sm.logger.Sugar().Infow("healing cooldown active, skipping", "service", svc.Name)
		return
	}

	sm.mu.Lock()
	sm.healingAttempts[svc.Name] = time.Now()
	sm.mu.Unlock()

	go func() {
		result := sm.healer.Heal(ctx, svc)
		sm.logger.Sugar().Infow("healing attempt completed",
			"service", svc.Name,
			"action", result.Action,
			"success", result.Success,
			"error", result.Error,
		)
		sm.bus.PublishHealingEvent(ctx, events.HealingEvent{
			ServiceName: svc.Name,
			Action:      result.Action,
			Success:     result.Success,
			Error:       result.ErrorString(),
			Timestamp:   time.Now(),
		})
	}()
}

// GetSnapshot returns the current state snapshot for a service.
func (sm *StateManager) GetSnapshot(name string) (ServiceStateSnapshot, bool) {
	sm.mu.RLock()
	state, ok := sm.states[name]
	sm.mu.RUnlock()
	if !ok {
		return ServiceStateSnapshot{}, false
	}
	return state.Snapshot(), true
}

// AllSnapshots returns snapshots for every known service.
func (sm *StateManager) AllSnapshots() []ServiceStateSnapshot {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	snaps := make([]ServiceStateSnapshot, 0, len(sm.states))
	for _, s := range sm.states {
		snaps = append(snaps, s.Snapshot())
	}
	return snaps
}

func alertMessage(name string, state State, snap ServiceStateSnapshot) string {
	switch state {
	case StateDead:
		return fmt.Sprintf("🔴 *%s* is DEAD — %d consecutive failures, last response %dms",
			name, snap.ConsecutiveFails, snap.ResponseTimeMs)
	case StateUnhealthy:
		return fmt.Sprintf("🟠 *%s* is UNHEALTHY — %d consecutive failures",
			name, snap.ConsecutiveFails)
	case StateDegraded:
		return fmt.Sprintf("🟡 *%s* is DEGRADED — first failure detected",
			name)
	case StateRecovering:
		return fmt.Sprintf("🔵 *%s* is RECOVERING — first successful response after outage",
			name)
	case StateHealthy:
		return fmt.Sprintf("🟢 *%s* recovered to HEALTHY",
			name)
	}
	return fmt.Sprintf("%s transitioned to %s", name, state)
}
