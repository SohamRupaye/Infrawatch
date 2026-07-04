package circuit

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned when a circuit is open and the check is skipped.
var ErrCircuitOpen = errors.New("circuit breaker is OPEN — check skipped")

// BreakerState represents the three states of a circuit breaker.
type BreakerState string

const (
	StateClosed   BreakerState = "CLOSED"
	StateOpen     BreakerState = "OPEN"
	StateHalfOpen BreakerState = "HALF_OPEN"
)

// BreakerConfig holds configuration for a single breaker.
type BreakerConfig struct {
	FailureThreshold int           // consecutive failures before opening
	SuccessThreshold int           // consecutive successes in half-open before closing
	Timeout          time.Duration // how long to wait before probing (half-open)
}

// Breaker is a thread-safe circuit breaker for one service.
type Breaker struct {
	mu               sync.Mutex
	cfg              BreakerConfig
	state            BreakerState
	consecutiveFails int
	consecutiveOK    int
	openedAt         time.Time
	lastTransition   time.Time
}

// NewBreaker creates a Breaker in CLOSED state.
func NewBreaker(cfg BreakerConfig) *Breaker {
	return &Breaker{
		cfg:   cfg,
		state: StateClosed,
	}
}

// IsOpen returns true when the circuit is fully open (real checks should be skipped).
func (b *Breaker) IsOpen() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == StateOpen {
		// Check if it's time to try half-open
		if time.Since(b.openedAt) >= b.cfg.Timeout {
			b.transitionTo(StateHalfOpen)
			return false // allow one probe through
		}
		return true
	}
	return false
}

// RecordSuccess feeds a successful check into the breaker.
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.consecutiveFails = 0
	b.consecutiveOK++

	switch b.state {
	case StateHalfOpen:
		if b.consecutiveOK >= b.cfg.SuccessThreshold {
			b.transitionTo(StateClosed)
		}
	case StateOpen:
		// Shouldn't happen normally (IsOpen blocks calls) but handle gracefully
		b.transitionTo(StateHalfOpen)
	}
}

// RecordFailure feeds a failed check into the breaker.
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.consecutiveOK = 0
	b.consecutiveFails++

	switch b.state {
	case StateClosed:
		if b.consecutiveFails >= b.cfg.FailureThreshold {
			b.transitionTo(StateOpen)
		}
	case StateHalfOpen:
		// Single failure in half-open re-opens immediately
		b.transitionTo(StateOpen)
	}
}

// Reset forces the breaker back to CLOSED (used by the REST API).
func (b *Breaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.consecutiveFails = 0
	b.consecutiveOK = 0
	b.transitionTo(StateClosed)
}

// State returns the current breaker state.
func (b *Breaker) State() BreakerState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Snapshot returns a read-only view of the breaker.
func (b *Breaker) Snapshot() BreakerSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return BreakerSnapshot{
		State:            b.state,
		ConsecutiveFails: b.consecutiveFails,
		OpenedAt:         b.openedAt,
		LastTransition:   b.lastTransition,
	}
}

// transitionTo changes state (must be called with lock held).
func (b *Breaker) transitionTo(next BreakerState) {
	b.state = next
	b.lastTransition = time.Now()
	if next == StateOpen {
		b.openedAt = time.Now()
		b.consecutiveOK = 0
	}
	if next == StateClosed {
		b.consecutiveFails = 0
	}
}

// BreakerSnapshot is a read-only view of a Breaker.
type BreakerSnapshot struct {
	State            BreakerState `json:"state"`
	ConsecutiveFails int          `json:"consecutive_fails"`
	OpenedAt         time.Time    `json:"opened_at,omitempty"`
	LastTransition   time.Time    `json:"last_transition"`
}
