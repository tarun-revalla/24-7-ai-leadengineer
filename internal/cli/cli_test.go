package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/app"
	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// run executes the CLI with the given arguments against a project directory,
// returning combined output. Commands are exercised through cobra so flag
// parsing and wiring are covered, not just the functions behind them.
func run(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()

	root := NewRootCommand("test", "none", "none")

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append([]string{"-C", dir}, args...))

	err := root.ExecuteContext(context.Background())
	return buf.String(), err
}

func newRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}
	return dir
}

func TestInitCommand(t *testing.T) {
	dir := newRepo(t)

	out, err := run(t, dir, "init", "--name", "Demo")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}

	for _, want := range []string{"PROJECT.md", "BACKLOG.md", "CURRENT.md", "created"} {
		if !strings.Contains(out, want) {
			t.Errorf("init output missing %q:\n%s", want, out)
		}
	}
}

func TestInitCommandIsIdempotent(t *testing.T) {
	dir := newRepo(t)

	if _, err := run(t, dir, "init"); err != nil {
		t.Fatalf("first init failed: %v", err)
	}

	out, err := run(t, dir, "init")
	if err != nil {
		t.Fatalf("second init failed: %v", err)
	}
	if !strings.Contains(out, "nothing changed") {
		t.Errorf("re-init should report no changes:\n%s", out)
	}
	if strings.Contains(out, "created") {
		t.Errorf("re-init should not create anything:\n%s", out)
	}
}

func TestStatusCommand(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "init", "--name", "Demo")

	out, err := run(t, dir, "status")
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, out)
	}

	for _, want := range []string{"Demo", "Current task", "Backlog", "Repository", "Checkpoints"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q:\n%s", want, out)
		}
	}
}

func TestStatusOnUninitializedProject(t *testing.T) {
	dir := newRepo(t)

	out, err := run(t, dir, "status")
	if err != nil {
		t.Fatalf("status should succeed on an uninitialised project: %v", err)
	}
	if !strings.Contains(out, "init") {
		t.Errorf("status should direct the operator to init:\n%s", out)
	}
}

// A command that does nothing must not report success. Returning nil from a
// stub is the silent-failure pattern this system exists to avoid.
func TestUnimplementedCommandsReturnError(t *testing.T) {
	dir := newRepo(t)

	for _, name := range []string{"recover"} {
		t.Run(name, func(t *testing.T) {
			_, err := run(t, dir, name)
			if err == nil {
				t.Fatalf("%s returned success without doing anything", name)
			}
			if !errors.Is(err, ErrNotImplemented) {
				t.Errorf("%s: got %v, want ErrNotImplemented", name, err)
			}
		})
	}
}

func TestStartOnUninitializedProject(t *testing.T) {
	dir := newRepo(t)

	if _, err := run(t, dir, "start"); err == nil {
		t.Fatal("start on an uninitialised project should fail rather than silently do nothing")
	}
}

func TestStartWithEmptyBacklog(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "init")

	out, err := run(t, dir, "start")
	if err != nil {
		t.Fatalf("start with an empty backlog should succeed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "No open tasks") {
		t.Errorf("expected an empty-backlog message:\n%s", out)
	}
}

func TestCheckpointListEmpty(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "init")

	out, err := run(t, dir, "checkpoint", "list")
	if err != nil {
		t.Fatalf("checkpoint list failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "No checkpoints") {
		t.Errorf("expected an empty-store message:\n%s", out)
	}
}

func TestCheckpointVerifyEmpty(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "init")

	out, err := run(t, dir, "checkpoint", "verify")
	if err != nil {
		t.Fatalf("verify on an empty store should succeed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "0 corrupt") {
		t.Errorf("expected a clean report:\n%s", out)
	}
}

func TestCheckpointShowMissing(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "init")

	if _, err := run(t, dir, "checkpoint", "show", "ckpt-nonexistent"); err == nil {
		t.Fatal("showing a missing checkpoint must fail")
	}
}

func TestCheckpointShowRejectsUnsafeID(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "init")

	if _, err := run(t, dir, "checkpoint", "show", "../../etc/passwd"); err == nil {
		t.Fatal("a traversal id must be refused")
	}
}

func TestConfigShow(t *testing.T) {
	dir := newRepo(t)

	out, err := run(t, dir, "config", "show")
	if err != nil {
		t.Fatalf("config show failed: %v\n%s", err, out)
	}

	for _, want := range []string{"claude.model", "checkpoint.retention", "logging.level"} {
		if !strings.Contains(out, want) {
			t.Errorf("config output missing %q:\n%s", want, out)
		}
	}
}

func TestInvalidConfigFileIsReported(t *testing.T) {
	dir := newRepo(t)

	root := NewRootCommand("test", "none", "none")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"-C", dir, "--config", "/nonexistent/config.yaml", "status"})

	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatal("a missing config file named explicitly must be an error")
	}
}

func TestVersionFlag(t *testing.T) {
	dir := newRepo(t)

	out, err := run(t, dir, "--version")
	if err != nil {
		t.Fatalf("--version failed: %v", err)
	}
	if !strings.Contains(out, "test") {
		t.Errorf("version output missing the version:\n%s", out)
	}
}

