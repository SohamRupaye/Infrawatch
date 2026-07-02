package circuit

import (
	"sync"
	"testing"
	"time"
)

// cfg returns a test BreakerConfig with sensible, fast-failing values.
func cfg(failThresh, successThresh int, timeout time.Duration) BreakerConfig {
	return BreakerConfig{
		FailureThreshold: failThresh,
		SuccessThreshold: successThresh,
		Timeout:          timeout,
	}
}

// ── Initial state ──────────────────────────────────────────────────────────

func TestBreaker_InitialState(t *testing.T) {
	b := NewBreaker(cfg(3, 1, 30*time.Second))
	if b.State() != StateClosed {
		t.Errorf("expected CLOSED on creation, got %s", b.State())
	}
	if b.IsOpen() {
		t.Error("IsOpen should be false when CLOSED")
	}
}

// ── CLOSED → OPEN ──────────────────────────────────────────────────────────

func TestBreaker_ClosedOpensAfterThreshold(t *testing.T) {
	b := NewBreaker(cfg(3, 1, 30*time.Second))
	b.RecordFailure()
	b.RecordFailure()
	if b.State() != StateClosed {
		t.Errorf("should still be CLOSED after 2 failures (threshold=3), got %s", b.State())
	}
	b.RecordFailure() // 3rd → trip
	if b.State() != StateOpen {
		t.Errorf("expected OPEN after 3 failures, got %s", b.State())
	}
}

func TestBreaker_ClosedOpensExactlyAtThreshold(t *testing.T) {
	b := NewBreaker(cfg(1, 1, 30*time.Second))
	b.RecordFailure()
	if b.State() != StateOpen {
		t.Errorf("expected OPEN after 1 failure with threshold=1, got %s", b.State())
	}
}

func TestBreaker_SuccessResetsFailureCounter(t *testing.T) {
	b := NewBreaker(cfg(3, 1, 30*time.Second))
	b.RecordFailure()
	b.RecordFailure()
	b.RecordSuccess() // resets consecutive fails
	b.RecordFailure() // counter back to 1 (not 3)
	if b.State() != StateClosed {
		t.Errorf("expected CLOSED — success should have reset the failure counter, got %s", b.State())
	}
}

func TestBreaker_IsOpenReturnsTrueWhenOpen(t *testing.T) {
	b := NewBreaker(cfg(1, 1, 30*time.Second))
	b.RecordFailure()
	if !b.IsOpen() {
		t.Error("IsOpen should return true when OPEN and timeout not elapsed")
	}
}

// ── OPEN → HALF_OPEN ───────────────────────────────────────────────────────

func TestBreaker_OpenTransitionsToHalfOpenAfterTimeout(t *testing.T) {
	b := NewBreaker(cfg(1, 1, 10*time.Millisecond))
	b.RecordFailure() // → OPEN
	time.Sleep(20 * time.Millisecond)
	// IsOpen should promote to HALF_OPEN and return false (probe allowed)
	if b.IsOpen() {
		t.Error("IsOpen should return false (allow probe) after timeout elapses")
	}
	if b.State() != StateHalfOpen {
		t.Errorf("expected HALF_OPEN after timeout, got %s", b.State())
	}
}

func TestBreaker_OpenStaysOpenBeforeTimeout(t *testing.T) {
	b := NewBreaker(cfg(1, 1, 10*time.Second))
	b.RecordFailure() // → OPEN
	if !b.IsOpen() {
		t.Error("IsOpen should be true before timeout")
	}
	if b.State() != StateOpen {
		t.Errorf("expected OPEN, got %s", b.State())
	}
}

// ── HALF_OPEN → CLOSED ─────────────────────────────────────────────────────

func TestBreaker_HalfOpenSuccessCloses(t *testing.T) {
	b := NewBreaker(cfg(1, 1, 10*time.Millisecond))
	b.RecordFailure() // → OPEN
	time.Sleep(20 * time.Millisecond)
	b.IsOpen()        // promotes to HALF_OPEN
	b.RecordSuccess() // 1 success, threshold=1 → CLOSED
	if b.State() != StateClosed {
		t.Errorf("expected CLOSED after success in HALF_OPEN, got %s", b.State())
	}
}

func TestBreaker_HalfOpenMultipleSuccessesNeeded(t *testing.T) {
	b := NewBreaker(cfg(1, 3, 10*time.Millisecond)) // successThreshold=3
	b.RecordFailure()
	time.Sleep(20 * time.Millisecond)
	b.IsOpen() // → HALF_OPEN
	b.RecordSuccess()
	if b.State() != StateHalfOpen {
		t.Errorf("expected HALF_OPEN after 1/3 successes, got %s", b.State())
	}
	b.RecordSuccess()
	if b.State() != StateHalfOpen {
		t.Errorf("expected HALF_OPEN after 2/3 successes, got %s", b.State())
	}
	b.RecordSuccess()
	if b.State() != StateClosed {
		t.Errorf("expected CLOSED after 3/3 successes, got %s", b.State())
	}
}

