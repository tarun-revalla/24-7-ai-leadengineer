// Package integration exercises the wired subsystems together, against a real
// filesystem and a real git repository. Unit tests cover each subsystem in
// isolation; these cover the seams between them, where the earlier defects in
// this project lived.
package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/app"
	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// newProject creates an initialised project in a real git repository.
func newProject(t *testing.T) *app.App {
	t.Helper()

	dir := t.TempDir()

	for _, args := range [][]string{
		{"init"},
		{"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	a, err := app.New(app.Options{ProjectPath: dir, LogToStderr: true})
	if err != nil {
		t.Fatalf("app.New failed: %v", err)
	}
	t.Cleanup(func() { a.Close() })

	return a
}

func TestInitCreatesUsableMemory(t *testing.T) {
	a := newProject(t)
	ctx := context.Background()

	result, err := a.Initialize(ctx, "Demo")
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if len(result.Created) == 0 {
		t.Fatal("expected documents to be created")
	}

	// Every document init writes must be readable by the manager that wrote
	// it. The original format defect was exactly this round trip failing.
	if _, err := a.Memory.GetProject(ctx); err != nil {
		t.Errorf("GetProject after init: %v", err)
	}
	if _, err := a.Memory.GetBacklog(ctx); err != nil {
		t.Errorf("GetBacklog after init: %v", err)
	}
	if _, err := a.Memory.GetCurrent(ctx); err != nil {
		t.Errorf("GetCurrent after init: %v", err)
	}
	if _, err := a.Memory.GetChangelog(ctx); err != nil {
		t.Errorf("GetChangelog after init: %v", err)
	}
	if _, err := a.Memory.GetDecisions(ctx); err != nil {
		t.Errorf("GetDecisions after init: %v", err)
	}
	if err := a.Memory.SyncWithClaude(ctx); err != nil {
		t.Errorf("SyncWithClaude after init: %v", err)
	}
}

// Re-running init on a live project must not discard its state. This is the
// difference between a safe operation and one that destroys a month of work.
func TestInitIsNonDestructive(t *testing.T) {
	a := newProject(t)
	ctx := context.Background()

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := a.Memory.SaveBacklog(ctx, &interfaces.Backlog{
		Tasks: []interfaces.BacklogTask{{ID: "T-1", Title: "Real work", Priority: 1, Status: "new"}},
	}); err != nil {
		t.Fatalf("SaveBacklog failed: %v", err)
	}
	if err := a.Memory.SaveDecision(ctx, interfaces.Decision{ID: "D-1", Title: "A decision"}); err != nil {
		t.Fatalf("SaveDecision failed: %v", err)
	}

	result, err := a.Initialize(ctx, "Demo")
	if err != nil {
		t.Fatalf("second Initialize failed: %v", err)
	}
	if len(result.Created) != 0 {
		t.Errorf("re-init created documents that already existed: %v", result.Created)
	}

	backlog, err := a.Memory.GetBacklog(ctx)
	if err != nil {
		t.Fatalf("GetBacklog: %v", err)
	}
	if len(backlog.Tasks) != 1 || backlog.Tasks[0].ID != "T-1" {
		t.Errorf("re-init destroyed the backlog: %+v", backlog.Tasks)
	}

	decisions, err := a.Memory.GetDecisions(ctx)
	if err != nil {
		t.Fatalf("GetDecisions: %v", err)
	}
	if len(decisions) != 1 {
		t.Errorf("re-init destroyed decisions: %+v", decisions)
	}
}

func TestStatusReflectsRealState(t *testing.T) {
	a := newProject(t)
	ctx := context.Background()
	a.Initialize(ctx, "Demo")

	a.Memory.SaveCurrent(ctx, &interfaces.CurrentTask{
		TaskID: "T-42", Title: "Wire it up", Status: "implementing", Progress: 0.5,
	})
	a.Memory.SaveBacklog(ctx, &interfaces.Backlog{
		Tasks: []interfaces.BacklogTask{
			{ID: "T-9", Title: "Low priority", Priority: 9, Status: "new"},
			{ID: "T-1", Title: "Urgent", Priority: 1, Status: "new"},
			{ID: "T-5", Title: "Finished", Priority: 5, Status: "done"},
		},
	})

	status := a.Status(ctx)
	if !status.Initialized {
		t.Fatal("project should report initialised")
	}

	if status.Current.TaskID != "T-42" {
		t.Errorf("current task: got %q, want T-42", status.Current.TaskID)
	}

	next := status.NextTasks(0)
	if len(next) != 2 {
		t.Fatalf("got %d open tasks, want 2 (completed work excluded)", len(next))
	}
	if next[0].ID != "T-1" {
		t.Errorf("highest priority first expected, got %q", next[0].ID)
	}

	if status.GitErr != nil {
		t.Errorf("git status failed: %v", status.GitErr)
	}
	if status.Git.Branch == "" {
		t.Error("branch should be reported")
	}

	rendered := status.Render()
	for _, want := range []string{"T-42", "Wire it up", "T-1", "Demo"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered status missing %q:\n%s", want, rendered)
		}
	}
}

func TestStatusOnUninitializedProject(t *testing.T) {
	a := newProject(t)

	status := a.Status(context.Background())
	if status.Initialized {
		t.Error("project without memory should not report initialised")
	}

	rendered := status.Render()
	if !strings.Contains(rendered, "init") {
		t.Errorf("status should tell the operator to run init:\n%s", rendered)
	}
}

// The recovery guarantee, exercised through the wired system: a damaged newest
// checkpoint must not prevent recovery from an older intact one.
func TestRecoveryPrefersNewestIntactCheckpoint(t *testing.T) {
	a := newProject(t)
	ctx := context.Background()
	a.Initialize(ctx, "Demo")

	goodID, err := a.Checkpoints.CreateCheckpoint(ctx, &interfaces.CheckpointState{
		TaskID: "T-1", TaskState: "implementing", Progress: 0.5,
	})
	if err != nil {
		t.Fatalf("CreateCheckpoint failed: %v", err)
	}

	badID, err := a.Checkpoints.CreateCheckpoint(ctx, &interfaces.CheckpointState{
		TaskID: "T-2", TaskState: "testing", Progress: 0.9,
	})
	if err != nil {
		t.Fatalf("CreateCheckpoint failed: %v", err)
	}

	// Simulate a torn write: the newest checkpoint is unreadable.
	badPath := filepath.Join(a.StatePath("checkpoints"), badID+".ckpt.json")
	if err := os.WriteFile(badPath, []byte(`{"version":1,"payload":`), 0o644); err != nil {
		t.Fatalf("failed to corrupt checkpoint: %v", err)
	}

	latest, err := a.Checkpoints.GetLatestCheckpoint(ctx)
	if err != nil {
		t.Fatalf("recovery failed with a corrupt newest checkpoint: %v", err)
	}
	if latest.ID != goodID {
		t.Errorf("recovered %q, want the older intact checkpoint %q", latest.ID, goodID)
	}
	if latest.TaskID != "T-1" {
		t.Errorf("recovered task %q, want T-1", latest.TaskID)
	}

	status := a.Status(ctx)
	if len(status.CorruptCheckpoints) != 1 {
		t.Errorf("status should surface the corrupt checkpoint, got %v", status.CorruptCheckpoints)
	}
	if !strings.Contains(status.Render(), "corrupt") {
		t.Error("rendered status should warn about corruption")
	}
}

func TestCheckpointSurvivesReopeningTheProject(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	cmd.Run()

	first, err := app.New(app.Options{ProjectPath: dir, LogToStderr: true})
	if err != nil {
		t.Fatalf("app.New failed: %v", err)
	}
	first.Initialize(ctx, "Demo")

	id, err := first.Checkpoints.CreateCheckpoint(ctx, &interfaces.CheckpointState{
		TaskID: "T-persist", Progress: 0.25, GitCommit: "abc123",
	})
	if err != nil {
		t.Fatalf("CreateCheckpoint failed: %v", err)
	}
	first.Close()

	// A fresh process must find the same state on disk.
	second, err := app.New(app.Options{ProjectPath: dir, LogToStderr: true})
	if err != nil {
		t.Fatalf("reopening failed: %v", err)
	}
	defer second.Close()

	restored, err := second.Checkpoints.LoadCheckpoint(ctx, id)
	if err != nil {
		t.Fatalf("checkpoint did not survive reopening: %v", err)
	}
	if restored.TaskID != "T-persist" || restored.GitCommit != "abc123" {
		t.Errorf("state altered across restart: %+v", restored)
	}

	project, err := second.Memory.GetProject(ctx)
	if err != nil {
		t.Fatalf("memory did not survive reopening: %v", err)
	}
	if project.Name != "Demo" {
		t.Errorf("project name: got %q, want Demo", project.Name)
	}
}

// A state write must not discard the prose a human or Claude wrote alongside it.
func TestDocumentBodySurvivesStateUpdates(t *testing.T) {
	a := newProject(t)
	ctx := context.Background()
	a.Initialize(ctx, "Demo")

	original, err := a.Memory.ReadFile(ctx, "PROJECT.md")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !strings.Contains(original, "# Project") {
		t.Fatalf("expected a seeded body:\n%s", original)
	}

	for i := 0; i < 3; i++ {
		if err := a.Memory.SaveProject(ctx, &interfaces.ProjectMetadata{
			Name: "Demo", Purpose: "Updated purpose",
		}); err != nil {
			t.Fatalf("SaveProject failed: %v", err)
		}
	}

	after, _ := a.Memory.ReadFile(ctx, "PROJECT.md")
	if !strings.Contains(after, "# Project") {
		t.Errorf("document body destroyed by state updates:\n%s", after)
	}
	if strings.Count(after, "---") < 2 {
		t.Errorf("front matter malformed after repeated writes:\n%s", after)
	}

	project, err := a.Memory.GetProject(ctx)
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}
	if project.Purpose != "Updated purpose" {
		t.Errorf("Purpose: got %q, want 'Updated purpose'", project.Purpose)
	}
}

func TestGitStatusReflectsWorkingTree(t *testing.T) {
	a := newProject(t)
	ctx := context.Background()

	status, err := a.Git.Status(ctx)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if !status.IsClean {
		t.Error("a fresh repository should be clean")
	}

	if err := os.WriteFile(filepath.Join(a.ProjectPath, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	status, err = a.Git.Status(ctx)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status.IsClean {
		t.Error("a repository with an untracked file must not report clean")
	}
}

func TestConfigDefaultsAreAvailable(t *testing.T) {
	a := newProject(t)

	if got := a.Config.GetString("claude.model"); got == "" {
		t.Error("claude.model should have a default")
	}
	if got := a.Config.GetInt("checkpoint.retention"); got <= 0 {
		t.Errorf("checkpoint.retention: got %d, want a positive default", got)
	}
}