// seedCheckpoints creates n checkpoints in an initialised project.
func seedCheckpoints(t *testing.T, dir string, n int) []string {
	t.Helper()

	a, err := app.New(app.Options{ProjectPath: dir, LogToStderr: true})
	if err != nil {
		t.Fatalf("app.New failed: %v", err)
	}
	defer a.Close()

	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id, err := a.Checkpoints.CreateCheckpoint(context.Background(), &interfaces.CheckpointState{
			TaskID:    fmt.Sprintf("T-%d", i),
			TaskState: "implementing",
			Progress:  float64(i) / float64(n),
			GitCommit: "abc123def456",
			QuotaState: &interfaces.QuotaSnapshot{
				Remaining: 0.6,
				ResetTime: time.Now().Add(time.Hour),
			},
		})
		if err != nil {
			t.Fatalf("CreateCheckpoint failed: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestCheckpointListWithEntries(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "init")
	ids := seedCheckpoints(t, dir, 3)

	out, err := run(t, dir, "checkpoint", "list")
	if err != nil {
		t.Fatalf("checkpoint list failed: %v\n%s", err, out)
	}

	if !strings.Contains(out, "ID") || !strings.Contains(out, "STATE") {
		t.Errorf("expected a table header:\n%s", out)
	}
	for _, id := range ids {
		if !strings.Contains(out, id) {
			t.Errorf("listing missing checkpoint %s:\n%s", id, out)
		}
	}
	if strings.Contains(out, "CORRUPT") {
		t.Errorf("intact checkpoints reported as corrupt:\n%s", out)
	}
}

func TestCheckpointShowDisplaysState(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "init")
	ids := seedCheckpoints(t, dir, 1)

	out, err := run(t, dir, "checkpoint", "show", ids[0])
	if err != nil {
		t.Fatalf("checkpoint show failed: %v\n%s", err, out)
	}

	for _, want := range []string{ids[0], "T-0", "implementing", "abc123def456", "Quota"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

// Corruption must be visible to the operator and must make verify fail, since
// a clean report would imply state that cannot actually be recovered.
func TestCheckpointVerifyReportsCorruption(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "init")
	ids := seedCheckpoints(t, dir, 2)

	path := filepath.Join(dir, ".ai", "checkpoints", ids[1]+".ckpt.json")
	if err := os.WriteFile(path, []byte("{truncated"), 0o644); err != nil {
		t.Fatalf("failed to corrupt checkpoint: %v", err)
	}

	out, err := run(t, dir, "checkpoint", "verify")
	if err == nil {
		t.Fatal("verify must fail when a checkpoint is corrupt")
	}
	if !strings.Contains(out, "CORRUPT") {
		t.Errorf("verify should name the corrupt checkpoint:\n%s", out)
	}
	if !strings.Contains(out, "1 corrupt") {
		t.Errorf("verify should report the count:\n%s", out)
	}

	listing, _ := run(t, dir, "checkpoint", "list")
	if !strings.Contains(listing, "CORRUPT") {
		t.Errorf("list should mark the corrupt checkpoint:\n%s", listing)
	}
}

func TestCheckpointPrune(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "init")
	seedCheckpoints(t, dir, 5)

	out, err := run(t, dir, "checkpoint", "prune", "--keep", "2")
	if err != nil {
		t.Fatalf("prune failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Removed 3") {
		t.Errorf("expected 3 removed:\n%s", out)
	}

	listing, _ := run(t, dir, "checkpoint", "list")
	if strings.Count(listing, "ckpt-") != 2 {
		t.Errorf("expected 2 checkpoints to remain:\n%s", listing)
	}
}

func TestCheckpointPruneRejectsZero(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "init")
	seedCheckpoints(t, dir, 2)

	// --keep 0 falls back to the configured retention rather than deleting
	// everything, so the store must still hold its checkpoints afterwards.
	if _, err := run(t, dir, "checkpoint", "prune", "--keep", "0"); err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	listing, _ := run(t, dir, "checkpoint", "list")
	if strings.Contains(listing, "No checkpoints") {
		t.Error("prune with --keep 0 deleted everything")
	}
}

func TestQuotaStatusWhenAvailable(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "init")

	out, err := run(t, dir, "quota", "status")
	if err != nil {
		t.Fatalf("quota status failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "available") {
		t.Errorf("expected an available report:\n%s", out)
	}
}

func TestQuotaStatusDuringCooldown(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "init")

	a, err := app.New(app.Options{ProjectPath: dir, LogToStderr: true})
	if err != nil {
		t.Fatalf("app.New failed: %v", err)
	}
	a.EnterQuotaCooldown(context.Background(),
		&interfaces.CheckpointState{TaskID: "T-7"}, time.Time{}, "usage limit reached")
	a.Close()

	out, err := run(t, dir, "quota", "status")
	if err != nil {
		t.Fatalf("quota status failed: %v\n%s", err, out)
	}

	for _, want := range []string{"cooldown", "Resumes", "usage limit reached", "Checkpoint"} {
		if !strings.Contains(out, want) {
			t.Errorf("quota status missing %q:\n%s", want, out)
		}
	}
}

func TestQuotaClear(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "init")

	a, _ := app.New(app.Options{ProjectPath: dir, LogToStderr: true})
	a.EnterQuotaCooldown(context.Background(), nil, time.Time{}, "usage limit")
	a.Close()

	out, err := run(t, dir, "quota", "clear")
	if err != nil {
		t.Fatalf("quota clear failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "cleared") {
		t.Errorf("expected confirmation:\n%s", out)
	}

	after, _ := run(t, dir, "quota", "status")
	if !strings.Contains(after, "available") {
		t.Errorf("quota should be available after clearing:\n%s", after)
	}
}

func TestQuotaClearWithNoCooldown(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "init")

	out, err := run(t, dir, "quota", "clear")
	if err != nil {
		t.Fatalf("quota clear failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "nothing to clear") {
		t.Errorf("expected a no-op message:\n%s", out)
	}
}
