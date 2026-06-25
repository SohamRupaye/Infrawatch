package health

import (
	"sync"
	"testing"
)

// helper: drive the FSM through a sequence of ok/fail inputs and return the
// final state along with the ordered slice of states visited on each *change*.
func drive(t *testing.T, inputs []bool) (State, []State) {
	t.Helper()
	s := NewServiceState("test-svc")
	var visited []State
	for _, ok := range inputs {
		newState, changed := s.Transition(ok, 0, "err")
		if changed {
			visited = append(visited, newState)
		}
	}
	return s.Current(), visited
}

// helper: drive and assert a specific final state.
func assertFinal(t *testing.T, inputs []bool, want State) {
	t.Helper()
	got, _ := drive(t, inputs)
	if got != want {
		t.Errorf("final state: got %s, want %s", got, want)
	}
}

// ── Fresh state ────────────────────────────────────────────────────────────

func TestNewServiceState_InitialState(t *testing.T) {
	s := NewServiceState("svc")
	if s.Current() != StateUnknown {
		t.Errorf("expected UNKNOWN, got %s", s.Current())
	}
}

// ── UNKNOWN → HEALTHY ──────────────────────────────────────────────────────

func TestFSM_UnknownToHealthy(t *testing.T) {
	assertFinal(t, []bool{true}, StateHealthy)
}

func TestFSM_UnknownFirstFailGoesDegraded(t *testing.T) {
	assertFinal(t, []bool{false}, StateDegraded)
}

// ── HEALTHY path ───────────────────────────────────────────────────────────

func TestFSM_HealthyFirstFail_Degraded(t *testing.T) {
	assertFinal(t, []bool{true, false}, StateDegraded)
}

func TestFSM_HealthyStaysHealthy(t *testing.T) {
	assertFinal(t, []bool{true, true, true, true, true}, StateHealthy)
}

// ── DEGRADED paths ─────────────────────────────────────────────────────────

func TestFSM_DegradedOneSuccessStaysDegraded(t *testing.T) {
	// fail → DEGRADED, single success → still DEGRADED (needs 2 consecutive)
	assertFinal(t, []bool{true, false, true}, StateDegraded)
}

func TestFSM_DegradedTwoSuccessesRecoverToHealthy(t *testing.T) {
	assertFinal(t, []bool{true, false, true, true}, StateHealthy)
}

func TestFSM_DegradedThreeFailsGoesUnhealthy(t *testing.T) {
	// HEALTHY → fail (DEGRADED) → fail (DEGRADED) → fail (UNHEALTHY)
	assertFinal(t, []bool{true, false, false, false}, StateUnhealthy)
}

func TestFSM_DegradedTwoFailsStaysDegraded(t *testing.T) {
	// The 3rd failure triggers UNHEALTHY; 2 fails in DEGRADED should stay there.
	// UNKNOWN→HEALTHY, HEALTHY→DEGRADED (1 fail), DEGRADED stays (2nd fail)
	assertFinal(t, []bool{true, false, false}, StateDegraded)
}

// ── UNHEALTHY paths ────────────────────────────────────────────────────────

func TestFSM_UnhealthyFirstSuccessGoesRecovering(t *testing.T) {
	// 3 fails → UNHEALTHY, then success → RECOVERING
	assertFinal(t, []bool{true, false, false, false, true}, StateRecovering)
}

func TestFSM_UnhealthyFiveFailsGoesDead(t *testing.T) {
	// HEALTHY → DEGRADED (1) → DEGRADED (2) → UNHEALTHY (3) → still (4) → DEAD (5)
	assertFinal(t, []bool{true, false, false, false, false, false}, StateDead)
}

func TestFSM_UnhealthyFourFailsStaysUnhealthy(t *testing.T) {
	// 4 total failures: DEGRADED after 1, UNHEALTHY after 3, still UNHEALTHY at 4
	assertFinal(t, []bool{true, false, false, false, false}, StateUnhealthy)
}

// ── RECOVERING paths ───────────────────────────────────────────────────────

