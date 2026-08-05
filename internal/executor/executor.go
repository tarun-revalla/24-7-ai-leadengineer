// Package executor drives a single backlog task from selection to commit.
//
// The loop is: select, plan, implement, verify, repair, commit, record. Two
// rules shape everything else. Nothing is committed until every required
// quality gate passes, and the working tree must be clean before work starts
// so a commit can never sweep in changes the task did not make.
package executor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/gates"
	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// Stage names the phase a task is in. Persisted so an interrupted task can be
// resumed at the right point rather than restarted.
type Stage string

const (
	StagePlanning     Stage = "planning"
	StageImplementing Stage = "implementing"
	StageVerifying    Stage = "verifying"
	StageRepairing    Stage = "repairing"
	StageCommitting   Stage = "committing"
	StageDone         Stage = "done"
)

// Sentinel outcomes the caller distinguishes from ordinary failure.
var (
	// ErrNoWork reports an empty backlog. Not a failure.
	ErrNoWork = errors.New("no open tasks in the backlog")
	// ErrDirtyTree reports uncommitted changes present before work began.
	ErrDirtyTree = errors.New("working tree has uncommitted changes")
	// ErrGatesFailed reports that quality gates still failed after repairs.
	ErrGatesFailed = errors.New("quality gates failed")
)

// Claude runs prompts. Narrowed to what the executor needs so it can be
// substituted without standing up a session manager.
type Claude interface {
	LaunchSession(ctx context.Context, prompt string) (*interfaces.SessionResult, error)
}

// Git is the subset of repository operations the executor performs.
type Git interface {
	Status(ctx context.Context) (*interfaces.GitStatus, error)
	Stage(ctx context.Context, paths []string) error
	Commit(ctx context.Context, message string) (string, error)
	GetLastCommit(ctx context.Context) (*interfaces.GitCommit, error)
}

// Memory is the persistent project state the executor reads and updates.
type Memory interface {
	GetBacklog(ctx context.Context) (*interfaces.Backlog, error)
	SaveBacklog(ctx context.Context, b *interfaces.Backlog) error
	GetCurrent(ctx context.Context) (*interfaces.CurrentTask, error)
	SaveCurrent(ctx context.Context, c *interfaces.CurrentTask) error
	GetChangelog(ctx context.Context) (*interfaces.Changelog, error)
	SaveChangelog(ctx context.Context, c *interfaces.Changelog) error
	GetProject(ctx context.Context) (*interfaces.ProjectMetadata, error)
}

// Checkpoints records recoverable state.
type Checkpoints interface {
	CreateCheckpoint(ctx context.Context, state *interfaces.CheckpointState) (string, error)
}

