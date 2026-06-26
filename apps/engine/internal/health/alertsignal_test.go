package health

import (
	"context"
	"testing"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
	"go.uber.org/zap"
)

// ── Alert floor + FSM ratchet ───────────────────────────────────────────────
//
// A passive service's "OK" input is derived from whether an alert floor is
// active (poller.go: OK = !floorActive), then fed through the exact same
// ServiceState.Transition used by real HTTP checks. These tests drive that
// same Transition function directly to prove the floor mechanism inherits
// the FSM's ratchet behavior instead of forcing instant state jumps.

func TestServiceState_FloorRatchet_FiringDegradesGradually(t *testing.T) {
	s := NewServiceState("passive-svc")

	// Simulates: alert fires -> floor active -> each tick feeds OK=false.
	assertRatchet := func(ok bool, wantState State) {
		t.Helper()
		newState, _ := s.Transition(ok, 0, "external alert: firing")
		if newState != wantState {
			t.Fatalf("after Transition(ok=%v): got %s, want %s", ok, newState, wantState)
		}
	}

	assertRatchet(false, StateDegraded)  // 1st failure
	assertRatchet(false, StateDegraded)  // 2nd failure
	assertRatchet(false, StateUnhealthy) // 3rd failure
	assertRatchet(false, StateUnhealthy) // 4th failure
	assertRatchet(false, StateDead)      // 5th failure
}

func TestServiceState_FloorRatchet_ResolvedRecoversGradually_NotInstantly(t *testing.T) {
	s := NewServiceState("passive-svc")

	// Drive to DEAD first (5 consecutive failures), mirroring a firing alert.
	for i := 0; i < 5; i++ {
		s.Transition(false, 0, "external alert: firing")
	}
	if s.Current() != StateDead {
		t.Fatalf("setup: expected DEAD before testing recovery, got %s", s.Current())
	}

	// A resolved alert clears the floor, but does NOT itself flip the state —
	// poller.go just lets the normal ticking loop re-evaluate with OK=true.
	// The FSM requires DEAD -> RECOVERING -> (3 consecutive OK) -> HEALTHY;
	// a single OK must NOT jump straight to HEALTHY.
	newState, changed := s.Transition(true, 0, "")
	if !changed || newState != StateRecovering {
		t.Fatalf("after 1st OK post-DEAD: got %s (changed=%v), want RECOVERING", newState, changed)
	}
	if s.Current() == StateHealthy {
		t.Fatalf("recovered to HEALTHY after a single OK tick — floor clearing must not force instant recovery")
	}

	newState, _ = s.Transition(true, 0, "")
	if newState != StateRecovering {
		t.Fatalf("after 2nd OK: got %s, want still RECOVERING (needs 3 consecutive)", newState)
	}

	newState, _ = s.Transition(true, 0, "")
	if newState != StateHealthy {
		t.Fatalf("after 3rd consecutive OK: got %s, want HEALTHY", newState)
	}
}

func TestServiceState_Floor_SetClearRoundTrip(t *testing.T) {
	s := NewServiceState("svc")

	if active, _ := s.Floor(); active {
		t.Fatal("new ServiceState should not have an active floor")
	}

	s.SetFloor("firing: disk full")
	active, reason := s.Floor()
	if !active || reason != "firing: disk full" {
		t.Fatalf("after SetFloor: got active=%v reason=%q", active, reason)
	}

	s.ClearFloor()
	active, reason = s.Floor()
	if active || reason != "" {
		t.Fatalf("after ClearFloor: got active=%v reason=%q, want false/\"\"", active, reason)
	}
}

// ── StateManager alert floor plumbing ───────────────────────────────────────

func TestStateManager_AlertFloor_GetOrCreate(t *testing.T) {
	sm := NewStateManager(nil, nil, nil, nil, nil, nil, zap.NewNop())

	// FloorStatus on a never-seen service must not panic and must report inactive.
	if active, _ := sm.FloorStatus("unseen-svc"); active {
		t.Fatal("expected no active floor for a never-seen service")
	}

	sm.SetAlertFloor("svc-a", "firing: cpu high")
	active, reason := sm.FloorStatus("svc-a")
	if !active || reason != "firing: cpu high" {
		t.Fatalf("after SetAlertFloor: got active=%v reason=%q", active, reason)
	}

	sm.ClearAlertFloor("svc-a")
	active, _ = sm.FloorStatus("svc-a")
	if active {
		t.Fatal("expected floor cleared after ClearAlertFloor")
	}
}

// ── AlertSignalHandler routing/validation ───────────────────────────────────
//
// Only the rejection paths are exercised here without a live Redis-backed
// Bus: an accepted signal calls Poller.PollNow -> poll() -> bus.PublishCircuitState,
// which needs a real *events.Bus. Full happy-path coverage would require an
// integration test against Redis, consistent with poller.go/state_manager.go
// having no such coverage elsewhere in this package either.

func newTestPoller(t *testing.T, services []config.ServiceConfig) *Poller {
	t.Helper()
	cfg := &config.Config{Services: services}
	return NewPoller(cfg, nil, nil, nil, nil, zap.NewNop())
}

func TestAlertSignalHandler_Handle_RejectsUnknownService(t *testing.T) {
	poller := newTestPoller(t, nil)
	sm := NewStateManager(nil, nil, nil, nil, nil, nil, zap.NewNop())
	h := NewAlertSignalHandler(poller, sm, zap.NewNop())

	h.Handle(context.Background(), AlertSignal{ServiceName: "ghost", Status: "firing"})

	if active, _ := sm.FloorStatus("ghost"); active {
		t.Fatal("signal for an unknown service must be dropped, not applied")
	}
}

func TestAlertSignalHandler_Handle_RejectsActiveModeService(t *testing.T) {
	poller := newTestPoller(t, []config.ServiceConfig{
		{Name: "active-svc", Mode: config.ModeActive, URL: "http://example.invalid"},
	})
	sm := NewStateManager(nil, nil, nil, nil, nil, nil, zap.NewNop())
	h := NewAlertSignalHandler(poller, sm, zap.NewNop())

	h.Handle(context.Background(), AlertSignal{ServiceName: "active-svc", Status: "firing"})

	if active, _ := sm.FloorStatus("active-svc"); active {
		t.Fatal("signal for a non-passive (active) service must be dropped, not applied")
	}
}
