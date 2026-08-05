package recovery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/checkpoint"
	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

type fakeGit struct {
	status *interfaces.GitStatus
	err    error
}

func (g *fakeGit) Status(ctx context.Context) (*interfaces.GitStatus, error) {
	if g.err != nil {
		return nil, g.err
	}
	return g.status, nil
}

func cleanStatus() *interfaces.GitStatus {
	return &interfaces.GitStatus{Branch: "main", IsClean: true}
}

type fakeMemory struct {
	backlog    *interfaces.Backlog
	backlogErr error
	current    *interfaces.CurrentTask
	currentErr error
}

func (m *fakeMemory) GetBacklog(ctx context.Context) (*interfaces.Backlog, error) {
	if m.backlogErr != nil {
		return nil, m.backlogErr
	}
	return m.backlog, nil
}

func (m *fakeMemory) GetCurrent(ctx context.Context) (*interfaces.CurrentTask, error) {
	if m.currentErr != nil {
		return nil, m.currentErr
	}
	return m.current, nil
}

type fakeCheckpoints struct {
	latest     *interfaces.CheckpointState
	latestErr  error
	list       []interfaces.CheckpointMetadata
	listErr    error
	invalidIDs map[string]bool
}

func (c *fakeCheckpoints) GetLatestCheckpoint(ctx context.Context) (*interfaces.CheckpointState, error) {
	if c.latestErr != nil {
		return nil, c.latestErr
	}
	return c.latest, nil
}

func (c *fakeCheckpoints) ListCheckpoints(ctx context.Context) ([]interfaces.CheckpointMetadata, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return c.list, nil
}

func (c *fakeCheckpoints) ValidateCheckpoint(ctx context.Context, id string) error {
	if c.invalidIDs[id] {
		return checkpoint.ErrCorrupt
	}
	return nil
}

func noCheckpoints() *fakeCheckpoints {
	return &fakeCheckpoints{latestErr: checkpoint.ErrNoCheckpoints}
}

func TestAnalyzeNoInterruptedWork(t *testing.T) {
	a := New(
		&fakeGit{status: cleanStatus()},
		&fakeMemory{
			backlog: &interfaces.Backlog{Tasks: []interfaces.BacklogTask{
				{ID: "T-1", Status: "done"},
				{ID: "T-2", Status: "new"},
				{ID: "T-3", Status: "blocked"},
			}},
			current: &interfaces.CurrentTask{},
		},
		noCheckpoints(),
	)

	report, err := a.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if report.Interrupted {
		t.Error("no task is in-progress; should not report interruption")
	}
	if !report.SafeToResume {
		t.Error("with nothing interrupted, resuming is always safe")
	}
}

func TestAnalyzeInterruptedWithCleanTree(t *testing.T) {
	a := New(
		&fakeGit{status: cleanStatus()},
		&fakeMemory{
			backlog: &interfaces.Backlog{Tasks: []interfaces.BacklogTask{
				{ID: "T-1", Title: "Add feature", Status: "in-progress"},
			}},
			current: &interfaces.CurrentTask{Status: "implementing", Progress: 0.5},
		},
		noCheckpoints(),
	)

	report, err := a.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if !report.Interrupted {
		t.Fatal("expected an interrupted task to be detected")
	}
	if report.Task.ID != "T-1" {
		t.Errorf("Task: got %q, want T-1", report.Task.ID)
	}
	if !report.SafeToResume {
		t.Error("a clean tree after interruption should be safe to resume")
	}

	rendered := report.Render()
	if !strings.Contains(rendered, "Safe to resume") {
		t.Errorf("rendered report should say it's safe to resume:\n%s", rendered)
	}
}

func TestAnalyzeInterruptedWithDirtyTree(t *testing.T) {
	dirty := &interfaces.GitStatus{
		Branch:          "main",
		UnstagedChanges: []string{"main.go"},
		UntrackedFiles:  []string{"scratch.go"},
	}

	a := New(
		&fakeGit{status: dirty},
		&fakeMemory{
			backlog: &interfaces.Backlog{Tasks: []interfaces.BacklogTask{
				{ID: "T-1", Status: "in-progress"},
			}},
			current: &interfaces.CurrentTask{},
		},
		noCheckpoints(),
	)

	report, err := a.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if report.SafeToResume {
		t.Error("a dirty tree after interruption must not be reported as safe")
	}
	if report.ModifiedCount != 1 || report.UntrackedCount != 1 {
		t.Errorf("dirty counts: modified=%d untracked=%d, want 1 and 1",
			report.ModifiedCount, report.UntrackedCount)
	}

	rendered := report.Render()
	for _, want := range []string{"uncommitted changes", "will not discard them automatically", "git status", "git diff"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered report missing %q:\n%s", want, rendered)
		}
	}
}

// State-directory changes are the system's own bookkeeping, never a sign of
// leftover work — resuming past them must stay safe.
func TestAnalyzeIgnoresStateDirChanges(t *testing.T) {
	status := &interfaces.GitStatus{
		Branch:          "main",
		UnstagedChanges: []string{".ai/CURRENT.md", ".ai/BACKLOG.md"},
	}

	a := New(
		&fakeGit{status: status},
		&fakeMemory{
			backlog: &interfaces.Backlog{Tasks: []interfaces.BacklogTask{
				{ID: "T-1", Status: "in-progress"},
			}},
			current: &interfaces.CurrentTask{},
		},
		noCheckpoints(),
	)

	report, err := a.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if !report.SafeToResume {
		t.Error("changes confined to .ai/ must not block resumption")
	}
	if report.ModifiedCount != 0 {
		t.Errorf("ModifiedCount: got %d, want 0 (state dir changes excluded)", report.ModifiedCount)
	}
}