// Logger records progress.
type Logger interface {
	Debug(msg string, fields ...interface{})
	Info(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
}

// Config supplies the executor's tunables.
type Config struct {
	// MaxRepairAttempts bounds how many times Claude is asked to fix failing
	// gates before the task is abandoned. Zero means no repair attempts.
	MaxRepairAttempts int
	// AutoCommit controls whether passing work is committed.
	AutoCommit bool
	// ProjectPath is the repository root.
	ProjectPath string
}

// Executor runs one task at a time.
type Executor struct {
	cfg         Config
	claude      Claude
	git         Git
	memory      Memory
	checkpoints Checkpoints
	gates       *gates.Runner
	log         Logger
}

// New creates an executor from its dependencies.
func New(
	cfg Config,
	claude Claude,
	git Git,
	memory Memory,
	checkpoints Checkpoints,
	gateRunner *gates.Runner,
	log Logger,
) *Executor {
	return &Executor{
		cfg:         cfg,
		claude:      claude,
		git:         git,
		memory:      memory,
		checkpoints: checkpoints,
		gates:       gateRunner,
		log:         log,
	}
}

// Outcome describes how a task run ended.
type Outcome struct {
	Task       interfaces.BacklogTask
	Committed  bool
	CommitHash string
	Report     *gates.Report
	Repairs    int
	Duration   time.Duration
	Checkpoint string
}

// RunNext selects the highest-priority open task and runs it to completion.
func (e *Executor) RunNext(ctx context.Context) (*Outcome, error) {
	task, err := e.selectTask(ctx)
	if err != nil {
		return nil, err
	}

	return e.Run(ctx, *task)
}

// Run drives one task through the full pipeline.
func (e *Executor) Run(ctx context.Context, task interfaces.BacklogTask) (*Outcome, error) {
	started := time.Now()
	outcome := &Outcome{Task: task}

	// A dirty tree makes it impossible to tell the task's changes from work
	// that was already there, and committing everything would sweep in
	// unrelated edits. Refuse rather than guess.
	if err := e.requireCleanTree(ctx); err != nil {
		return nil, err
	}

	e.log.Info("starting task", "task", task.ID, "title", task.Title)

	if err := e.markInProgress(ctx, task, StageImplementing); err != nil {
		return nil, err
	}

	if err := e.implement(ctx, task); err != nil {
		e.recordFailure(ctx, task, err)
		return outcome, err
	}

	report, repairs, err := e.verifyAndRepair(ctx, task)
	outcome.Report = report
	outcome.Repairs = repairs
	if err != nil {
		e.recordFailure(ctx, task, err)
		return outcome, err
	}

	if e.cfg.AutoCommit {
		hash, err := e.commit(ctx, task)
		if err != nil {
			e.recordFailure(ctx, task, err)
			return outcome, err
		}
		outcome.Committed = hash != ""
		outcome.CommitHash = hash
	}

	if err := e.recordCompletion(ctx, task, outcome.CommitHash); err != nil {
		return outcome, err
	}

	outcome.Duration = time.Since(started)

	id, err := e.checkpoint(ctx, task, StageDone, 1.0, outcome.CommitHash)
	if err != nil {
		e.log.Warn("failed to checkpoint completed task", "task", task.ID, "error", err)
	}
	outcome.Checkpoint = id

	e.log.Info("task complete",
		"task", task.ID,
		"committed", outcome.Committed,
		"repairs", repairs,
		"duration", outcome.Duration.String(),
	)

	return outcome, nil
}

// selectTask returns the highest-priority open task.
func (e *Executor) selectTask(ctx context.Context) (*interfaces.BacklogTask, error) {
	backlog, err := e.memory.GetBacklog(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read backlog: %w", err)
	}

	open := make([]interfaces.BacklogTask, 0, len(backlog.Tasks))
	for _, t := range backlog.Tasks {
		if IsOpenStatus(t.Status) {
			open = append(open, t)
		}
	}

	if len(open) == 0 {
		return nil, ErrNoWork
	}

	// Stable sort on priority alone keeps backlog order as the tie-break, so
	// selection is deterministic for a given backlog.
	sort.SliceStable(open, func(i, j int) bool {
		return open[i].Priority < open[j].Priority
	})

	return &open[0], nil
}

// StateDirPrefix is the system's own state directory, relative to the repo.
const StateDirPrefix = ".ai/"

// requireCleanTree refuses to start work over uncommitted changes.
func (e *Executor) requireCleanTree(ctx context.Context) error {
	status, err := e.git.Status(ctx)
	if err != nil {
		return fmt.Errorf("failed to inspect the repository: %w", err)
	}

	if status.HasConflicts {
		return fmt.Errorf("%w: unresolved merge conflicts", ErrDirtyTree)
	}

	clean, staged, modified, untracked := RealWorkStatus(status)
	if !clean {
		return fmt.Errorf("%w: %d staged, %d modified, %d untracked; commit or stash before starting",
			ErrDirtyTree, staged, modified, untracked)
	}

	return nil
}

// RealWorkStatus reports how many paths outside the system's state directory
// are staged, modified, or untracked, and whether that leaves the tree clean.
// It does not look at conflicts — HasConflicts on the underlying status is a
// separate, more specific signal callers should check first, since "there is
// a conflict" and "there are N unrelated modified files" call for different
// guidance.
//
// Exported so any caller that needs to judge tree cleanliness — the recovery
// analyzer, in particular — uses this exact rule rather than re-deriving it.
// The status/executor split over "open" backlog statuses drifted once before
// for precisely this reason; a dirty-tree definition duplicated in a second
// package would be the same mistake again.
func RealWorkStatus(status *interfaces.GitStatus) (clean bool, staged, modified, untracked int) {
	staged = excludingState(status.StagedChanges)
	modified = excludingState(status.UnstagedChanges)
	untracked = excludingState(status.UntrackedFiles)

	return staged+modified+untracked == 0, staged, modified, untracked
}

// excludingState counts paths outside the system's state directory.
//
// Changes under .ai/ are excluded. That directory is the system's own
// bookkeeping — the executor rewrites it on every run, and a human editing the
// backlog is queueing work rather than leaving unrelated edits behind. The
// guard exists to stop a task's commit from sweeping in someone else's
// in-progress work, and state the system owns is never that.
func excludingState(paths []string) int {
	n := 0
	for _, p := range paths {
		if !strings.HasPrefix(strings.TrimPrefix(p, "./"), StateDirPrefix) {
			n++
		}
	}
	return n
}

// implement asks Claude to carry out the task.
func (e *Executor) implement(ctx context.Context, task interfaces.BacklogTask) error {
	project, err := e.memory.GetProject(ctx)
	if err != nil {
		return fmt.Errorf("failed to read project metadata: %w", err)
	}

	prompt := BuildImplementPrompt(project, task)

	e.log.Info("implementing", "task", task.ID)

	result, err := e.claude.LaunchSession(ctx, prompt)
	if err != nil {
		return fmt.Errorf("implementation failed: %w", err)
	}
	if len(result.Errors) > 0 {
		return fmt.Errorf("implementation reported errors: %s", strings.Join(result.Errors, "; "))
	}

	if _, err := e.checkpoint(ctx, task, StageImplementing, 0.5, ""); err != nil {
		e.log.Warn("failed to checkpoint after implementation", "task", task.ID, "error", err)
	}

	return nil
}

// verifyAndRepair runs the gates, asking Claude to fix failures up to the
// configured limit. It returns the final report.
func (e *Executor) verifyAndRepair(
	ctx context.Context,
	task interfaces.BacklogTask,
) (*gates.Report, int, error) {
	if err := e.updateStage(ctx, task, StageVerifying, 0.6); err != nil {
		return nil, 0, err
	}

	report := e.gates.Run(ctx, e.cfg.ProjectPath)
	if report.Passed() {
		e.log.Info("quality gates passed", "task", task.ID)
		return report, 0, nil
	}

	for attempt := 1; attempt <= e.cfg.MaxRepairAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return report, attempt - 1, err
		}

		e.log.Warn("quality gates failed; attempting repair",
			"task", task.ID,
			"attempt", attempt,
			"failures", len(report.Failures()),
		)

		if err := e.updateStage(ctx, task, StageRepairing, 0.6); err != nil {
			return report, attempt - 1, err
		}

		result, err := e.claude.LaunchSession(ctx, BuildRepairPrompt(task, report))
		if err != nil {
			return report, attempt, fmt.Errorf("repair attempt %d failed: %w", attempt, err)
		}
		if len(result.Errors) > 0 {
			return report, attempt, fmt.Errorf("repair attempt %d reported errors: %s",
				attempt, strings.Join(result.Errors, "; "))
		}

		report = e.gates.Run(ctx, e.cfg.ProjectPath)
		if report.Passed() {
			e.log.Info("quality gates passed after repair", "task", task.ID, "attempts", attempt)
			return report, attempt, nil
		}
	}

	// Gates still failing. The work is left in the tree for a human to inspect
	// rather than committed or discarded.
	return report, e.cfg.MaxRepairAttempts, fmt.Errorf("%w after %d repair attempt(s): %s",
		ErrGatesFailed, e.cfg.MaxRepairAttempts, gateNames(report.Failures()))
}