func TestFSM_RecoveringThreeSuccessesGoesHealthy(t *testing.T) {
	// HEALTHY → DEGRADED → UNHEALTHY → RECOVERING → RECOVERING → HEALTHY
	assertFinal(t, []bool{true, false, false, false, true, true, true}, StateHealthy)
}

func TestFSM_RecoveringOneSuccessStaysRecovering(t *testing.T) {
	assertFinal(t, []bool{true, false, false, false, true}, StateRecovering)
}

func TestFSM_RecoveringTwoSuccessStaysRecovering(t *testing.T) {
	assertFinal(t, []bool{true, false, false, false, true, true}, StateRecovering)
}

func TestFSM_RecoveringTwoFailsGoesBackUnhealthy(t *testing.T) {
	// Re-lapse during recovery: 2 fails → UNHEALTHY
	assertFinal(t, []bool{true, false, false, false, true, false, false}, StateUnhealthy)
}

func TestFSM_RecoveringOneFailStaysRecovering(t *testing.T) {
	assertFinal(t, []bool{true, false, false, false, true, false}, StateRecovering)
}

// ── DEAD paths ─────────────────────────────────────────────────────────────

func TestFSM_DeadIsSticky(t *testing.T) {
	// Once DEAD, failures keep it DEAD (no auto-transition).
	final, _ := drive(t, []bool{true, false, false, false, false, false})
	if final != StateDead {
		t.Fatalf("expected DEAD, got %s", final)
	}
	// More failures should not change state
	s := NewServiceState("svc")
	for _, ok := range []bool{true, false, false, false, false, false} {
		s.Transition(ok, 0, "err") //nolint:errcheck
	}
	for i := 0; i < 5; i++ {
		_, changed := s.Transition(false, 0, "err")
		if changed {
			t.Errorf("expected no state change from DEAD on failure, iteration %d", i)
		}
	}
}

func TestFSM_DeadFirstSuccessGoesRecovering(t *testing.T) {
	s := NewServiceState("svc")
	// Reach DEAD
	for _, ok := range []bool{true, false, false, false, false, false} {
		s.Transition(ok, 0, "err") //nolint:errcheck
	}
	if s.Current() != StateDead {
		t.Fatalf("precondition: expected DEAD, got %s", s.Current())
	}
	newState, changed := s.Transition(true, 0, "")
	if !changed {
		t.Error("expected state change from DEAD on success")
	}
	if newState != StateRecovering {
		t.Errorf("expected RECOVERING from DEAD+success, got %s", newState)
	}
}

// ── Transition history bookkeeping ────────────────────────────────────────

func TestFSM_TransitionHistoryRecorded(t *testing.T) {
	s := NewServiceState("svc")
	s.Transition(true, 10, "")   // UNKNOWN → HEALTHY
	s.Transition(false, 0, "e")  // HEALTHY → DEGRADED
	snap := s.Snapshot()
	if len(snap.Transitions) != 2 {
		t.Errorf("expected 2 transitions, got %d", len(snap.Transitions))
	}
	if snap.Transitions[0].From != StateUnknown || snap.Transitions[0].To != StateHealthy {
		t.Errorf("unexpected first transition: %+v", snap.Transitions[0])
	}
	if snap.Transitions[1].From != StateHealthy || snap.Transitions[1].To != StateDegraded {
		t.Errorf("unexpected second transition: %+v", snap.Transitions[1])
	}
}

func TestFSM_TransitionHistoryBoundedAt500(t *testing.T) {
	s := NewServiceState("svc")
	// Oscillate HEALTHY ↔ DEGRADED to generate many transitions fast
	s.Transition(true, 0, "")
	for i := 0; i < 600; i++ {
		// One fail takes HEALTHY→DEGRADED; two successes take DEGRADED→HEALTHY
		s.Transition(false, 0, "err")
		s.Transition(true, 0, "")
		s.Transition(true, 0, "")
	}
	snap := s.Snapshot()
	if len(snap.Transitions) > 500 {
		t.Errorf("transition history should be bounded at 500, got %d", len(snap.Transitions))
	}
}

// ── Uptime percentage ─────────────────────────────────────────────────────

