// Package recovery diagnoses whether a project was left mid-task by a crash
// or interruption, and tells an operator what to do about it.
//
// It is deliberately read-only. The one crash signature the executor can
// leave behind — a backlog task stuck at status "in-progress" — has two
// possible working-tree states: clean, in which case `start` alone resumes
// the task safely, or dirty, in which case the uncommitted changes might be
// partial work worth keeping. Automatically discarding them would risk
// exactly the repository corruption this system exists to prevent, so
// resolving a dirty tree is left to the operator's own git commands. This
// package's job is to make that judgment call easy, not to make it for them.
package recovery

import (
	"context"
	"errors"
	"fmt"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/checkpoint"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/executor"
	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// interruptedStatus is the one backlog status the executor never leaves a
// task in when it exits normally — including on failure, which moves a task
// to "blocked". Finding a task still at this status means the process ended
// (crash, kill, power loss) before it could record any outcome at all.
const interruptedStatus = "in-progress"

// Git is the subset of repository state recovery needs to judge.
type Git interface {
	Status(ctx context.Context) (*interfaces.GitStatus, error)
}

// Memory is the subset of project state recovery reads.
type Memory interface {
	GetBacklog(ctx context.Context) (*interfaces.Backlog, error)
	GetCurrent(ctx context.Context) (*interfaces.CurrentTask, error)
}

// Checkpoints is the subset of checkpoint operations recovery reads.
type Checkpoints interface {
	GetLatestCheckpoint(ctx context.Context) (*interfaces.CheckpointState, error)
	ListCheckpoints(ctx context.Context) ([]interfaces.CheckpointMetadata, error)
	ValidateCheckpoint(ctx context.Context, id string) error
}

// Analyzer diagnoses interrupted work.
type Analyzer struct {
	git         Git
	memory      Memory
	checkpoints Checkpoints
}

// New creates an Analyzer from its dependencies.
func New(git Git, memory Memory, checkpoints Checkpoints) *Analyzer {
	return &Analyzer{git: git, memory: memory, checkpoints: checkpoints}
}

// Report is the result of one analysis pass.
type Report struct {
	// Interrupted is true when a backlog task was left at "in-progress".
	Interrupted bool
	Task        *interfaces.BacklogTask
	Current     *interfaces.CurrentTask

	TreeClean      bool
	HasConflicts   bool
	StagedCount    int
	ModifiedCount  int
	UntrackedCount int
	GitErr         error

	LatestCheckpoint   *interfaces.CheckpointState
	CheckpointCount    int
	CorruptCheckpoints int
	CheckpointErr      error

	// SafeToResume is true when there is nothing standing between the
	// project and simply running `start` again.
	SafeToResume bool
}

// Analyze inspects the project for signs of interrupted work.
//
// Each section is best-effort: a failure reading one does not prevent
// reporting the others, since an operator diagnosing a broken project needs
// to see everything that still works, not have the whole report withheld
// because one part failed.
func (a *Analyzer) Analyze(ctx context.Context) (*Report, error) {
	r := &Report{}

	backlog, err := a.memory.GetBacklog(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read the backlog: %w", err)
	}
	for i := range backlog.Tasks {
		if backlog.Tasks[i].Status == interruptedStatus {
			r.Interrupted = true
			r.Task = &backlog.Tasks[i]
			break
		}
	}

	if current, err := a.memory.GetCurrent(ctx); err == nil {
		r.Current = current
	}

	if status, err := a.git.Status(ctx); err != nil {
		r.GitErr = err
	} else {
		r.HasConflicts = status.HasConflicts
		if !status.HasConflicts {
			r.TreeClean, r.StagedCount, r.ModifiedCount, r.UntrackedCount = executor.RealWorkStatus(status)
		}
	}

	latest, err := a.checkpoints.GetLatestCheckpoint(ctx)
	switch {
	case err == nil:
		r.LatestCheckpoint = latest
	case errors.Is(err, checkpoint.ErrNoCheckpoints):
		// No checkpoints yet is normal for a project that has not run.
	default:
		r.CheckpointErr = err
	}

	if list, err := a.checkpoints.ListCheckpoints(ctx); err == nil {
		r.CheckpointCount = len(list)
		for _, md := range list {
			if verr := a.checkpoints.ValidateCheckpoint(ctx, md.ID); verr != nil {
				r.CorruptCheckpoints++
			}
		}
	}

	r.SafeToResume = !r.Interrupted || (r.GitErr == nil && !r.HasConflicts && r.TreeClean)

	return r, nil
}
