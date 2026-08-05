package executor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/gates"
	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

type harness struct {
	exec        *Executor
	claude      *fakeClaude
	git         *fakeGit
	memory      *fakeMemory
	checkpoints *fakeCheckpoints
}

func newHarness(t *testing.T, gateList []gates.Gate, tasks ...interfaces.BacklogTask) *harness {
	t.Helper()

	h := &harness{
		claude:      &fakeClaude{},
		git:         newFakeGit(),
		memory:      newFakeMemory(tasks...),
		checkpoints: &fakeCheckpoints{},
	}

	// The executor's own actions dirty the tree, so a passing run needs
	// something to commit.
	h.claude.onCall = func(int) { h.git.setDirty() }

	h.exec = New(
		Config{MaxRepairAttempts: 2, AutoCommit: true, ProjectPath: t.TempDir()},
		h.claude, h.git, h.memory, h.checkpoints,
		gates.NewRunner(gateList...),
		discardLogger{},
	)

	return h
}

func passingGate() *scriptedGate { return &scriptedGate{name: "test"} }

func TestRunNextSelectsHighestPriority(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()},
		sampleTask("T-LOW", 9),
		sampleTask("T-HIGH", 1),
		sampleTask("T-MID", 5),
	)

	outcome, err := h.exec.RunNext(context.Background())
	if err != nil {
		t.Fatalf("RunNext failed: %v", err)
	}

	if outcome.Task.ID != "T-HIGH" {
		t.Errorf("selected %q, want the highest-priority task T-HIGH", outcome.Task.ID)
	}
}

func TestRunNextSkipsClosedTasks(t *testing.T) {
	done := sampleTask("T-DONE", 1)
	done.Status = "done"
	blocked := sampleTask("T-BLOCKED", 2)
	blocked.Status = "blocked"

	h := newHarness(t, []gates.Gate{passingGate()}, done, blocked, sampleTask("T-OPEN", 8))

	outcome, err := h.exec.RunNext(context.Background())
	if err != nil {
		t.Fatalf("RunNext failed: %v", err)
	}
	if outcome.Task.ID != "T-OPEN" {
		t.Errorf("selected %q; completed and blocked tasks must not be re-run", outcome.Task.ID)
	}
}

func TestRunNextWithEmptyBacklog(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()})

	_, err := h.exec.RunNext(context.Background())
	if !errors.Is(err, ErrNoWork) {
		t.Errorf("got %v, want ErrNoWork", err)
	}
}

func TestSuccessfulRunCommitsAndRecords(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()}, sampleTask("T-1", 1))

	outcome, err := h.exec.Run(context.Background(), sampleTask("T-1", 1))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !outcome.Committed {
		t.Error("a passing run should commit")
	}
	if outcome.CommitHash == "" {
		t.Error("commit hash should be recorded")
	}
	if outcome.Repairs != 0 {
		t.Errorf("repairs: got %d, want 0", outcome.Repairs)
	}

	if got := h.memory.taskStatus("T-1"); got != "done" {
		t.Errorf("task status: got %q, want done", got)
	}

	entries := h.memory.changelogEntries()
	if len(entries) != 1 {
		t.Fatalf("changelog entries: got %d, want 1", len(entries))
	}
	if entries[0].TaskID != "T-1" || entries[0].Commit == "" {
		t.Errorf("changelog entry incomplete: %+v", entries[0])
	}

	if current := h.memory.currentTask(); current.Status != string(StageDone) {
		t.Errorf("current task status: got %q, want done", current.Status)
	}
}

// The central safety property: work that fails its gates must never be
// committed, no matter how many repair attempts were made.
func TestFailingGatesNeverCommit(t *testing.T) {
	alwaysFails := &scriptedGate{name: "test", failUntil: 1000}
	h := newHarness(t, []gates.Gate{alwaysFails}, sampleTask("T-1", 1))

	outcome, err := h.exec.Run(context.Background(), sampleTask("T-1", 1))
	if !errors.Is(err, ErrGatesFailed) {
		t.Fatalf("got %v, want ErrGatesFailed", err)
	}

	if outcome.Committed {
		t.Fatal("failing work was committed")
	}
	if h.git.commitCount() != 0 {
		t.Fatalf("git recorded %d commit(s) for failing work", h.git.commitCount())
	}

	if got := h.memory.taskStatus("T-1"); got != "blocked" {
		t.Errorf("a task that failed its gates should be blocked, got %q", got)
	}

	entries := h.memory.changelogEntries()
	if len(entries) != 0 {
		t.Errorf("failing work must not appear in the changelog: %+v", entries)
	}
}

