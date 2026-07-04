// Package store provides a Redis-backed read model for the Infrawatch API.
// The engine writes all state to Redis Streams; this store subscribes to
// those streams and maintains an in-memory cache the REST handlers query.
//
// On startup with TimescaleDB available, call Initialize before Run to
// pre-populate current service state from the DB.  Initialize also sets the
// Redis subscription to start from "$" (new events only) so we don't replay
// history that the DB already owns.  Without a DB, Run falls back to replaying
// from "0" as before.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	enginepkg "github.com/SohamRupaye/infrawatch/apps/engine/pkg"
	"go.uber.org/zap"
)

// ServiceView is the combined read model for one service.
type ServiceView struct {
	Name             string                    `json:"name"`
	Tags             []string                  `json:"tags,omitempty"`
	State            string                    `json:"state"`
	PreviousState    string                    `json:"previous_state"`
	LastChecked      time.Time                 `json:"last_checked"`
	LastHealthy      time.Time                 `json:"last_healthy"`
	ResponseTimeMs   int64                     `json:"response_time_ms"`
	ErrorMessage     string                    `json:"error_message,omitempty"`
	ConsecutiveFails int                       `json:"consecutive_fails"`
	UptimePct        float64                   `json:"uptime_pct"`
	Circuit          enginepkg.BreakerSnapshot `json:"circuit"`
	FallbackURL      string                    `json:"fallback_url,omitempty"`
}

// MetricPoint is one time-series observation kept in the in-memory ring buffer.
// Historical data beyond the ring is served from TimescaleDB by the handlers.
type MetricPoint struct {
	Timestamp      time.Time `json:"timestamp"`
	OK             bool      `json:"ok"`
	ResponseTimeMs int64     `json:"response_time_ms"`
	StatusCode     int       `json:"status_code"`
	State          string    `json:"state"`
}

// StateStore builds and maintains an in-memory read model from Redis Streams.
// It is the fast path for current-state queries (dashboard node graph, circuit
// breaker status).  Historical queries go to TimescaleDB via DBReader.
type StateStore struct {
	mu       sync.RWMutex
	services map[string]*ServiceView
	metrics  map[string][]MetricPoint
	circuits map[string]enginepkg.BreakerSnapshot
	logger   *zap.Logger

	// redisStartID controls where the Redis subscription begins.
	//   "0"  — replay the entire stream history (no DB, cold start)
	//   "$"  — only new events from now (DB has loaded historical state)
	redisStartID string
}

const maxPoints = 2000

// NewStateStore creates an empty StateStore.  The Redis subscription will
// replay from the beginning of the stream unless Initialize is called first.
func NewStateStore(logger *zap.Logger) *StateStore {
	return &StateStore{
		services:     make(map[string]*ServiceView),
		metrics:      make(map[string][]MetricPoint),
		circuits:     make(map[string]enginepkg.BreakerSnapshot),
		logger:       logger,
		redisStartID: "0", // default: replay all history from Redis
	}
}

