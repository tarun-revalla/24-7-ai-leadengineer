package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/executor"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/gates"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/review"
	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// ErrNoWork reports an empty backlog, re-exported so callers need not import
// the executor package to recognise it.
var ErrNoWork = executor.ErrNoWork

// Executor builds a task executor from the wired subsystems.
//
// The gate set is chosen from the project type, so a Go repository is checked
// with Go tooling. Only Go is supported today; anything else gets an empty set
// rather than a set of checks that cannot apply, and RunTask refuses to
// proceed rather than committing unverified work.
func (a *App) Executor() (*executor.Executor, error) {
	projectType := a.Config.GetString("project.type")

	var gateList []gates.Gate
	switch projectType {
	case "go":
		minCoverage := 0.0
		if a.Config.GetBool("quality.requireTests") {
			minCoverage = a.Config.GetFloat64("quality.minimumCoverage")
		}
		gateList = gates.GoGates(minCoverage)
	default:
		return nil, fmt.Errorf("no quality gates are defined for project type %q; "+
			"committing unverified work is not permitted", projectType)
	}

	cfg := executor.Config{
		MaxRepairAttempts: a.Config.GetInt("claude.maxRetries"),
		MaxReviewAttempts: a.Config.GetInt("review.maxRevisions"),
		AutoCommit:        a.Config.GetBool("tasks.autoCommit"),
		ProjectPath:       a.ProjectPath,
	}

	// A nil reviewer disables the stage. The interface must be left nil rather
	// than set to a typed nil, which would satisfy the interface and then
	// panic on the first call.
	var reviewer executor.Reviewer
	if a.Config.GetBool("review.enabled") {
		reviewer = review.New(claudeReviewSession{a.Claude})
	}

	return executor.New(
		cfg,
		a.Claude,
		a.Git,
		a.Memory,
		a.Checkpoints,
		gates.NewRunner(gateList...),
		reviewer,
		a.Logger,
	), nil
}

// claudeReviewSession adapts the session manager to the narrow interface the
// review package needs, so review does not depend on the full session type.
type claudeReviewSession struct {
	claude interfaces.ClaudeSessionManager
}

func (c claudeReviewSession) LaunchSession(ctx context.Context, prompt string) (*review.Response, error) {
	result, err := c.claude.LaunchSession(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return &review.Response{Output: result.Output, Errors: result.Errors}, nil
}

// RunNextTask executes the highest-priority open task.
//
// Quota is awaited first, so a cooldown recorded by an earlier run — or by a
// process that has since restarted — is respected before any work begins.
func (a *App) RunNextTask(ctx context.Context) (*executor.Outcome, error) {
	if _, err := a.AwaitQuota(ctx); err != nil {
		return nil, err
	}

	exec, err := a.Executor()
	if err != nil {
		return nil, err
	}

	outcome, err := exec.RunNext(ctx)
	if err != nil {
		return outcome, a.classifyRunError(ctx, err)
	}

	if err := a.ClearQuota(ctx); err != nil {
		a.Logger.Warn("failed to clear quota state", "error", err)
	}

	return outcome, nil
}

// RunLoop executes tasks until the backlog empties, the context is cancelled,
// or a run fails.
//
// maxTasks bounds the run; zero means no bound. A failing task stops the loop
// rather than advancing to the next one: the failure is recorded on the task
// and continuing would pile unreviewed failures behind it.
func (a *App) RunLoop(ctx context.Context, maxTasks int) ([]*executor.Outcome, error) {
	var completed []*executor.Outcome

	for maxTasks <= 0 || len(completed) < maxTasks {
		if err := ctx.Err(); err != nil {
			return completed, err
		}

		outcome, err := a.RunNextTask(ctx)
		if err != nil {
			if errors.Is(err, ErrNoWork) {
				a.Logger.Info("backlog is empty", "completed", len(completed))
				return completed, nil
			}
			return completed, err
		}

		completed = append(completed, outcome)
	}

	return completed, nil
}

// classifyRunError converts a usage limit encountered mid-task into the
// quota sentinel, after recording the cooldown and saving the work.
func (a *App) classifyRunError(ctx context.Context, err error) error {
	failure, detectErr := a.Claude.DetectFailure(ctx)
	if detectErr != nil || failure == "" {
		return err
	}

	if failure == interfaces.FailureTypeQuota {
		// The executor has already checkpointed the interrupted task, so the
		// cooldown records no new state of its own.
		if _, qErr := a.EnterQuotaCooldown(ctx, nil, time.Time{}, "claude usage limit reached"); qErr != nil {
			a.Logger.Error("failed to record the quota cooldown", "error", qErr)
		}
		return fmt.Errorf("%w: %v", ErrQuotaExhausted, err)
	}

	return err
}