func TestRepairLoopRetriesUntilGatesPass(t *testing.T) {
	failsOnce := &scriptedGate{name: "test", failUntil: 1}
	h := newHarness(t, []gates.Gate{failsOnce}, sampleTask("T-1", 1))

	outcome, err := h.exec.Run(context.Background(), sampleTask("T-1", 1))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if outcome.Repairs != 1 {
		t.Errorf("repairs: got %d, want 1", outcome.Repairs)
	}
	if !outcome.Committed {
		t.Error("work should commit once gates pass")
	}

	// One implement call plus one repair call.
	if h.claude.count() != 2 {
		t.Errorf("claude calls: got %d, want 2", h.claude.count())
	}
	if failsOnce.runCount() != 2 {
		t.Errorf("gate runs: got %d, want 2", failsOnce.runCount())
	}
}

func TestRepairAttemptsAreBounded(t *testing.T) {
	alwaysFails := &scriptedGate{name: "test", failUntil: 1000}
	h := newHarness(t, []gates.Gate{alwaysFails}, sampleTask("T-1", 1))

	outcome, _ := h.exec.Run(context.Background(), sampleTask("T-1", 1))

	if outcome.Repairs != 2 {
		t.Errorf("repairs: got %d, want the configured limit of 2", outcome.Repairs)
	}
	// One implement call plus two repairs.
	if h.claude.count() != 3 {
		t.Errorf("claude calls: got %d, want 3", h.claude.count())
	}
}

func TestZeroRepairAttemptsFailsImmediately(t *testing.T) {
	alwaysFails := &scriptedGate{name: "test", failUntil: 1000}
	h := newHarness(t, []gates.Gate{alwaysFails}, sampleTask("T-1", 1))
	h.exec.cfg.MaxRepairAttempts = 0

	outcome, err := h.exec.Run(context.Background(), sampleTask("T-1", 1))
	if !errors.Is(err, ErrGatesFailed) {
		t.Fatalf("got %v, want ErrGatesFailed", err)
	}
	if outcome.Repairs != 0 {
		t.Errorf("repairs: got %d, want 0", outcome.Repairs)
	}
	// Only the implement call.
	if h.claude.count() != 1 {
		t.Errorf("claude calls: got %d, want 1", h.claude.count())
	}
}

// Committing over a tree that was already dirty would sweep unrelated work
// into a commit describing something else.
func TestRefusesToStartOnDirtyTree(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()}, sampleTask("T-1", 1))
	h.git.clean = false

	_, err := h.exec.Run(context.Background(), sampleTask("T-1", 1))
	if !errors.Is(err, ErrDirtyTree) {
		t.Fatalf("got %v, want ErrDirtyTree", err)
	}

	if h.claude.count() != 0 {
		t.Error("no work should be attempted over a dirty tree")
	}
	if got := h.memory.taskStatus("T-1"); got != "new" {
		t.Errorf("task status should be untouched, got %q", got)
	}
}

func TestRefusesToStartWithConflicts(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()}, sampleTask("T-1", 1))
	h.git.conflict = true

	_, err := h.exec.Run(context.Background(), sampleTask("T-1", 1))
	if !errors.Is(err, ErrDirtyTree) {
		t.Fatalf("got %v, want ErrDirtyTree", err)
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Errorf("the error should name the conflict: %v", err)
	}
}

func TestImplementationFailureBlocksTask(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()}, sampleTask("T-1", 1))
	h.claude.results = []claudeReply{failReply("claude crashed")}

	_, err := h.exec.Run(context.Background(), sampleTask("T-1", 1))
	if err == nil {
		t.Fatal("a failed implementation must be reported")
	}

	if h.git.commitCount() != 0 {
		t.Error("nothing should be committed after a failed implementation")
	}
	if got := h.memory.taskStatus("T-1"); got != "blocked" {
		t.Errorf("task status: got %q, want blocked", got)
	}

	// The reason must be preserved for a human or a later run.
	current := h.memory.currentTask()
	if len(current.Errors) == 0 {
		t.Error("the failure reason should be recorded on the current task")
	}
}

func TestReportedErrorsAreTreatedAsFailure(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()}, sampleTask("T-1", 1))
	h.claude.results = []claudeReply{errorReply("could not find the file")}

	_, err := h.exec.Run(context.Background(), sampleTask("T-1", 1))
	if err == nil {
		t.Fatal("errors reported by Claude must not be ignored")
	}
	if !strings.Contains(err.Error(), "could not find the file") {
		t.Errorf("the reported error should surface: %v", err)
	}
}

// A task requiring no code change is legitimate; inventing an empty commit
// for it is not.
func TestNoChangesProducesNoCommit(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()}, sampleTask("T-1", 1))
	h.claude.onCall = nil // leave the tree clean

	outcome, err := h.exec.Run(context.Background(), sampleTask("T-1", 1))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if outcome.Committed {
		t.Error("no commit should be made when nothing changed")
	}
	if h.git.commitCount() != 0 {
		t.Error("an empty commit was created")
	}
	if got := h.memory.taskStatus("T-1"); got != "done" {
		t.Errorf("the task still completed, got status %q", got)
	}
}

