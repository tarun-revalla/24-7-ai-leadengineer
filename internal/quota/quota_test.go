package quota

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fixedClock returns a controllable time source.
type fixedClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock(t time.Time) *fixedClock { return &fixedClock{now: t} }

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fixedClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// recordingSleeper captures requested waits instead of performing them.
type recordingSleeper struct {
	mu    sync.Mutex
	waits []time.Duration
	err   error
}

func (s *recordingSleeper) sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waits = append(s.waits, d)
	return s.err
}

func (s *recordingSleeper) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.waits)
}

func (s *recordingSleeper) last() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.waits) == 0 {
		return 0
	}
	return s.waits[len(s.waits)-1]
}

var epoch = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func newManager(t *testing.T, opts ...Option) (*Manager, *fixedClock, *recordingSleeper) {
	t.Helper()

	clock := newClock(epoch)
	sleeper := &recordingSleeper{}

	all := append([]Option{
		WithClock(clock.Now),
		WithSleeper(sleeper.sleep),
	}, opts...)

	m, err := New(t.TempDir(), all...)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	return m, clock, sleeper
}

func TestNewRejectsEmptyDir(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("empty directory must be rejected")
	}
}

func TestFreshStateIsAvailable(t *testing.T) {
	m, _, _ := newManager(t)
	ctx := context.Background()

	available, remaining := m.Available(ctx)
	if !available {
		t.Error("a project with no recorded limit must be available")
	}
	if remaining != 0 {
		t.Errorf("remaining: got %v, want 0", remaining)
	}
}

func TestRecordExhaustionSchedulesResume(t *testing.T) {
	m, clock, _ := newManager(t, WithCooldown(time.Hour))
	ctx := context.Background()

	state, err := m.RecordExhaustion(ctx, time.Time{}, "usage limit reached")
	if err != nil {
		t.Fatalf("RecordExhaustion failed: %v", err)
	}

	if !state.Exhausted {
		t.Error("state should be exhausted")
	}
	if state.Reason != "usage limit reached" {
		t.Errorf("Reason: got %q", state.Reason)
	}

	want := clock.Now().Add(time.Hour)
	if !state.ResumeAt.Equal(want) {
		t.Errorf("ResumeAt: got %v, want %v", state.ResumeAt, want)
	}

	available, remaining := m.Available(ctx)
	if available {
		t.Error("must not be available during a cooldown")
	}
	if remaining != time.Hour {
		t.Errorf("remaining: got %v, want 1h", remaining)
	}
}

// A provider-supplied reset time is better information than any estimate.
func TestResetHintOverridesEstimate(t *testing.T) {
	m, clock, _ := newManager(t, WithCooldown(time.Hour))
	ctx := context.Background()

	hint := clock.Now().Add(10 * time.Minute)
	state, err := m.RecordExhaustion(ctx, hint, "rate limited")
	if err != nil {
		t.Fatalf("RecordExhaustion failed: %v", err)
	}

	if !state.ResumeAt.Equal(hint) {
		t.Errorf("ResumeAt: got %v, want the hint %v", state.ResumeAt, hint)
	}
}

func TestPastResetHintIsIgnored(t *testing.T) {
	m, clock, _ := newManager(t, WithCooldown(time.Hour))
	ctx := context.Background()

	// A stale hint would schedule a resume in the past, making the cooldown a
	// no-op and sending the system straight back into the limit.
	stale := clock.Now().Add(-time.Hour)
	state, err := m.RecordExhaustion(ctx, stale, "rate limited")
	if err != nil {
		t.Fatalf("RecordExhaustion failed: %v", err)
	}

	if !state.ResumeAt.After(clock.Now()) {
		t.Errorf("ResumeAt must be in the future, got %v", state.ResumeAt)
	}
}