// Initialize loads the last known state per service from TimescaleDB and
// pre-populates the in-memory store so the dashboard shows correct data
// immediately after a restart — no need to wait for the next Redis event.
//
// After calling Initialize the Redis subscription will start from "$" (current
// tail) instead of "0", because DB already owns the historical record.
//
// It is safe to call Initialize more than once (e.g. after a re-connection),
// but it must be called before Run to take effect on the start ID.
func (s *StateStore) Initialize(ctx context.Context, db *DBReader) error {
	if db == nil {
		s.logger.Warn("store: Initialize called with nil DBReader — skipping DB seeding")
		return nil
	}

	states, err := db.GetLastStatesPerService(ctx)
	if err != nil {
		return fmt.Errorf("store: loading last states from DB: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, row := range states {
		svc, ok := s.services[row.Service]
		if !ok {
			svc = &ServiceView{Name: row.Service}
			s.services[row.Service] = svc
		}
		svc.State = row.ToState
		svc.PreviousState = row.FromState
		svc.LastChecked = row.Time
		// ErrorMessage isn't stored in state_changes but Reason is close enough
		// for the restart snapshot.
		svc.ErrorMessage = row.Reason
	}

	// From this point forward, only subscribe to new Redis events.
	// Historical state is owned by the DB.
	s.redisStartID = "$"

	s.logger.Sugar().Infow("store: seeded from TimescaleDB",
		"services", len(states),
		"redis_start", s.redisStartID,
	)
	return nil
}

// Run subscribes to the Redis streams and keeps the store up to date.
// Blocks until ctx is done.
//
// Call Initialize before Run when TimescaleDB is available so that:
//   - The store starts with accurate state (not empty)
//   - The Redis subscription begins at "$" rather than replaying all history
func (s *StateStore) Run(ctx context.Context, bus *enginepkg.Bus) {
	startID := s.redisStartID // read once before any goroutine launches

	go bus.Subscribe(ctx, enginepkg.StreamStateChange, startID, func(_ string, payload []byte) {
		var evt enginepkg.StateChangeEvent
		if err := json.Unmarshal(payload, &evt); err != nil {
			s.logger.Sugar().Warnw("store: bad state_change payload", "error", err)
			return
		}
		s.applyStateChange(evt)
	})

	go bus.Subscribe(ctx, enginepkg.StreamMetrics, startID, func(_ string, payload []byte) {
		var evt enginepkg.MetricEvent
		if err := json.Unmarshal(payload, &evt); err != nil {
			return
		}
		s.applyMetric(evt)
	})

	<-ctx.Done()
}

func (s *StateStore) applyStateChange(evt enginepkg.StateChangeEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	svc, ok := s.services[evt.ServiceName]
	if !ok {
		svc = &ServiceView{Name: evt.ServiceName}
		s.services[evt.ServiceName] = svc
	}
	svc.State = evt.NewState
	svc.PreviousState = evt.PreviousState

	// Re-hydrate rich fields from the embedded snapshot via JSON round-trip.
	if evt.Snapshot != nil {
		data, err := json.Marshal(evt.Snapshot)
		if err == nil {
			var snap enginepkg.ServiceStateSnapshot
			if err := json.Unmarshal(data, &snap); err == nil {
				svc.LastChecked = snap.LastChecked
				svc.LastHealthy = snap.LastHealthy
				svc.ResponseTimeMs = snap.ResponseTimeMs
				svc.ErrorMessage = snap.ErrorMessage
				svc.ConsecutiveFails = snap.ConsecutiveFails
				svc.UptimePct = snap.UptimePct
			}
		}
	}
}

func (s *StateStore) applyMetric(evt enginepkg.MetricEvent) {
	pt := MetricPoint{
		Timestamp:      evt.Timestamp,
		OK:             evt.OK,
		ResponseTimeMs: evt.ResponseTimeMs,
		StatusCode:     evt.StatusCode,
		State:          evt.State,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pts := append(s.metrics[evt.ServiceName], pt)
	if len(pts) > maxPoints {
		pts = pts[len(pts)-maxPoints:]
	}
	s.metrics[evt.ServiceName] = pts

	if svc, ok := s.services[evt.ServiceName]; ok {
		svc.ResponseTimeMs = evt.ResponseTimeMs
		svc.LastChecked = evt.Timestamp
	} else {
		s.services[evt.ServiceName] = &ServiceView{
			Name:           evt.ServiceName,
			State:          evt.State,
			ResponseTimeMs: evt.ResponseTimeMs,
			LastChecked:    evt.Timestamp,
		}
	}
}

// UpdateCircuit stores a fresh circuit breaker snapshot.
func (s *StateStore) UpdateCircuit(name string, snap enginepkg.BreakerSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.circuits[name] = snap
	if svc, ok := s.services[name]; ok {
		svc.Circuit = snap
	}
}

// AllServices returns a snapshot of all known services.
func (s *StateStore) AllServices() []ServiceView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ServiceView, 0, len(s.services))
	for _, svc := range s.services {
		out = append(out, *svc)
	}
	return out
}

// GetService returns the view for one service.
func (s *StateStore) GetService(name string) (ServiceView, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	svc, ok := s.services[name]
	if !ok {
		return ServiceView{}, false
	}
	return *svc, true
}

// MetricsFor returns the raw metric ring for a service.
// For long-range queries use DBReader.QueryMetrics instead.
func (s *StateStore) MetricsFor(name string) []MetricPoint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pts := s.metrics[name]
	out := make([]MetricPoint, len(pts))
	copy(out, pts)
	return out
}

// AllCircuits returns all circuit breaker snapshots.
func (s *StateStore) AllCircuits() map[string]enginepkg.BreakerSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]enginepkg.BreakerSnapshot, len(s.circuits))
	for k, v := range s.circuits {
		out[k] = v
	}
	return out
}
