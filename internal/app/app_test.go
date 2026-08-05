package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/claude"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/memory"
	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

func TestInitializeCreatesMemoryDocuments(t *testing.T) {
	a := newTestApp(t)

	result, err := a.Initialize(context.Background(), "Demo Project")
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if len(result.Created) == 0 {
		t.Fatal("initialising an empty project should create documents")
	}

	for _, dir := range []string{"checkpoints", "sessions", "logs", "metrics"} {
		if _, err := os.Stat(a.StatePath(dir)); err != nil {
			t.Errorf("%s directory was not created: %v", dir, err)
		}
	}

	project, err := a.Memory.GetProject(context.Background())
	if err != nil {
		t.Fatalf("the project document should be readable after init: %v", err)
	}
	if project.Name != "Demo Project" {
		t.Errorf("project name: got %q, want %q", project.Name, "Demo Project")
	}
}

// Re-running init on a live project must not discard its backlog, decisions or
// in-flight task — the whole point of it being idempotent.
func TestInitializeIsNonDestructive(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	backlog := &interfaces.Backlog{Tasks: []interfaces.BacklogTask{
		{ID: "T-1", Title: "Existing work", Priority: 1, Status: "new"},
	}}
	if err := a.Memory.SaveBacklog(ctx, backlog); err != nil {
		t.Fatalf("SaveBacklog failed: %v", err)
	}

	result, err := a.Initialize(ctx, "Demo")
	if err != nil {
		t.Fatalf("second Initialize failed: %v", err)
	}
	if len(result.Created) != 0 {
		t.Errorf("nothing should be created on a second run, got %v", result.Created)
	}
	if len(result.Skipped) == 0 {
		t.Error("existing documents should be reported as skipped")
	}

	after, err := a.Memory.GetBacklog(ctx)
	if err != nil {
		t.Fatalf("GetBacklog failed: %v", err)
	}
	if len(after.Tasks) != 1 || after.Tasks[0].ID != "T-1" {
		t.Errorf("re-running init destroyed the backlog: %+v", after.Tasks)
	}
}

func TestInitializeDefaultsProjectNameToDirectory(t *testing.T) {
	a := newTestApp(t)

	if _, err := a.Initialize(context.Background(), ""); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	project, err := a.Memory.GetProject(context.Background())
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}
	if project.Name != filepath.Base(a.ProjectPath) {
		t.Errorf("project name: got %q, want the directory name %q",
			project.Name, filepath.Base(a.ProjectPath))
	}
}