// commit stages and commits the task's work.
func (e *Executor) commit(ctx context.Context, task interfaces.BacklogTask) (string, error) {
	if err := e.updateStage(ctx, task, StageCommitting, 0.9); err != nil {
		return "", err
	}

	status, err := e.git.Status(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to inspect the repository: %w", err)
	}

	if status.IsClean {
		// Gates passed and nothing changed: the task required no code change.
		// Recording that is honest; inventing an empty commit is not.
		e.log.Info("no changes to commit", "task", task.ID)
		return "", nil
	}

	if err := e.git.Stage(ctx, []string{"."}); err != nil {
		return "", fmt.Errorf("failed to stage changes: %w", err)
	}

	hash, err := e.git.Commit(ctx, BuildCommitMessage(task))
	if err != nil {
		return "", fmt.Errorf("failed to commit: %w", err)
	}

	// Commit returns the short hash parsed from git's output; the full hash is
	// more useful downstream and is authoritative.
	if full, err := e.git.GetLastCommit(ctx); err == nil && full.Hash != "" {
		hash = full.Hash
	}

	e.log.Info("committed", "task", task.ID, "commit", hash)
	return hash, nil
}

// markInProgress records the task as started.
func (e *Executor) markInProgress(ctx context.Context, task interfaces.BacklogTask, stage Stage) error {
	current := &interfaces.CurrentTask{
		TaskID:    task.ID,
		Title:     task.Title,
		Plan:      task.Description,
		Status:    string(stage),
		Progress:  0.1,
		StartedAt: time.Now().UTC(),
	}

	if err := e.memory.SaveCurrent(ctx, current); err != nil {
		return fmt.Errorf("failed to record the current task: %w", err)
	}

	return e.setBacklogStatus(ctx, task.ID, "in-progress")
}