// Meeting a limit immediately after resuming means the estimate was too
// short, so the next wait must be longer.
func TestConsecutiveHitsBackOffExponentially(t *testing.T) {
	m, clock, _ := newManager(t, WithCooldown(time.Hour), WithMaxCooldown(8*time.Hour))
	ctx := context.Background()

	want := []time.Duration{time.Hour, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour, 8 * time.Hour}

	for i, expected := range want {
		state, err := m.RecordExhaustion(ctx, time.Time{}, "limit")
		if err != nil {
			t.Fatalf("hit %d: %v", i+1, err)
		}

		got := state.ResumeAt.Sub(clock.Now())
		if got != expected {
			t.Errorf("hit %d: cooldown %v, want %v", i+1, got, expected)
		}
		if state.ConsecutiveHits != i+1 {
			t.Errorf("hit %d: ConsecutiveHits %d", i+1, state.ConsecutiveHits)
		}
	}
}

func TestClearResetsBackoff(t *testing.T) {
	m, clock, _ := newManager(t, WithCooldown(time.Hour))
	ctx := context.Background()

	m.RecordExhaustion(ctx, time.Time{}, "limit")
	m.RecordExhaustion(ctx, time.Time{}, "limit")

	if err := m.Clear(ctx); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	if available, _ := m.Available(ctx); !available {
		t.Error("must be available after Clear")
	}

	// A success proves the limit lifted, so the next hit starts from the base.
	state, _ := m.RecordExhaustion(ctx, time.Time{}, "limit")
	if got := state.ResumeAt.Sub(clock.Now()); got != time.Hour {
		t.Errorf("cooldown after Clear: got %v, want 1h", got)
	}
}

func TestCooldownExpiresWithTime(t *testing.T) {
	m, clock, _ := newManager(t, WithCooldown(time.Hour))
	ctx := context.Background()

	m.RecordExhaustion(ctx, time.Time{}, "limit")

	clock.Advance(59 * time.Minute)
	if available, _ := m.Available(ctx); available {
		t.Error("must still be waiting one minute before the deadline")
	}

	clock.Advance(2 * time.Minute)
	if available, remaining := m.Available(ctx); !available {
		t.Errorf("must be available after the deadline, %v remaining", remaining)
	}
}

// The property that makes unattended operation safe: a process that dies
// mid-cooldown and restarts must keep waiting, not resume into the limit.
func TestCooldownSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	clock := newClock(epoch)
	ctx := context.Background()

	first, err := New(dir, WithClock(clock.Now), WithCooldown(time.Hour))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if _, err := first.RecordExhaustion(ctx, time.Time{}, "usage limit"); err != nil {
		t.Fatalf("RecordExhaustion failed: %v", err)
	}

	// A different Manager over the same directory stands in for a restart.
	clock.Advance(10 * time.Minute)

	second, err := New(dir, WithClock(clock.Now), WithCooldown(time.Hour))
	if err != nil {
		t.Fatalf("reopening failed: %v", err)
	}

	available, remaining := second.Available(ctx)
	if available {
		t.Fatal("a restarted process resumed straight into an active cooldown")
	}
	if remaining != 50*time.Minute {
		t.Errorf("remaining: got %v, want 50m", remaining)
	}

	state := second.Current(ctx)
	if state.Reason != "usage limit" {
		t.Errorf("Reason lost across restart: %q", state.Reason)
	}
	if state.ConsecutiveHits != 1 {
		t.Errorf("ConsecutiveHits lost across restart: %d", state.ConsecutiveHits)
	}
}

func TestRestartAfterCooldownExpiry(t *testing.T) {
	dir := t.TempDir()
	clock := newClock(epoch)
	ctx := context.Background()

	first, _ := New(dir, WithClock(clock.Now), WithCooldown(time.Hour))
	first.RecordExhaustion(ctx, time.Time{}, "limit")

	// The machine was down longer than the cooldown.
	clock.Advance(3 * time.Hour)

	second, _ := New(dir, WithClock(clock.Now), WithCooldown(time.Hour))
	if available, _ := second.Available(ctx); !available {
		t.Error("a cooldown that expired while the process was down must be over")
	}
}

