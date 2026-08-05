package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/executor"
)

// WatchOptions controls continuous operation.
type WatchOptions struct {
	// Interval is how long to wait before re-reading the backlog once it is
	// empty. Zero uses DefaultWatchInterval.
	Interval time.Duration
	// MaxConsecutiveFailures stops the watch after this many runs fail
	// without a single task completing in between. Zero uses
	// DefaultMaxConsecutiveFailures.
	MaxConsecutiveFailures int
	// OnOutcome is called for each completed task, so a caller can report
	// progress as it happens rather than at the end of a run that never ends.
	OnOutcome func(*executor.Outcome)
	// OnIdle is called when the backlog is empty and the watch is about to
	// sleep.
	OnIdle func(next time.Duration)
	// OnFailure is called when a run stops with an error the watch will
	// absorb and continue past.
	OnFailure func(err error, consecutive int)
}

const (
	// DefaultWatchInterval balances picking up newly added work promptly
	// against re-reading an empty backlog thousands of times an hour.
	DefaultWatchInterval = 60 * time.Second
	// DefaultMaxConsecutiveFailures bounds a systemic problem. A failing task
	// is marked blocked and never selected again, so repeated failures
	// without progress mean something the loop cannot fix — a dirty tree, a
	// missing toolchain, no credentials — and continuing would burn quota
	// producing the same error indefinitely.
	DefaultMaxConsecutiveFailures = 3
)

// ErrTooManyFailures reports that the watch stopped because nothing was
// completing. The underlying cause is wrapped.
var ErrTooManyFailures = errors.New("stopping: consecutive runs failed without completing a task")

// Watch runs the backlog continuously until the context is cancelled.
//
// This is what the system is for. RunLoop drains the backlog and returns,
// which is right for a single invocation but leaves the process needing an
// external scheduler to be what the name promises. Watch closes that gap: an
// empty backlog is not the end of the work, it is a pause until someone adds
// more.
//
// A failed task does not stop the watch. The executor marks it blocked, and a
// blocked task is never selected again, so the loop moves on to work that can
// still succeed rather than halting the whole system over one bad task. What
// does stop the watch is repeated failure with no progress in between, which
// means a problem no amount of retrying will clear.
func (a *App) Watch(ctx context.Context, opts WatchOptions) error {
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultWatchInterval
	}

	maxFailures := opts.MaxConsecutiveFailures
	if maxFailures <= 0 {
		maxFailures = DefaultMaxConsecutiveFailures
	}

	consecutiveFailures := 0

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		outcomes, err := a.RunLoop(ctx, 0)

		for _, o := range outcomes {
			if opts.OnOutcome != nil {
				opts.OnOutcome(o)
			}
		}

		// Any completed task means the system is working, whatever happened
		// afterwards. Resetting here is what stops a long-running watch from
		// accumulating unrelated failures over hours and stopping on a total
		// that never represented a real problem.
		if len(outcomes) > 0 {
			consecutiveFailures = 0
		}

		switch {
		case err == nil:
			// The backlog is empty. Wait for someone to add work.
			if opts.OnIdle != nil {
				opts.OnIdle(interval)
			}

		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return err

		case errors.Is(err, ErrQuotaExhausted):
			// The cooldown is already recorded and AwaitQuota will sleep
			// through it on the next pass. Not a failure of the work.
			a.Logger.Info("usage limit reached; waiting for the window to reset")

		default:
			consecutiveFailures++
			if opts.OnFailure != nil {
				opts.OnFailure(err, consecutiveFailures)
			}
			a.Logger.Error("task run failed",
				"error", err, "consecutive", consecutiveFailures)

			if consecutiveFailures >= maxFailures {
				return fmt.Errorf("%w (%d in a row): %v",
					ErrTooManyFailures, consecutiveFailures, err)
			}
		}

		if err := sleep(ctx, interval); err != nil {
			return err
		}
	}
}

// sleep waits, returning early if the context is cancelled. A bare
// time.Sleep would ignore Ctrl-C for the whole interval.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