// updateStage advances the recorded stage and progress.
func (e *Executor) updateStage(
	ctx context.Context,
	task interfaces.BacklogTask,
	stage Stage,
	progress float64,
) error {
	current, err := e.memory.GetCurrent(ctx)
	if err != nil {
		return fmt.Errorf("failed to read the current task: %w", err)
	}

	current.Status = string(stage)
	current.Progress = progress

	if err := e.memory.SaveCurrent(ctx, current); err != nil {
		return fmt.Errorf("failed to update the current task: %w", err)
	}

	return nil
}

// recordFailure preserves what went wrong so a later run, or a human, can see
// why the task stopped.
func (e *Executor) recordFailure(ctx context.Context, task interfaces.BacklogTask, cause error) {
	current, err := e.memory.GetCurrent(ctx)
	if err != nil {
		e.log.Error("failed to read the current task while recording a failure", "error", err)
		return
	}

	current.Errors = append(current.Errors, cause.Error())

	if err := e.memory.SaveCurrent(ctx, current); err != nil {
		e.log.Error("failed to record the failure", "task", task.ID, "error", err)
	}

	// The task returns to the backlog as blocked rather than in-progress, so a
	// later run does not silently pick up work that already failed once.
	if err := e.setBacklogStatus(ctx, task.ID, "blocked"); err != nil {
		e.log.Error("failed to mark the task blocked", "task", task.ID, "error", err)
	}

	if _, err := e.checkpoint(ctx, task, Stage(current.Status), current.Progress, ""); err != nil {
		e.log.Warn("failed to checkpoint after a failure", "task", task.ID, "error", err)
	}

	e.log.Error("task failed", "task", task.ID, "error", cause)
}

// recordCompletion moves the task out of the backlog and into the changelog.
func (e *Executor) recordCompletion(ctx context.Context, task interfaces.BacklogTask, commit string) error {
	changelog, err := e.memory.GetChangelog(ctx)
	if err != nil {
		return fmt.Errorf("failed to read the changelog: %w", err)
	}

	// Newest first, matching how the file is read.
	changelog.Entries = append([]interfaces.ChangelogEntry{{
		TaskID:  task.ID,
		Title:   task.Title,
		Date:    time.Now().UTC(),
		Commit:  commit,
		Summary: task.Description,
	}}, changelog.Entries...)

	if err := e.memory.SaveChangelog(ctx, changelog); err != nil {
		return fmt.Errorf("failed to update the changelog: %w", err)
	}

	if err := e.setBacklogStatus(ctx, task.ID, "done"); err != nil {
		return err
	}

	current := &interfaces.CurrentTask{Status: string(StageDone), Progress: 1.0}
	if err := e.memory.SaveCurrent(ctx, current); err != nil {
		return fmt.Errorf("failed to clear the current task: %w", err)
	}

	return nil
}

// setBacklogStatus updates one task's status in place.
func (e *Executor) setBacklogStatus(ctx context.Context, taskID, status string) error {
	backlog, err := e.memory.GetBacklog(ctx)
	if err != nil {
		return fmt.Errorf("failed to read backlog: %w", err)
	}

	found := false
	for i := range backlog.Tasks {
		if backlog.Tasks[i].ID == taskID {
			backlog.Tasks[i].Status = status
			backlog.Tasks[i].UpdatedAt = time.Now().UTC()
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("task %s is not in the backlog", taskID)
	}

	if err := e.memory.SaveBacklog(ctx, backlog); err != nil {
		return fmt.Errorf("failed to update backlog: %w", err)
	}

	return nil
}

// checkpoint records recoverable state for the task.
func (e *Executor) checkpoint(
	ctx context.Context,
	task interfaces.BacklogTask,
	stage Stage,
	progress float64,
	commit string,
) (string, error) {
	return e.checkpoints.CreateCheckpoint(ctx, &interfaces.CheckpointState{
		TaskID:    task.ID,
		TaskState: string(stage),
		Progress:  progress,
		GitCommit: commit,
	})
}

// IsOpenStatus reports whether a backlog task status counts as actionable
// work. It is the single source of truth for "open": both task selection here
// and the status report use it, so a task the executor will not run is never
// shown to an operator as ready to run.
func IsOpenStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done", "completed", "blocked":
		return false
	default:
		return true
	}
}

func gateNames(results []gates.Result) string {
	names := make([]string, 0, len(results))
	for _, r := range results {
		names = append(names, r.Gate)
	}
	return strings.Join(names, ", ")
}
