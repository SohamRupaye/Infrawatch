package health

import (
	"sync"
	"time"
)

// State represents a service's health state in the FSM.
type State string

const (
	StateHealthy    State = "HEALTHY"
	StateDegraded   State = "DEGRADED"
	StateUnhealthy  State = "UNHEALTHY"
	StateDead       State = "DEAD"
	StateRecovering State = "RECOVERING"
	StateUnknown    State = "UNKNOWN"
)

// Transition records a single state change event.
type Transition struct {
	From      State     `json:"from"`
	To        State     `json:"to"`
	Reason    string    `json:"reason"`
	Timestamp time.Time `json:"timestamp"`
}

// ServiceState holds runtime state for one service.
type ServiceState struct {
	mu sync.RWMutex

	Name             string
	CurrentState     State
	PreviousState    State
	LastChecked      time.Time
	LastHealthy      time.Time
	ConsecutiveFails int
	ConsecutiveOK    int
	ResponseTimeMs   int64
	ErrorMessage     string
	Transitions      []Transition

	// Counters used by the FSM
	totalChecks int
	totalErrors int
}

// NewServiceState creates a new ServiceState for the given service name.
func NewServiceState(name string) *ServiceState {
	return &ServiceState{
		Name:         name,
		CurrentState: StateUnknown,
		Transitions:  make([]Transition, 0, 32),
	}
}

// Current returns the current state (thread-safe read).
func (s *ServiceState) Current() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.CurrentState
}

// Snapshot returns a copy of the state safe to pass around.
func (s *ServiceState) Snapshot() ServiceStateSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	transitions := make([]Transition, len(s.Transitions))
	copy(transitions, s.Transitions)
	return ServiceStateSnapshot{
		Name:             s.Name,
		CurrentState:     s.CurrentState,
		PreviousState:    s.PreviousState,
		LastChecked:      s.LastChecked,
		LastHealthy:      s.LastHealthy,
		ConsecutiveFails: s.ConsecutiveFails,
		ResponseTimeMs:   s.ResponseTimeMs,
		ErrorMessage:     s.ErrorMessage,
		Transitions:      transitions,
		UptimePct:        s.uptimePct(),
	}
}

// uptimePct returns the rolling uptime percentage (must be called with lock held).
func (s *ServiceState) uptimePct() float64 {
	if s.totalChecks == 0 {
		return 100.0
	}
	return float64(s.totalChecks-s.totalErrors) / float64(s.totalChecks) * 100.0
}

// Transition applies the state machine logic given a poll result.
// Returns the new state and whether a state change occurred.
func (s *ServiceState) Transition(ok bool, responseMs int64, errMsg string) (newState State, changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalChecks++
	s.LastChecked = time.Now()
	s.ResponseTimeMs = responseMs
	s.ErrorMessage = errMsg

	if !ok {
		s.totalErrors++
		s.ConsecutiveFails++
		s.ConsecutiveOK = 0
	} else {
		s.ConsecutiveFails = 0
		s.ConsecutiveOK++
		s.LastHealthy = time.Now()
	}

	next := s.computeNextState(ok)
	if next == s.CurrentState {
		return s.CurrentState, false
	}

	t := Transition{
		From:      s.CurrentState,
		To:        next,
		Reason:    transitionReason(s.CurrentState, next, ok, errMsg),
		Timestamp: time.Now(),
	}
	s.Transitions = append(s.Transitions, t)

	// Keep transition history bounded
	if len(s.Transitions) > 500 {
		s.Transitions = s.Transitions[len(s.Transitions)-500:]
	}

	s.PreviousState = s.CurrentState
	s.CurrentState = next
	return next, true
}

// computeNextState encodes the FSM transition table.
// Must be called with lock held.
func (s *ServiceState) computeNextState(ok bool) State {
	fails := s.ConsecutiveFails
	consOK := s.ConsecutiveOK
	cur := s.CurrentState

	if ok {
		switch cur {
		case StateUnknown, StateHealthy:
			return StateHealthy
		case StateDegraded:
			if consOK >= 2 {
				return StateHealthy
			}
			return StateDegraded
		case StateUnhealthy, StateDead:
			return StateRecovering
		case StateRecovering:
			if consOK >= 3 {
				return StateHealthy
			}
			return StateRecovering
		}
		return StateHealthy
	}

	// Not OK
	switch cur {
	case StateUnknown, StateHealthy:
		if fails >= 1 {
			return StateDegraded
		}
	case StateDegraded:
		if fails >= 3 {
			return StateUnhealthy
		}
		return StateDegraded
	case StateUnhealthy:
		if fails >= 5 {
			return StateDead
		}
		return StateUnhealthy
	case StateDead:
		return StateDead
	case StateRecovering:
		// Relapsed during recovery
		if fails >= 2 {
			return StateUnhealthy
		}
		return StateRecovering
	}

	if fails >= 5 {
		return StateDead
	}
	if fails >= 3 {
		return StateUnhealthy
	}
	if fails >= 1 {
		return StateDegraded
	}
	return StateHealthy
}

// ServiceStateSnapshot is a read-only view of a service's state.
type ServiceStateSnapshot struct {
	Name             string       `json:"name"`
	CurrentState     State        `json:"current_state"`
	PreviousState    State        `json:"previous_state"`
	LastChecked      time.Time    `json:"last_checked"`
	LastHealthy      time.Time    `json:"last_healthy"`
	ConsecutiveFails int          `json:"consecutive_fails"`
	ResponseTimeMs   int64        `json:"response_time_ms"`
	ErrorMessage     string       `json:"error_message,omitempty"`
	Transitions      []Transition `json:"transitions"`
	UptimePct        float64      `json:"uptime_pct"`
}

func transitionReason(from, to State, ok bool, errMsg string) string {
	if ok {
		switch to {
		case StateHealthy:
			return "consecutive successful checks"
		case StateRecovering:
			return "first successful check after outage"
		}
	}
	if errMsg != "" {
		return errMsg
	}
	switch to {
	case StateDegraded:
		return "first health check failure"
	case StateUnhealthy:
		return "3 consecutive failures"
	case StateDead:
		return "5 consecutive failures"
	}
	return "unknown"
}
