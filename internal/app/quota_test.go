package app

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

func newTestApp(t *testing.T) *App {
	t.Helper()

	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	a, err := New(Options{ProjectPath: dir, LogToStderr: true})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() { a.Close() })

	return a
}

func TestAwaitQuotaReturnsImmediatelyWhenAvailable(t *testing.T) {
	a := newTestApp(t)

	start := time.Now()
	waited, err := a.AwaitQuota(context.Background())
	if err != nil {
		t.Fatalf("AwaitQuota failed: %v", err)
	}
	if waited != 0 {
		t.Errorf("waited: got %v, want 0", waited)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("AwaitQuota blocked for %v with quota available", elapsed)
	}
}

// The work must be saved before the cooldown is recorded, so a crash during
// the wait still leaves something to resume from.
func TestEnterQuotaCooldownCheckpointsFirst(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	state := &interfaces.CheckpointState{
		TaskID: "T-1", TaskState: "implementing", Progress: 0.7,
	}

	recorded, err := a.EnterQuotaCooldown(ctx, state, time.Time{}, "usage limit")
	if err != nil {
		t.Fatalf("EnterQuotaCooldown failed: %v", err)
	}

	if !recorded.Exhausted {
		t.Error("quota should be marked exhausted")
	}
	if recorded.LastCheckpoint == "" {
		t.Fatal("cooldown must record the checkpoint it saved")
	}

	saved, err := a.Checkpoints.LoadCheckpoint(ctx, recorded.LastCheckpoint)
	if err != nil {
		t.Fatalf("the checkpoint taken before the cooldown is unreadable: %v", err)
	}
	if saved.TaskID != "T-1" || saved.Progress != 0.7 {
		t.Errorf("checkpoint did not capture the work: %+v", saved)
	}

	if available, _ := a.Quota.Available(ctx); available {
		t.Error("quota should be in cooldown")
	}
}

func TestEnterQuotaCooldownWithoutState(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	recorded, err := a.EnterQuotaCooldown(ctx, nil, time.Time{}, "usage limit")
	if err != nil {
		t.Fatalf("EnterQuotaCooldown failed: %v", err)
	}
	if recorded.LastCheckpoint != "" {
		t.Errorf("no checkpoint expected when there is no state, got %q", recorded.LastCheckpoint)
	}
	if !recorded.Exhausted {
		t.Error("cooldown should still be recorded")
	}
}

func TestClearQuotaAfterCooldown(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	a.EnterQuotaCooldown(ctx, nil, time.Time{}, "usage limit")

	if err := a.ClearQuota(ctx); err != nil {
		t.Fatalf("ClearQuota failed: %v", err)
	}
	if available, _ := a.Quota.Available(ctx); !available {
		t.Error("quota should be available after clearing")
	}
}

func TestClearQuotaIsNoOpWhenAlreadyAvailable(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	if err := a.ClearQuota(ctx); err != nil {
		t.Fatalf("ClearQuota on a fresh project failed: %v", err)
	}
}

func TestStatusReportsQuotaCooldown(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	a.Initialize(ctx, "Demo")

	a.EnterQuotaCooldown(ctx, &interfaces.CheckpointState{TaskID: "T-9"}, time.Time{}, "usage limit reached")

	status := a.Status(ctx)
	if status.QuotaAvailable {
		t.Error("status should report the cooldown")
	}
	if status.QuotaRemaining <= 0 {
		t.Errorf("remaining: got %v, want positive", status.QuotaRemaining)
	}

	rendered := status.Render()
	for _, want := range []string{"Claude quota", "cooldown", "usage limit reached", "saved as"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("status missing %q:\n%s", want, rendered)
		}
	}
}

func TestStatusReportsQuotaAvailable(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	a.Initialize(ctx, "Demo")

	rendered := a.Status(ctx).Render()
	if !strings.Contains(rendered, "available") {
		t.Errorf("status should report quota available:\n%s", rendered)
	}
}

// A cooldown must outlive the process that recorded it.
func TestQuotaCooldownSurvivesReopening(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	dir := a.ProjectPath

	a.EnterQuotaCooldown(ctx, nil, time.Time{}, "usage limit")
	a.Close()

	reopened, err := New(Options{ProjectPath: dir, LogToStderr: true})
	if err != nil {
		t.Fatalf("reopening failed: %v", err)
	}
	defer reopened.Close()

	if available, remaining := reopened.Quota.Available(ctx); available {
		t.Fatal("a restarted process ignored an active cooldown")
	} else if remaining <= 0 {
		t.Errorf("remaining: got %v, want positive", remaining)
	}
}

func TestAwaitQuotaHonoursCancellation(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	a.EnterQuotaCooldown(ctx, nil, time.Time{}, "usage limit")

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	if _, err := a.AwaitQuota(cancelled); err == nil {
		t.Fatal("a cancelled context must abort the wait")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("cancellation took %v; should be immediate", elapsed)
	}

	// The cooldown must remain recorded for the next run.
	if available, _ := a.Quota.Available(ctx); available {
		t.Error("cancelling the wait cleared the cooldown")
	}
}

func TestErrQuotaExhaustedIsIdentifiable(t *testing.T) {
	// Callers distinguish "stop and wait" from a genuine failure, so the
	// sentinel must survive wrapping.
	wrapped := errors.Join(ErrQuotaExhausted, errors.New("context"))
	if !errors.Is(wrapped, ErrQuotaExhausted) {
		t.Error("ErrQuotaExhausted must be identifiable through wrapping")
	}
}
