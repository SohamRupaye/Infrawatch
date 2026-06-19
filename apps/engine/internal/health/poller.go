package health

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/anomaly"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/circuit"
	"github.com/SohamRupaye/infrawatch/apps/engine/internal/events"
	"go.uber.org/zap"
)

// Poller drives concurrent health checks for all configured services.
type Poller struct {
	cfg      *config.Config
	stateMgr *StateManager
	cb       *circuit.Registry
	anomaly  *anomaly.Detector
	bus      *events.Bus
	checker  *Checker
	logger   *zap.Logger

	stopChans map[string]chan struct{}
	wg        sync.WaitGroup
	mu        sync.Mutex
}

// NewPoller constructs a Poller.
func NewPoller(
	cfg *config.Config,
	stateMgr *StateManager,
	cb *circuit.Registry,
	anomalyDetector *anomaly.Detector,
	bus *events.Bus,
	logger *zap.Logger,
) *Poller {
	return &Poller{
		cfg:       cfg,
		stateMgr:  stateMgr,
		cb:        cb,
		anomaly:   anomalyDetector,
		bus:       bus,
		checker:   NewChecker(),
		logger:    logger,
		stopChans: make(map[string]chan struct{}),
	}
}

// Start launches one goroutine per service. Each goroutine fires on its own
// tick interval independently — no service can block another.
func (p *Poller) Start(ctx context.Context) {
	for _, svc := range p.cfg.Services {
		p.UpsertService(ctx, svc)
	}
	p.logger.Sugar().Infow("poller started", "services", len(p.cfg.Services))
}

// Stop signals all service goroutines and waits for them to drain.
func (p *Poller) Stop(ctx context.Context) {
	p.mu.Lock()
	for _, ch := range p.stopChans {
		close(ch)
	}
	p.stopChans = make(map[string]chan struct{})
	p.mu.Unlock()

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info("poller: all goroutines stopped cleanly")
	case <-ctx.Done():
		p.logger.Warn("poller: shutdown timed out, some goroutines may still be running")
	}
}

// UpsertService starts or restarts the polling loop for a service and updates
// the in-memory config list used by the running engine.
func (p *Poller) UpsertService(ctx context.Context, svc config.ServiceConfig) {
	p.mu.Lock()
	if old, ok := p.stopChans[svc.Name]; ok {
		close(old)
		delete(p.stopChans, svc.Name)
	}

	stopCh := make(chan struct{})
	p.stopChans[svc.Name] = stopCh

	updated := false
	for i := range p.cfg.Services {
		if p.cfg.Services[i].Name == svc.Name {
			p.cfg.Services[i] = svc
			updated = true
			break
		}
	}
	if !updated {
		p.cfg.Services = append(p.cfg.Services, svc)
	}
	p.mu.Unlock()

	p.wg.Add(1)
	go p.runServiceLoop(ctx, svc, stopCh)
	p.logger.Sugar().Infow("poller service upserted", "service", svc.Name, "updated", updated)
}

// RemoveService stops polling for a service and removes it from active config.
func (p *Poller) RemoveService(name string) {
	p.mu.Lock()
	if stopCh, ok := p.stopChans[name]; ok {
		close(stopCh)
		delete(p.stopChans, name)
	}
	filtered := make([]config.ServiceConfig, 0, len(p.cfg.Services))
	for _, svc := range p.cfg.Services {
		if svc.Name != name {
			filtered = append(filtered, svc)
		}
	}
	p.cfg.Services = filtered
	p.mu.Unlock()
	p.logger.Sugar().Infow("poller service removed", "service", name)
}

// runServiceLoop is the per-service polling goroutine.
func (p *Poller) runServiceLoop(ctx context.Context, svc config.ServiceConfig, stopCh <-chan struct{}) {
	defer p.wg.Done()

	interval := svc.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	// Stagger initial checks slightly to avoid thundering herd at startup
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run an immediate first check
	p.poll(ctx, svc)

	for {
		select {
		case <-ticker.C:
			p.poll(ctx, svc)
		case <-stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// poll executes one check cycle for a service.
func (p *Poller) poll(ctx context.Context, svc config.ServiceConfig) {
	sugar := p.logger.Sugar()

	// If the circuit is OPEN, skip the real check and return synthetic failure
	breaker := p.cb.Get(svc.Name)
	if breaker.IsOpen() {
		sugar.Debugw("circuit open, skipping check", "service", svc.Name)
		p.stateMgr.Record(ctx, svc, CheckResult{
			ServiceName: svc.Name,
			OK:          false,
			Error:       circuit.ErrCircuitOpen,
			Timestamp:   time.Now(),
		})
		return
	}

	result := p.checker.Check(ctx, svc)

	// If the service's own check passed, verify all declared dependencies are
	// healthy.  A service that depends on a DEAD or UNHEALTHY upstream should
	// be considered degraded even if it responds to its own health probe —
	// because it cannot serve real traffic without the dependency.
	if result.OK {
		if reason := p.dependencyFailureReason(svc); reason != "" {
			sugar.Debugw("dependency unhealthy, overriding check result",
				"service", svc.Name,
				"reason", reason,
			)
			result.OK = false
			result.Error = fmt.Errorf("dependency failure: %s", reason)
		}
	}

	// Feed result into circuit breaker
	if result.OK {
		breaker.RecordSuccess()
	} else {
		breaker.RecordFailure()
	}

	// Feed latency into anomaly detector
	p.anomaly.RecordLatency(svc.Name, result.ResponseTime)

	// Let the state manager apply FSM transitions, alerts, and healing
	p.stateMgr.Record(ctx, svc, result)

	if result.Error != nil {
		sugar.Debugw("health check failed",
			"service", svc.Name,
			"error", result.Error,
			"response_time_ms", result.ResponseTime.Milliseconds(),
		)
	} else {
		sugar.Debugw("health check ok",
			"service", svc.Name,
			"status", result.StatusCode,
			"response_time_ms", result.ResponseTime.Milliseconds(),
		)
	}
}

// dependencyFailureReason returns a non-empty string if any declared dependency
// of svc is currently DEAD or UNHEALTHY.  Returns "" when all dependencies are
// healthy or not yet observed (unknown dependencies do not block the check).
func (p *Poller) dependencyFailureReason(svc config.ServiceConfig) string {
	for _, dep := range svc.Dependencies {
		snap, ok := p.stateMgr.GetSnapshot(dep)
		if !ok {
			// Dependency not yet polled — don't penalise the dependent service.
			continue
		}
		if snap.CurrentState == StateDead || snap.CurrentState == StateUnhealthy {
			return fmt.Sprintf("%q is %s", dep, snap.CurrentState)
		}
	}
	return ""
}
