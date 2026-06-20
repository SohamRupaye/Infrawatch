package circuit

import (
	"sync"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
	"go.uber.org/zap"
)

// Registry holds a Breaker for every service, keyed by service name.
type Registry struct {
	mu       sync.RWMutex
	breakers map[string]*Breaker
	cfg      config.CircuitConfig
	logger   *zap.Logger
}

// NewRegistry creates a Registry pre-populated from config.
func NewRegistry(cfg config.CircuitConfig, logger *zap.Logger) *Registry {
	return &Registry{
		breakers: make(map[string]*Breaker),
		cfg:      cfg,
		logger:   logger,
	}
}

// Get returns the Breaker for serviceName, creating one lazily if needed.
func (r *Registry) Get(serviceName string) *Breaker {
	r.mu.RLock()
	b, ok := r.breakers[serviceName]
	r.mu.RUnlock()
	if ok {
		return b
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Double-check after acquiring write lock
	if b, ok = r.breakers[serviceName]; ok {
		return b
	}
	b = NewBreaker(BreakerConfig{
		FailureThreshold: r.cfg.FailureThreshold,
		SuccessThreshold: r.cfg.SuccessThreshold,
		Timeout:          r.cfg.Timeout,
	})
	r.breakers[serviceName] = b
	return b
}

// Reset resets the circuit for a given service (called via REST API).
func (r *Registry) Reset(serviceName string) bool {
	r.mu.RLock()
	b, ok := r.breakers[serviceName]
	r.mu.RUnlock()
	if !ok {
		return false
	}
	b.Reset()
	r.logger.Sugar().Infow("circuit reset", "service", serviceName)
	return true
}

// AllSnapshots returns snapshots of every tracked breaker.
func (r *Registry) AllSnapshots() map[string]BreakerSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]BreakerSnapshot, len(r.breakers))
	for name, b := range r.breakers {
		out[name] = b.Snapshot()
	}
	return out
}