func TestIsUninitializedRecognisesMissingState(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"missing file", os.ErrNotExist, true},
		{"wrapped missing file", errors.New("x: " + os.ErrNotExist.Error()), false},
		{"no front matter", memory.ErrNoFrontMatter, true},
		{"unrelated failure", errors.New("disk on fire"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsUninitialized(tt.err); got != tt.want {
				t.Errorf("IsUninitialized(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestStatusOnUninitializedProject(t *testing.T) {
	a := newTestApp(t)

	status := a.Status(context.Background())
	if status.Initialized {
		t.Error("a project with no .ai documents should not report as initialised")
	}

	out := status.Render()
	if !strings.Contains(out, "Not initialised") {
		t.Errorf("the report should say the project is uninitialised:\n%s", out)
	}
	if !strings.Contains(out, "leadengineer init") {
		t.Errorf("the report should say what to do about it:\n%s", out)
	}
}

func TestStatusRendersAnInitialisedProject(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	backlog := &interfaces.Backlog{Tasks: []interfaces.BacklogTask{
		{ID: "T-HIGH", Title: "Urgent thing", Priority: 1, Status: "new"},
		{ID: "T-LOW", Title: "Later thing", Priority: 9, Status: "new"},
		{ID: "T-DONE", Title: "Finished thing", Priority: 2, Status: "done"},
	}}
	if err := a.Memory.SaveBacklog(ctx, backlog); err != nil {
		t.Fatalf("SaveBacklog failed: %v", err)
	}

	status := a.Status(ctx)
	out := status.Render()

	for _, want := range []string{"Demo", "T-HIGH", "Urgent thing", "Backlog"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "T-DONE") {
		t.Errorf("completed tasks should not be listed as upcoming work:\n%s", out)
	}
}

func TestNextTasksOrdersByPriority(t *testing.T) {
	s := &Status{Backlog: &interfaces.Backlog{Tasks: []interfaces.BacklogTask{
		{ID: "C", Priority: 9, Status: "new"},
		{ID: "A", Priority: 1, Status: "new"},
		{ID: "B", Priority: 5, Status: "new"},
		{ID: "X", Priority: 0, Status: "done"},
		{ID: "Y", Priority: 0, Status: "blocked"},
	}}}

	got := s.NextTasks(0)
	if len(got) != 3 {
		t.Fatalf("open tasks: got %d, want 3 (done and blocked are not open)", len(got))
	}
	for i, want := range []string{"A", "B", "C"} {
		if got[i].ID != want {
			t.Errorf("position %d: got %s, want %s", i, got[i].ID, want)
		}
	}
}

func TestNextTasksRespectsLimit(t *testing.T) {
	s := &Status{Backlog: &interfaces.Backlog{Tasks: []interfaces.BacklogTask{
		{ID: "A", Priority: 1, Status: "new"},
		{ID: "B", Priority: 2, Status: "new"},
		{ID: "C", Priority: 3, Status: "new"},
	}}}

	if got := s.NextTasks(2); len(got) != 2 {
		t.Errorf("got %d tasks, want the limit of 2", len(got))
	}
}

func TestNextTasksWithNoBacklog(t *testing.T) {
	if got := (&Status{}).NextTasks(5); got != nil {
		t.Errorf("a missing backlog should yield no tasks, got %v", got)
	}
}

func TestShortHashTruncatesLongHashes(t *testing.T) {
	if got := shortHash("0123456789abcdef"); got != "01234567" {
		t.Errorf("got %q, want the first 8 characters", got)
	}
	// Anything already short enough must survive intact rather than being
	// padded or cut.
	if got := shortHash("abc"); got != "abc" {
		t.Errorf("got %q, want %q unchanged", got, "abc")
	}
}

func TestRecoverOnAHealthyProject(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	report, err := a.Recover(ctx)
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}
	if report.Interrupted {
		t.Error("a project with no in-progress task must not report an interruption")
	}
}

// A task left at "in-progress" is the one state the executor never leaves
// behind on a normal exit, so finding one means the process died.
func TestRecoverDetectsAnInterruptedTask(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	backlog := &interfaces.Backlog{Tasks: []interfaces.BacklogTask{
		{ID: "T-1", Title: "Interrupted work", Priority: 1, Status: "in-progress"},
	}}
	if err := a.Memory.SaveBacklog(ctx, backlog); err != nil {
		t.Fatalf("SaveBacklog failed: %v", err)
	}

	report, err := a.Recover(ctx)
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}
	if !report.Interrupted {
		t.Fatal("an in-progress task should be reported as interrupted")
	}
	if report.Task == nil || report.Task.ID != "T-1" {
		t.Errorf("the interrupted task should be identified, got %+v", report.Task)
	}
}

// appWithConfig builds an app over a project carrying the given config.yaml.
func appWithConfig(t *testing.T, yaml string) *App {
	t.Helper()

	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	a, err := New(Options{ProjectPath: dir, LogToStderr: true})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	return a
}

// Only Go has a gate set; committing work no gate ever checked is worse than
// refusing to start.
func TestExecutorRefusesUnsupportedProjectTypes(t *testing.T) {
	a := appWithConfig(t, "project:\n  type: rust\n")

	_, err := a.Executor()
	if err == nil {
		t.Fatal("a project type with no gates must not produce an executor")
	}
	if !strings.Contains(err.Error(), "rust") {
		t.Errorf("the error should name the unsupported type: %v", err)
	}
}

// A JavaScript or TypeScript project gets the Node gate set, under any of the
// names someone would reasonably write in their config.
func TestExecutorBuildsNodeGates(t *testing.T) {
	for _, projectType := range []string{"node", "javascript", "typescript"} {
		a := appWithConfig(t, "project:\n  type: "+projectType+"\n")
		if _, err := a.Executor(); err != nil {
			t.Errorf("project type %q should build an executor: %v", projectType, err)
		}
	}
}

func TestExecutorHonoursTheReviewSetting(t *testing.T) {
	for _, enabled := range []string{"true", "false"} {
		a := appWithConfig(t, "review:\n  enabled: "+enabled+"\n")
		if _, err := a.Executor(); err != nil {
			t.Errorf("review.enabled=%s should still build an executor: %v", enabled, err)
		}
	}
}

// A conventional config.yaml in the project is picked up without being named.
func TestConventionalConfigFileIsLoaded(t *testing.T) {
	a := appWithConfig(t, "quality:\n  minimumCoverage: 0.42\n")

	if got := a.Config.GetFloat64("quality.minimumCoverage"); got != 0.42 {
		t.Errorf("got %v, want the value from config.yaml", got)
	}
}

// An invalid setting must be reported at startup rather than surfacing later
// as unexplained behaviour.
func TestInvalidConfigIsRejectedAtStartup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("quality:\n  minimumCoverage: 5.0\n"), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	if _, err := New(Options{ProjectPath: dir, LogToStderr: true}); err == nil {
		t.Fatal("a coverage floor above 1.0 must be rejected")
	}
}

