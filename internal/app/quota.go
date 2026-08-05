package app

import (
	"context"
	"fmt"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/quota"
	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// AwaitQuota blocks until Claude may be called again, returning how long it
// waited. It is the first thing any unit of work should do: a cooldown
// recorded by a previous run, or by a process that has since restarted, is
// still in force.
func (a *App) AwaitQuota(ctx context.Context) (time.Duration, error) {
	available, remaining := a.Quota.Available(ctx)
	if available {
		return 0, nil
	}

	a.Logger.Info("waiting for quota to reset",
		"remaining", remaining.String(),
		"resumeAt", a.Quota.Current(ctx).ResumeAt,
	)

	waited, err := a.Quota.WaitUntilAvailable(ctx)
	if err != nil {
		return 0, err
	}

	a.Logger.Info("quota cooldown elapsed", "waited", waited.String())
	return waited, nil
}

// EnterQuotaCooldown checkpoints the supplied state and records the cooldown.
//
// The checkpoint is taken first and deliberately: if recording the cooldown
// fails, the work is still saved. The reverse order could leave the system
// waiting on a limit with nothing to resume from.
func (a *App) EnterQuotaCooldown(
	ctx context.Context,
	state *interfaces.CheckpointState,
	resetHint time.Time,
	reason string,
) (*quota.State, error) {
	var checkpointID string

	if state != nil {
		id, err := a.Checkpoints.CreateCheckpoint(ctx, state)
		if err != nil {
			return nil, fmt.Errorf("failed to checkpoint before cooldown: %w", err)
		}
		checkpointID = id

		a.Logger.Info("checkpointed before quota cooldown",
			"checkpoint", id, "task", state.TaskID)
	}

	recorded, err := a.Quota.RecordExhaustion(ctx, resetHint, reason)
	if err != nil {
		return nil, err
	}

	if checkpointID != "" {
		if err := a.Quota.NoteCheckpoint(ctx, checkpointID); err != nil {
			return nil, err
		}
		recorded.LastCheckpoint = checkpointID
	}

	a.Logger.Warn("quota exhausted; entering cooldown",
		"reason", reason,
		"resumeAt", recorded.ResumeAt,
		"consecutiveHits", recorded.ConsecutiveHits,
		"checkpoint", checkpointID,
	)

	return recorded, nil
}

// ClearQuota marks quota available after a successful call.
//
// Only a success is evidence that a limit has lifted, so this is never called
// speculatively — doing so would reset the backoff that protects against a
// misjudged reset window.
func (a *App) ClearQuota(ctx context.Context) error {
	if available, _ := a.Quota.Available(ctx); available {
		if !a.Quota.Current(ctx).Exhausted {
			return nil
		}
	}

	a.Logger.Info("quota available again")
	return a.Quota.Clear(ctx)
}

// RunClaude executes a prompt with quota handling around it.
//
// It waits out any active cooldown, runs the prompt, and on a usage limit
// checkpoints and schedules the resume. snapshot supplies the state to save;
// it is called only when a checkpoint is actually needed.
func (a *App) RunClaude(
	ctx context.Context,
	prompt string,
	snapshot func() *interfaces.CheckpointState,
) (*interfaces.SessionResult, error) {
	if _, err := a.AwaitQuota(ctx); err != nil {
		return nil, err
	}

	result, err := a.Claude.LaunchSession(ctx, prompt)
	if err != nil {
		return nil, err
	}

	failure, detectErr := a.Claude.DetectFailure(ctx)
	if detectErr != nil {
		// Failing to classify is not itself a failure of the call; the result
		// stands, but the ambiguity is worth recording.
		a.Logger.Warn("could not classify session outcome", "error", detectErr)
		return result, nil
	}

	if failure == interfaces.FailureTypeQuota {
		var state *interfaces.CheckpointState
		if snapshot != nil {
			state = snapshot()
		}

		// No reset time is parsed from the CLI yet, so the cooldown is
		// estimated with backoff. See PHASE3-005.
		if _, err := a.EnterQuotaCooldown(ctx, state, time.Time{}, "claude usage limit reached"); err != nil {
			return result, err
		}

		return result, ErrQuotaExhausted
	}

	if failure == "" {
		if err := a.ClearQuota(ctx); err != nil {
			a.Logger.Warn("failed to clear quota state", "error", err)
		}
	}

	return result, nil
}