// ── HALF_OPEN → OPEN ───────────────────────────────────────────────────────

func TestBreaker_HalfOpenSingleFailureReopens(t *testing.T) {
	b := NewBreaker(cfg(1, 2, 10*time.Millisecond))
	b.RecordFailure()
	time.Sleep(20 * time.Millisecond)
	b.IsOpen() // → HALF_OPEN
	b.RecordFailure()
	if b.State() != StateOpen {
		t.Errorf("expected OPEN after failure in HALF_OPEN, got %s", b.State())
	}
}

// ── Manual Reset ───────────────────────────────────────────────────────────

func TestBreaker_ResetFromOpen(t *testing.T) {
	b := NewBreaker(cfg(1, 1, 30*time.Second))
	b.RecordFailure() // → OPEN
	b.Reset()
	if b.State() != StateClosed {
		t.Errorf("expected CLOSED after Reset, got %s", b.State())
	}
	if b.IsOpen() {
		t.Error("IsOpen should be false after Reset")
	}
}

func TestBreaker_ResetClearsCounters(t *testing.T) {
	b := NewBreaker(cfg(3, 1, 30*time.Second))
	b.RecordFailure()
	b.RecordFailure()
	b.Reset()
	// Should take another 3 failures to open again
	b.RecordFailure()
	b.RecordFailure()
	if b.State() != StateClosed {
		t.Errorf("expected CLOSED — Reset should have cleared failure counter, got %s", b.State())
	}
	b.RecordFailure()
	if b.State() != StateOpen {
		t.Errorf("expected OPEN after 3 post-reset failures, got %s", b.State())
	}
}

// ── Snapshot ───────────────────────────────────────────────────────────────

func TestBreaker_SnapshotReflectsState(t *testing.T) {
	b := NewBreaker(cfg(3, 1, 30*time.Second))
	b.RecordFailure()
	b.RecordFailure()
	snap := b.Snapshot()
	if snap.State != StateClosed {
		t.Errorf("snapshot state: expected CLOSED, got %s", snap.State)
	}
	if snap.ConsecutiveFails != 2 {
		t.Errorf("snapshot consecutive_fails: expected 2, got %d", snap.ConsecutiveFails)
	}
}

func TestBreaker_SnapshotAfterOpen(t *testing.T) {
	b := NewBreaker(cfg(1, 1, 30*time.Second))
	b.RecordFailure()
	snap := b.Snapshot()
	if snap.State != StateOpen {
		t.Errorf("expected snapshot state=OPEN, got %s", snap.State)
	}
	if snap.OpenedAt.IsZero() {
		t.Error("OpenedAt should be set when OPEN")
	}
}

// ── Full lifecycle ─────────────────────────────────────────────────────────

func TestBreaker_FullLifecycle(t *testing.T) {
	b := NewBreaker(cfg(3, 2, 10*time.Millisecond))

	// Healthy phase
	for i := 0; i < 5; i++ {
		b.RecordSuccess()
	}
	if b.State() != StateClosed {
		t.Fatalf("expected CLOSED after all successes, got %s", b.State())
	}

	// Trip the breaker
	b.RecordFailure()
	b.RecordFailure()
	b.RecordFailure()
	if b.State() != StateOpen {
		t.Fatalf("expected OPEN after 3 failures, got %s", b.State())
	}
	if !b.IsOpen() {
		t.Fatal("IsOpen should return true immediately after opening")
	}

	// Wait for timeout
	time.Sleep(20 * time.Millisecond)
	if b.IsOpen() {
		t.Fatal("IsOpen should allow probe after timeout")
	}
	if b.State() != StateHalfOpen {
		t.Fatalf("expected HALF_OPEN, got %s", b.State())
	}

	// Successful recovery
	b.RecordSuccess()
	b.RecordSuccess() // successThreshold=2
	if b.State() != StateClosed {
		t.Fatalf("expected CLOSED after recovery, got %s", b.State())
	}
}

// ── Concurrency safety ────────────────────────────────────────────────────

func TestBreaker_ConcurrentAccess(t *testing.T) {
	b := NewBreaker(cfg(5, 1, 5*time.Millisecond))
	var wg sync.WaitGroup

	// Simultaneous failures and successes from many goroutines
	for i := 0; i < 200; i++ {
		wg.Add(1)
		isSuccess := i%3 == 0
		go func(isSuccess bool) {
			defer wg.Done()
			if isSuccess {
				b.RecordSuccess()
			} else {
				b.RecordFailure()
			}
			b.IsOpen()
			b.State()
			b.Snapshot()
		}(isSuccess)
	}

	wg.Wait()
	// Just assert no panic and state is one of the valid values
	s := b.State()
	valid := map[BreakerState]bool{
		StateClosed: true, StateOpen: true, StateHalfOpen: true,
	}
	if !valid[s] {
		t.Errorf("got invalid breaker state after concurrent access: %q", s)
	}
}