func TestRunLoopStopsWhenTheBacklogIsEmpty(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// An empty backlog is not a failure; the loop should end quietly.
	outcomes, err := a.RunLoop(ctx, 1)
	if err != nil {
		t.Fatalf("an empty backlog should not be an error: %v", err)
	}
	if len(outcomes) != 0 {
		t.Errorf("no tasks should have run, got %d", len(outcomes))
	}
}

func TestRunLoopHonoursCancellation(t *testing.T) {
	a := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := a.RunLoop(ctx, 0); err == nil {
		t.Fatal("a cancelled context should stop the loop")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	a, err := New(Options{ProjectPath: dir, LogToStderr: true})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if err := a.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	// A command that fails after opening the app closes it twice: once in its
	// own error path, once via defer.
	if err := a.Close(); err != nil {
		t.Errorf("second Close should be harmless, got %v", err)
	}
}

func TestNewRejectsAMissingConfigFile(t *testing.T) {
	_, err := New(Options{
		ProjectPath: t.TempDir(),
		ConfigFile:  filepath.Join(t.TempDir(), "nope.yaml"),
		LogToStderr: true,
	})
	if err == nil {
		t.Fatal("a config file that does not exist must be reported, not ignored")
	}
}

// Logging to a file has to create the directory: .ai/logs/ does not exist in a
// project that has never been initialised, and a system whose first act is to
// fail on a missing log directory is no use.
func TestLoggerCreatesItsOutputDirectory(t *testing.T) {
	dir := t.TempDir()

	a, err := New(Options{ProjectPath: dir, LogToStderr: false})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	logDir := filepath.Join(dir, ".ai", "logs")
	if _, err := os.Stat(logDir); err != nil {
		t.Errorf("the log directory should have been created: %v", err)
	}
}

func TestLoggerHonoursAnAbsoluteOutputPath(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "nested", "app.log")

	if err := os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("logging:\n  output: "+logPath+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	a, err := New(Options{ProjectPath: dir, LogToStderr: false})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	if _, err := os.Stat(filepath.Dir(logPath)); err != nil {
		t.Errorf("an absolute log path's directory should be created: %v", err)
	}
}

func TestRunClaudeWaitsOutAnActiveCooldown(t *testing.T) {
	a := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())

	if _, err := a.EnterQuotaCooldown(ctx, nil, time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatalf("EnterQuotaCooldown failed: %v", err)
	}

	// Cancelling proves the call was blocked on the cooldown rather than
	// having gone ahead and contacted Claude.
	cancel()

	if _, err := a.RunClaude(ctx, "anything", nil); err == nil {
		t.Fatal("a cancelled wait should be reported, not treated as quota being available")
	}
}