func TestWaitUntilAvailableSleepsForRemaining(t *testing.T) {
	m, _, sleeper := newManager(t, WithCooldown(time.Hour))
	ctx := context.Background()

	m.RecordExhaustion(ctx, time.Time{}, "limit")

	waited, err := m.WaitUntilAvailable(ctx)
	if err != nil {
		t.Fatalf("WaitUntilAvailable failed: %v", err)
	}
	if waited != time.Hour {
		t.Errorf("waited: got %v, want 1h", waited)
	}
	if sleeper.count() != 1 {
		t.Errorf("expected one sleep, got %d", sleeper.count())
	}
	if sleeper.last() != time.Hour {
		t.Errorf("sleep duration: got %v, want 1h", sleeper.last())
	}
}

func TestWaitReturnsImmediatelyWhenAvailable(t *testing.T) {
	m, _, sleeper := newManager(t)
	ctx := context.Background()

	waited, err := m.WaitUntilAvailable(ctx)
	if err != nil {
		t.Fatalf("WaitUntilAvailable failed: %v", err)
	}
	if waited != 0 {
		t.Errorf("waited: got %v, want 0", waited)
	}
	if sleeper.count() != 0 {
		t.Error("must not sleep when quota is available")
	}
}

// Cancellation must not silently consume the cooldown; a later run has to
// still observe it.
func TestWaitHonoursCancellation(t *testing.T) {
	m, _, _ := newManager(t, WithCooldown(time.Hour))
	ctx := context.Background()

	m.RecordExhaustion(ctx, time.Time{}, "limit")

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	// The real sleeper is used here so cancellation is genuinely exercised.
	real, err := New(t.TempDir(), WithCooldown(time.Hour))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	real.RecordExhaustion(ctx, time.Time{}, "limit")

	start := time.Now()
	if _, err := real.WaitUntilAvailable(cancelled); err == nil {
		t.Fatal("a cancelled context must abort the wait")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("cancellation took %v; it should be immediate", elapsed)
	}

	if available, _ := real.Available(ctx); available {
		t.Error("cancelling the wait must not clear the cooldown")
	}
}

func TestNoteCheckpointLinksSavedState(t *testing.T) {
	m, _, _ := newManager(t)
	ctx := context.Background()

	m.RecordExhaustion(ctx, time.Time{}, "limit")
	if err := m.NoteCheckpoint(ctx, "ckpt-123"); err != nil {
		t.Fatalf("NoteCheckpoint failed: %v", err)
	}

	if got := m.Current(ctx).LastCheckpoint; got != "ckpt-123" {
		t.Errorf("LastCheckpoint: got %q, want ckpt-123", got)
	}

	// The link must outlive the cooldown so recovery can find where to resume.
	if err := m.Clear(ctx); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}
	if got := m.Current(ctx).LastCheckpoint; got != "ckpt-123" {
		t.Errorf("LastCheckpoint lost on Clear: %q", got)
	}
}

// A damaged scratch file must not become an outage that nothing can clear.
func TestCorruptStateFileTreatedAsAvailable(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	m, err := New(dir)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, StateFile), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if available, _ := m.Available(ctx); !available {
		t.Error("a corrupt quota file must not permanently block the system")
	}

	// And it must be repairable by the next write.
	if _, err := m.RecordExhaustion(ctx, time.Time{}, "limit"); err != nil {
		t.Fatalf("RecordExhaustion over a corrupt file failed: %v", err)
	}
	if available, _ := m.Available(ctx); available {
		t.Error("state should have been rewritten and honoured")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m, _, _ := newManager(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.RecordExhaustion(ctx, time.Time{}, "limit")
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Available(ctx)
			m.Current(ctx)
		}()
	}
	wg.Wait()

	if state := m.Current(ctx); !state.Exhausted {
		t.Error("expected an exhausted state after concurrent writes")
	}
}