func TestFSM_UptimePct_AllHealthy(t *testing.T) {
	s := NewServiceState("svc")
	for i := 0; i < 10; i++ {
		s.Transition(true, 0, "")
	}
	snap := s.Snapshot()
	if snap.UptimePct != 100.0 {
		t.Errorf("expected 100%% uptime, got %.2f", snap.UptimePct)
	}
}

func TestFSM_UptimePct_HalfHealthy(t *testing.T) {
	s := NewServiceState("svc")
	for i := 0; i < 5; i++ {
		s.Transition(true, 0, "")
	}
	for i := 0; i < 5; i++ {
		s.Transition(false, 0, "err")
	}
	snap := s.Snapshot()
	if snap.UptimePct != 50.0 {
		t.Errorf("expected 50%% uptime, got %.2f", snap.UptimePct)
	}
}

func TestFSM_UptimePct_NoChecks(t *testing.T) {
	s := NewServiceState("svc")
	snap := s.Snapshot()
	if snap.UptimePct != 100.0 {
		t.Errorf("expected 100%% when no checks have run, got %.2f", snap.UptimePct)
	}
}

// ── No spurious change ────────────────────────────────────────────────────

func TestFSM_NoChangeReportedWhenStateUnchanged(t *testing.T) {
	s := NewServiceState("svc")
	s.Transition(true, 0, "") // → HEALTHY
	_, changed := s.Transition(true, 0, "")
	if changed {
		t.Error("expected no state change when HEALTHY receives another success")
	}
}

// ── PreviousState tracking ────────────────────────────────────────────────

func TestFSM_PreviousStateTracked(t *testing.T) {
	s := NewServiceState("svc")
	s.Transition(true, 0, "")  // UNKNOWN → HEALTHY
	s.Transition(false, 0, "") // HEALTHY → DEGRADED
	snap := s.Snapshot()
	if snap.PreviousState != StateHealthy {
		t.Errorf("expected PreviousState=HEALTHY, got %s", snap.PreviousState)
	}
	if snap.CurrentState != StateDegraded {
		t.Errorf("expected CurrentState=DEGRADED, got %s", snap.CurrentState)
	}
}

// ── Concurrency safety ────────────────────────────────────────────────────

func TestFSM_ConcurrentTransitions(t *testing.T) {
	s := NewServiceState("svc")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		ok := i%2 == 0
		go func(ok bool) {
			defer wg.Done()
			s.Transition(ok, 1, "err")
		}(ok)
	}
	wg.Wait()
	// Simply assert it doesn't panic and returns a valid state
	state := s.Current()
	valid := map[State]bool{
		StateUnknown: true, StateHealthy: true, StateDegraded: true,
		StateUnhealthy: true, StateDead: true, StateRecovering: true,
	}
	if !valid[state] {
		t.Errorf("got invalid state after concurrent transitions: %q", state)
	}
}

// ── Full happy path ───────────────────────────────────────────────────────

func TestFSM_FullRecoveryPath(t *testing.T) {
	// Simulates: start → healthy → outage → dead → recovery → healthy
	type step struct {
		ok   bool
		want State
	}
	steps := []step{
		{true, StateHealthy},   // UNKNOWN → HEALTHY
		{true, StateHealthy},   // stay
		{false, StateDegraded}, // HEALTHY → DEGRADED (1 fail)
		{false, StateDegraded}, // stay
		{false, StateUnhealthy}, // DEGRADED → UNHEALTHY (3 fails)
		{false, StateUnhealthy}, // stay (4 fails)
		{false, StateDead},      // UNHEALTHY → DEAD (5 fails)
		{true, StateRecovering}, // DEAD → RECOVERING
		{true, StateRecovering}, // stay (2 OK)
		{true, StateHealthy},    // RECOVERING → HEALTHY (3 OK)
	}

	s := NewServiceState("svc")
	for i, st := range steps {
		newState, _ := s.Transition(st.ok, 0, "err")
		if newState != st.want {
			t.Errorf("step %d (ok=%v): got %s, want %s", i, st.ok, newState, st.want)
		}
	}
}