func TestAutoCommitDisabledLeavesWorkUncommitted(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()}, sampleTask("T-1", 1))
	h.exec.cfg.AutoCommit = false

	outcome, err := h.exec.Run(context.Background(), sampleTask("T-1", 1))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if outcome.Committed {
		t.Error("nothing should be committed when auto-commit is off")
	}
	if h.git.commitCount() != 0 {
		t.Error("a commit was made despite auto-commit being off")
	}
	if got := h.memory.taskStatus("T-1"); got != "done" {
		t.Errorf("the task should still complete, got %q", got)
	}
}

func TestCheckpointsRecordedThroughTheRun(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()}, sampleTask("T-1", 1))

	if _, err := h.exec.Run(context.Background(), sampleTask("T-1", 1)); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	stages := h.checkpoints.stages()
	if len(stages) < 2 {
		t.Fatalf("expected checkpoints during and after the run, got %v", stages)
	}
	if stages[len(stages)-1] != string(StageDone) {
		t.Errorf("the final checkpoint should record completion, got %q", stages[len(stages)-1])
	}
}

func TestFailureIsCheckpointed(t *testing.T) {
	alwaysFails := &scriptedGate{name: "test", failUntil: 1000}
	h := newHarness(t, []gates.Gate{alwaysFails}, sampleTask("T-1", 1))

	h.exec.Run(context.Background(), sampleTask("T-1", 1))

	if h.checkpoints.count() == 0 {
		t.Error("a failed run must still leave recoverable state")
	}
}

func TestCommitMessageNamesTheTask(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()}, sampleTask("T-42", 1))

	if _, err := h.exec.Run(context.Background(), sampleTask("T-42", 1)); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	msg := h.git.lastMessage()
	if !strings.HasPrefix(msg, "T-42: ") {
		t.Errorf("commit subject should name the task: %q", msg)
	}
	if !strings.Contains(msg, "quality gates passed") {
		t.Errorf("commit should record that gates passed: %q", msg)
	}
}

func TestCancellationStopsTheRun(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()}, sampleTask("T-1", 1))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := h.exec.Run(ctx, sampleTask("T-1", 1))
	if err == nil {
		t.Fatal("a cancelled context should stop the run")
	}
	if h.git.commitCount() != 0 {
		t.Error("nothing should be committed after cancellation")
	}
}

func TestRepairPromptCarriesGateOutput(t *testing.T) {
	failsOnce := &scriptedGate{name: "test", failUntil: 1}
	h := newHarness(t, []gates.Gate{failsOnce}, sampleTask("T-1", 1))

	if _, err := h.exec.Run(context.Background(), sampleTask("T-1", 1)); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	repair := h.claude.promptAt(1)
	if !strings.Contains(repair, "expected 1, got 2") {
		t.Errorf("the repair prompt must carry the tool output:\n%s", repair)
	}
	if !strings.Contains(repair, "test failed") {
		t.Errorf("the repair prompt should name the failing gate:\n%s", repair)
	}
	// Weakening a gate is the cheapest way to make it pass, so the prompt
	// must rule it out explicitly.
	if !strings.Contains(repair, "weaken") {
		t.Errorf("the repair prompt must forbid weakening gates:\n%s", repair)
	}
}

func TestImplementPromptConstrainsScope(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()}, sampleTask("T-1", 1))

	if _, err := h.exec.Run(context.Background(), sampleTask("T-1", 1)); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	prompt := h.claude.promptAt(0)
	for _, want := range []string{"T-1", "Do not commit", "Change only what this task requires", ".ai/"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("implement prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestUnknownTaskIsRejected(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()}, sampleTask("T-1", 1))

	_, err := h.exec.Run(context.Background(), sampleTask("T-UNKNOWN", 1))
	if err == nil {
		t.Fatal("running a task absent from the backlog should fail")
	}
}

// The system's own state directory is rewritten on every run, so treating it
// as unrelated work would block every task.
func TestStateDirectoryChangesDoNotBlockStart(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()}, sampleTask("T-1", 1))
	h.git.clean = false
	h.git.dirtyPaths = []string{".ai/CURRENT.md", ".ai/BACKLOG.md"}

	if _, err := h.exec.Run(context.Background(), sampleTask("T-1", 1)); err != nil {
		t.Fatalf("changes confined to .ai/ should not block a run: %v", err)
	}
}

// Real work outside .ai/ must still block, even alongside state changes.
func TestUserChangesStillBlockStart(t *testing.T) {
	h := newHarness(t, []gates.Gate{passingGate()}, sampleTask("T-1", 1))
	h.git.clean = false
	h.git.dirtyPaths = []string{".ai/CURRENT.md", "internal/thing.go"}

	_, err := h.exec.Run(context.Background(), sampleTask("T-1", 1))
	if !errors.Is(err, ErrDirtyTree) {
		t.Fatalf("got %v, want ErrDirtyTree when real work is uncommitted", err)
	}
}