func TestAnalyzeWithConflicts(t *testing.T) {
	status := &interfaces.GitStatus{Branch: "main", HasConflicts: true}

	a := New(
		&fakeGit{status: status},
		&fakeMemory{
			backlog: &interfaces.Backlog{Tasks: []interfaces.BacklogTask{
				{ID: "T-1", Status: "in-progress"},
			}},
			current: &interfaces.CurrentTask{},
		},
		noCheckpoints(),
	)

	report, err := a.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if report.SafeToResume {
		t.Error("conflicts must never be reported as safe to resume")
	}
	if !report.HasConflicts {
		t.Error("HasConflicts should be true")
	}

	rendered := report.Render()
	if !strings.Contains(rendered, "conflicts") {
		t.Errorf("rendered report should mention conflicts:\n%s", rendered)
	}
}

func TestAnalyzeGitStatusFailureIsReportedNotFatal(t *testing.T) {
	a := New(
		&fakeGit{err: errors.New("not a git repository")},
		&fakeMemory{
			backlog: &interfaces.Backlog{Tasks: []interfaces.BacklogTask{
				{ID: "T-1", Status: "in-progress"},
			}},
			current: &interfaces.CurrentTask{},
		},
		noCheckpoints(),
	)

	report, err := a.Analyze(context.Background())
	if err != nil {
		t.Fatalf("a git failure should not fail the whole analysis: %v", err)
	}

	if report.GitErr == nil {
		t.Error("GitErr should record the failure")
	}
	if report.SafeToResume {
		t.Error("unknown tree state must not be reported as safe")
	}
}

func TestAnalyzeBacklogFailureIsFatal(t *testing.T) {
	a := New(
		&fakeGit{status: cleanStatus()},
		&fakeMemory{backlogErr: errors.New("disk error")},
		noCheckpoints(),
	)

	if _, err := a.Analyze(context.Background()); err == nil {
		t.Fatal("without a backlog there is nothing to analyze; this must fail")
	}
}

func TestAnalyzeSurfacesLatestCheckpoint(t *testing.T) {
	now := time.Now().UTC()
	a := New(
		&fakeGit{status: cleanStatus()},
		&fakeMemory{
			backlog: &interfaces.Backlog{},
			current: &interfaces.CurrentTask{},
		},
		&fakeCheckpoints{
			latest: &interfaces.CheckpointState{ID: "ckpt-1", TaskID: "T-1", Progress: 0.4, Timestamp: now},
			list:   []interfaces.CheckpointMetadata{{ID: "ckpt-1"}},
		},
	)

	report, err := a.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if report.LatestCheckpoint == nil || report.LatestCheckpoint.ID != "ckpt-1" {
		t.Fatalf("LatestCheckpoint not surfaced: %+v", report.LatestCheckpoint)
	}
	if report.CheckpointCount != 1 {
		t.Errorf("CheckpointCount: got %d, want 1", report.CheckpointCount)
	}

	rendered := report.Render()
	if !strings.Contains(rendered, "ckpt-1") {
		t.Errorf("rendered report missing the checkpoint id:\n%s", rendered)
	}
}

func TestAnalyzeReportsCorruptCheckpoints(t *testing.T) {
	a := New(
		&fakeGit{status: cleanStatus()},
		&fakeMemory{backlog: &interfaces.Backlog{}, current: &interfaces.CurrentTask{}},
		&fakeCheckpoints{
			latest: &interfaces.CheckpointState{ID: "ckpt-good"},
			list: []interfaces.CheckpointMetadata{
				{ID: "ckpt-good"},
				{ID: "ckpt-bad"},
			},
			invalidIDs: map[string]bool{"ckpt-bad": true},
		},
	)

	report, err := a.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if report.CorruptCheckpoints != 1 {
		t.Errorf("CorruptCheckpoints: got %d, want 1", report.CorruptCheckpoints)
	}

	rendered := report.Render()
	if !strings.Contains(rendered, "1 corrupt checkpoint") {
		t.Errorf("rendered report should mention corruption:\n%s", rendered)
	}
}

func TestRenderNoInterruptionMentionsNoWork(t *testing.T) {
	report := &Report{Interrupted: false, SafeToResume: true}

	rendered := report.Render()
	if !strings.Contains(rendered, "No interrupted work") {
		t.Errorf("rendered report: %s", rendered)
	}
}

func TestRenderIncludesRecordedErrors(t *testing.T) {
	report := &Report{
		Interrupted:  true,
		Task:         &interfaces.BacklogTask{ID: "T-1", Title: "Do the thing"},
		Current:      &interfaces.CurrentTask{Status: "implementing", Errors: []string{"claude timed out"}},
		TreeClean:    true,
		SafeToResume: true,
	}

	rendered := report.Render()
	if !strings.Contains(rendered, "claude timed out") {
		t.Errorf("rendered report should surface recorded errors:\n%s", rendered)
	}
}