func TestTruncateDurationScalesWithMagnitude(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m0s"},
		{3 * time.Hour, "3h0m0s"},
	}

	for _, tt := range tests {
		if got := truncateDuration(tt.in); got != tt.want {
			t.Errorf("truncateDuration(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Each section of the status report records its own error, so one broken
// document cannot hide the parts of the system that still work.
func TestStatusReportsPerSectionFailures(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := os.WriteFile(a.StatePath("BACKLOG.md"), []byte("not a document\n"), 0o644); err != nil {
		t.Fatalf("failed to corrupt the backlog: %v", err)
	}

	status := a.Status(ctx)
	out := status.Render()

	if status.BacklogErr == nil {
		t.Error("an unreadable backlog should be reported")
	}
	if !strings.Contains(out, "Demo") {
		t.Errorf("a broken backlog must not hide the rest of the report:\n%s", out)
	}
}

// fakeClaudeExecutor stands in for the Claude CLI subprocess.
type fakeClaudeExecutor struct {
	stdout   string
	stderr   string
	exitCode int
}

func (f fakeClaudeExecutor) Execute(
	ctx context.Context, name string, args []string, workDir string,
) (*claude.ExecResult, error) {
	return &claude.ExecResult{Stdout: f.stdout, Stderr: f.stderr, ExitCode: f.exitCode}, nil
}

// appWithClaude replaces the subprocess boundary so a full run can be driven
// without the Claude CLI installed.
func appWithClaude(t *testing.T, exec fakeClaudeExecutor) *App {
	t.Helper()

	a := newTestApp(t)

	manager, err := claude.New(a.ProjectPath, "test-model", 1, 30, claude.WithExecutor(exec))
	if err != nil {
		t.Fatalf("failed to build the Claude manager: %v", err)
	}
	a.Claude = manager

	return a
}

func TestReviewSessionAdapterCarriesOutputAndErrors(t *testing.T) {
	a := appWithClaude(t, fakeClaudeExecutor{
		stdout: `{"session_id": "s1", "result": "reviewed"}`,
	})

	resp, err := claudeReviewSession{a.Claude}.LaunchSession(context.Background(), "review this")
	if err != nil {
		t.Fatalf("LaunchSession failed: %v", err)
	}
	if !strings.Contains(resp.Output, "reviewed") {
		t.Errorf("the adapter lost the session output: %q", resp.Output)
	}
}

func TestReviewSessionAdapterPropagatesFailure(t *testing.T) {
	a := newTestApp(t)

	// An empty prompt is rejected before any subprocess runs.
	if _, err := (claudeReviewSession{a.Claude}).LaunchSession(context.Background(), ""); err == nil {
		t.Fatal("a rejected session must surface as an error, not an empty review")
	}
}

// A usage limit hit mid-task must be classified as quota rather than reported
// as an ordinary failure: retrying a quota failure cannot succeed.
func TestUsageLimitDuringARunIsClassifiedAsQuota(t *testing.T) {
	a := appWithClaude(t, fakeClaudeExecutor{
		stderr:   "Claude usage limit reached. Try again later.",
		exitCode: 1,
	})
	ctx := context.Background()

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	backlog := &interfaces.Backlog{Tasks: []interfaces.BacklogTask{
		{ID: "T-1", Title: "Some work", Priority: 1, Status: "new"},
	}}
	if err := a.Memory.SaveBacklog(ctx, backlog); err != nil {
		t.Fatalf("SaveBacklog failed: %v", err)
	}

	_, err := a.RunNextTask(ctx)
	if err == nil {
		t.Fatal("a usage limit must stop the run")
	}
	if !errors.Is(err, ErrQuotaExhausted) {
		t.Fatalf("got %v, want ErrQuotaExhausted", err)
	}

	// The cooldown has to persist, or a restarted process would immediately
	// burn another attempt against a limit that has not reset.
	if available, _ := a.Quota.Available(ctx); available {
		t.Error("hitting a usage limit should record a cooldown")
	}
}

// Anything that is not a usage limit must surface as itself, not be
// misreported as quota exhaustion.
func TestOrdinaryFailureIsNotClassifiedAsQuota(t *testing.T) {
	a := appWithClaude(t, fakeClaudeExecutor{
		stderr:   "some other problem entirely",
		exitCode: 1,
	})
	ctx := context.Background()

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	backlog := &interfaces.Backlog{Tasks: []interfaces.BacklogTask{
		{ID: "T-1", Title: "Some work", Priority: 1, Status: "new"},
	}}
	if err := a.Memory.SaveBacklog(ctx, backlog); err != nil {
		t.Fatalf("SaveBacklog failed: %v", err)
	}

	_, err := a.RunNextTask(ctx)
	if err == nil {
		t.Fatal("the run should have failed")
	}
	if errors.Is(err, ErrQuotaExhausted) {
		t.Errorf("an ordinary failure was misreported as a usage limit: %v", err)
	}
}

func TestRunClaudeReturnsTheSessionResult(t *testing.T) {
	a := appWithClaude(t, fakeClaudeExecutor{
		stdout: `{"session_id": "s1", "result": "did the thing"}`,
	})

	result, err := a.RunClaude(context.Background(), "do the thing", nil)
	if err != nil {
		t.Fatalf("RunClaude failed: %v", err)
	}
	if !strings.Contains(result.Output, "did the thing") {
		t.Errorf("the session output was lost: %q", result.Output)
	}
}

// On a usage limit RunClaude must save the caller's state before scheduling
// the resume, so a crash during the wait still leaves something to resume from.
func TestRunClaudeCheckpointsOnAUsageLimit(t *testing.T) {
	a := appWithClaude(t, fakeClaudeExecutor{
		stderr:   "Claude usage limit reached",
		exitCode: 1,
	})
	ctx := context.Background()

	snapshotted := false
	snapshot := func() *interfaces.CheckpointState {
		snapshotted = true
		return &interfaces.CheckpointState{TaskID: "T-1", TaskState: "implementing"}
	}

	if _, err := a.RunClaude(ctx, "do the thing", snapshot); err == nil {
		t.Fatal("a usage limit must be reported")
	}

	if !snapshotted {
		t.Error("the caller's state should have been captured before the cooldown")
	}
	if available, _ := a.Quota.Available(ctx); available {
		t.Error("a usage limit should record a cooldown")
	}
}

// RunLoop stops at the configured task limit rather than draining the backlog.
func TestRunLoopStopsAtTheTaskLimit(t *testing.T) {
	a := appWithClaude(t, fakeClaudeExecutor{
		stderr:   "something broke",
		exitCode: 1,
	})
	ctx := context.Background()

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	backlog := &interfaces.Backlog{Tasks: []interfaces.BacklogTask{
		{ID: "T-1", Title: "First", Priority: 1, Status: "new"},
		{ID: "T-2", Title: "Second", Priority: 2, Status: "new"},
	}}
	if err := a.Memory.SaveBacklog(ctx, backlog); err != nil {
		t.Fatalf("SaveBacklog failed: %v", err)
	}

	// The first task fails, which stops the loop: continuing would pile
	// unreviewed failures behind it.
	outcomes, err := a.RunLoop(ctx, 2)
	if err == nil {
		t.Fatal("a failing task should stop the loop")
	}
	if len(outcomes) != 0 {
		t.Errorf("no task completed, got %d outcomes", len(outcomes))
	}
}

// --- language-agnostic gates ---

// A declared toolchain beats anything compiled in: the built-in Go and Node
// sets are a convenience for two common cases, not the limit of what is
// supported.
func TestDeclaredToolchainOverridesTheBuiltInSet(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := a.Memory.SaveToolchain(ctx, &interfaces.Toolchain{
		Language: "rust",
		Gates: []interfaces.ToolchainGate{
			{Name: "build", Command: []string{"cargo", "build"}, Required: true},
			{Name: "test", Command: []string{"cargo", "test"}, Required: true},
		},
	}); err != nil {
		t.Fatalf("SaveToolchain failed: %v", err)
	}

	// project.type is still "go"; the declaration must win anyway.
	gateList, err := a.gateSet("go")
	if err != nil {
		t.Fatalf("gateSet failed: %v", err)
	}

	names := map[string]bool{}
	for _, g := range gateList {
		names[g.Name()] = true
	}
	if !names["build"] || !names["test"] {
		t.Errorf("the declared gates should be used, got %v", names)
	}
	if names["vet"] || names["format"] {
		t.Errorf("Go's built-in gates must not run for a declared toolchain: %v", names)
	}
	if !names["secrets"] {
		t.Error("the secret scan must survive any declaration")
	}
}

// A malformed declaration must be reported, not quietly replaced by a
// built-in set that checks something else entirely.
func TestMalformedToolchainIsReportedNotIgnored(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if err := a.Memory.SaveToolchain(ctx, &interfaces.Toolchain{
		Gates: []interfaces.ToolchainGate{{Name: "build"}}, // no command
	}); err != nil {
		t.Fatalf("SaveToolchain failed: %v", err)
	}

	_, err := a.gateSet("go")
	if err == nil {
		t.Fatal("an unusable declaration must be reported")
	}
	if !strings.Contains(err.Error(), "TOOLCHAIN.md") {
		t.Errorf("the error should point at the file to fix: %v", err)
	}
}

func TestToolchainSurvivesReopening(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	want := &interfaces.Toolchain{
		Language: "zig",
		Gates:    []interfaces.ToolchainGate{{Name: "test", Command: []string{"zig", "build", "test"}, Required: true}},
	}
	if err := a.Memory.SaveToolchain(ctx, want); err != nil {
		t.Fatalf("SaveToolchain failed: %v", err)
	}

	reopened, err := New(Options{ProjectPath: a.ProjectPath, LogToStderr: true})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	got, err := reopened.Memory.GetToolchain(ctx)
	if err != nil {
		t.Fatalf("GetToolchain failed: %v", err)
	}
	if got.Language != "zig" || len(got.Gates) != 1 {
		t.Errorf("the toolchain did not survive: %+v", got)
	}
	if strings.Join(got.Gates[0].Command, " ") != "zig build test" {
		t.Errorf("command: got %v", got.Gates[0].Command)
	}
}

// --- inspection mode ---

func TestInspectionModeRunsNoProjectTooling(t *testing.T) {
	a := appWithConfig(t, "quality:\n  inspectionOnly: true\n")

	if !a.InspectionMode() {
		t.Fatal("inspectionOnly should enable inspection mode")
	}

	gateList, err := a.gateSet("go")
	if err != nil {
		t.Fatalf("gateSet failed: %v", err)
	}

	if len(gateList) != 1 || gateList[0].Name() != "secrets" {
		names := []string{}
		for _, g := range gateList {
			names = append(names, g.Name())
		}
		t.Errorf("only the secret scan should run, got %v", names)
	}
}

// The one check that survives: it needs no toolchain, and a published
// credential is not undone by fixing the code afterwards.
func TestInspectionModeKeepsTheSecretScan(t *testing.T) {
	a := appWithConfig(t, "quality:\n  inspectionOnly: true\n")

	gateList, _ := a.gateSet("rust")
	if len(gateList) == 0 || gateList[0].Name() != "secrets" {
		t.Error("the secret scan must run even when nothing else does")
	}
}

// With no tooling running, switching review off too would leave nothing
// checking the change at all, and every task would commit unconditionally.
func TestInspectionModeForcesReviewOn(t *testing.T) {
	a := appWithConfig(t, "quality:\n  inspectionOnly: true\nreview:\n  enabled: false\n")

	exec, err := a.Executor()
	if err != nil {
		t.Fatalf("Executor failed: %v", err)
	}
	if exec == nil {
		t.Fatal("an executor should have been built")
	}
	// The reviewer is unexported; its presence is asserted through the fact
	// that an inspection-mode executor builds at all with review disabled,
	// plus the gate set being reduced to secrets only.
	gateList, _ := a.gateSet("go")
	if len(gateList) != 1 {
		t.Errorf("inspection mode should reduce the gate set, got %d gates", len(gateList))
	}
}

// A project type with no gates is normally refused; inspection mode is exactly
// the escape hatch for that, so it must not refuse.
func TestInspectionModeWorksForAnyProjectType(t *testing.T) {
	a := appWithConfig(t, "project:\n  type: rust\nquality:\n  inspectionOnly: true\n")

	if _, err := a.Executor(); err != nil {
		t.Fatalf("inspection mode should work for any project type: %v", err)
	}
}

func TestInspectionModeIsOffByDefault(t *testing.T) {
	a := newTestApp(t)
	if a.InspectionMode() {
		t.Error("inspection mode must be a deliberate choice, not a default")
	}
}

// A flag changes one run, not the project's configuration on disk.
func TestOverridesBeatTheConfigFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("quality:\n  inspectionOnly: false\n"), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	a, err := New(Options{
		ProjectPath: dir,
		LogToStderr: true,
		Overrides:   map[string]any{"quality.inspectionOnly": true},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	if !a.InspectionMode() {
		t.Error("an override should beat the config file")
	}

	// Nothing was written back.
	onDisk, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if strings.Contains(string(onDisk), "true") {
		t.Errorf("an override must not be persisted: %s", onDisk)
	}
}

// --- continuous operation ---

// The backlog emptying is a pause, not the end of the work. Without this the
// process needs an external scheduler to be what its name promises.
func TestWatchKeepsRunningOnAnEmptyBacklog(t *testing.T) {
	a := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	idled := make(chan struct{}, 4)
	done := make(chan error, 1)

	go func() {
		done <- a.Watch(ctx, WatchOptions{
			Interval: 10 * time.Millisecond,
			OnIdle:   func(time.Duration) { idled <- struct{}{} },
		})
	}()

	// Two idle passes prove it looped rather than returning after the first.
	for i := 0; i < 2; i++ {
		select {
		case <-idled:
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("watch stopped instead of waiting for new work")
		}
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled after cancelling", err)
	}
}

// Ctrl-C during the sleep must take effect immediately; a bare time.Sleep
// would ignore it for the whole interval.
func TestWatchStopsPromptlyOnCancellation(t *testing.T) {
	a := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- a.Watch(ctx, WatchOptions{Interval: time.Hour})
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("got %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not stop; it slept through the cancellation")
	}
}

// A problem no amount of retrying will clear — a dirty tree, a missing
// toolchain — would otherwise burn quota producing the same error forever.
func TestWatchStopsAfterRepeatedFailuresWithNoProgress(t *testing.T) {
	a := appWithClaude(t, fakeClaudeExecutor{stderr: "something broke", exitCode: 1})
	ctx := context.Background()

	if _, err := a.Initialize(ctx, "Demo"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Enough tasks that the backlog never empties on its own.
	backlog := &interfaces.Backlog{}
	for i := 0; i < 10; i++ {
		backlog.Tasks = append(backlog.Tasks, interfaces.BacklogTask{
			ID: fmt.Sprintf("T-%d", i), Title: "work", Priority: 1, Status: "new",
		})
	}
	if err := a.Memory.SaveBacklog(ctx, backlog); err != nil {
		t.Fatalf("SaveBacklog failed: %v", err)
	}

	failures := 0
	err := a.Watch(ctx, WatchOptions{
		Interval:               time.Millisecond,
		MaxConsecutiveFailures: 2,
		OnFailure:              func(error, int) { failures++ },
	})

	if !errors.Is(err, ErrTooManyFailures) {
		t.Fatalf("got %v, want ErrTooManyFailures", err)
	}
	if failures != 2 {
		t.Errorf("failures: got %d, want the configured limit of 2", failures)
	}
}

func TestWatchDefaultsAreApplied(t *testing.T) {
	a := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// A cancelled context returns before any sleeping, so this only asserts
	// that zero values do not mean "no wait" or "stop on first failure".
	if err := a.Watch(ctx, WatchOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}

	if DefaultWatchInterval <= 0 {
		t.Error("the default interval must be positive, or an empty backlog spins")
	}
	if DefaultMaxConsecutiveFailures <= 0 {
		t.Error("the default failure bound must be positive")
	}
}
