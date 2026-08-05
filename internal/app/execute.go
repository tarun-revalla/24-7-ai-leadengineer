package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/executor"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/gates"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/review"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/toolchain"
	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// ErrNoWork reports an empty backlog, re-exported so callers need not import
// the executor package to recognise it.
var ErrNoWork = executor.ErrNoWork

// Executor builds a task executor from the wired subsystems.
//
// See gateSet for how the checks are chosen; the short version is that a
// toolchain detected from the repository beats anything compiled in, and a
// project with no gates at all is refused rather than run.
func (a *App) Executor() (*executor.Executor, error) {
	projectType := a.Config.GetString("project.type")

	gateList, err := a.gateSet(projectType)
	if err != nil {
		return nil, err
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
	//
	// In inspection mode the reviewer is forced on regardless of
	// review.enabled: no tooling runs, so switching review off too would leave
	// nothing checking the change at all, and every task would commit
	// unconditionally. That is not a configuration anyone wants by accident.
	var reviewer executor.Reviewer
	if a.Config.GetBool("review.enabled") || a.InspectionMode() {
		reviewer = review.New(claudeReviewSession{a.Claude})
	}

	return executor.New(cfg, executor.Deps{
		Claude:      a.Claude,
		Git:         a.Git,
		Memory:      a.Memory,
		Checkpoints: a.Checkpoints,
		Gates:       gates.NewRunner(gateList...),
		Reviewer:    reviewer,
		Metrics:     a.Metrics,
		Log:         a.Logger,
	}), nil
}

// gateSet chooses the checks a change must pass.
//
// A toolchain detected from the repository wins over anything compiled in.
// That ordering is the point: the built-in Go and Node sets are a convenience
// for the two most common cases, not the limit of what the system supports.
// Any project that can declare its own commands — in any language, or none —
// is a first-class project here.
//
// A project with neither a detected toolchain nor a built-in set is refused
// rather than run, because committing work that no gate ever checked is the
// one thing this system exists to prevent.
func (a *App) gateSet(projectType string) ([]gates.Gate, error) {
	ctx := context.Background()

	// Inspection mode runs no project tooling at all. The secret scan stays,
	// because it needs no toolchain and a published credential is the one
	// failure that cannot be undone by fixing the code afterwards. Everything
	// else falls to the review stage, which reads the diff.
	if a.InspectionMode() {
		return []gates.Gate{gates.SecretScan{}}, nil
	}

	if t, err := a.Memory.GetToolchain(ctx); err == nil && len(t.Gates) > 0 {
		specs := make([]gates.Spec, 0, len(t.Gates))
		for _, g := range t.Gates {
			specs = append(specs, gates.Spec{
				Name:          g.Name,
				Command:       g.Command,
				Required:      g.Required,
				Expectation:   g.Expectation,
				SkipIfMissing: g.SkipIfMissing,
			})
		}

		gateList, err := gates.FromSpecs(specs)
		if err != nil {
			// A malformed declaration must be reported, not quietly replaced
			// by a built-in set that checks something else.
			return nil, fmt.Errorf("the declared toolchain in .ai/TOOLCHAIN.md is not usable: %w", err)
		}
		return gateList, nil
	}

	minCoverage := 0.0
	if a.Config.GetBool("quality.requireTests") {
		minCoverage = a.Config.GetFloat64("quality.minimumCoverage")
	}

	switch projectType {
	case "go":
		return gates.GoGates(minCoverage), nil
	case "node", "javascript", "typescript":
		return gates.NodeGates(minCoverage), nil
	default:
		return nil, fmt.Errorf("no quality gates are defined for project type %q; "+
			"run `leadengineer detect` to work them out from the repository, "+
			"or declare them in .ai/TOOLCHAIN.md", projectType)
	}
}

// DetectToolchain works out how this project verifies itself and records it.
func (a *App) DetectToolchain(ctx context.Context) (*interfaces.Toolchain, error) {
	if _, err := a.AwaitQuota(ctx); err != nil {
		return nil, err
	}

	detected, err := toolchain.New(claudeDetectSession{a.Claude}, a.ProjectPath).Detect(ctx)
	if err != nil {
		return nil, err
	}

	// Validated before it is saved, so a declaration that cannot run is
	// rejected now rather than failing the next task for an unrelated-looking
	// reason.
	specs := make([]gates.Spec, 0, len(detected.Gates))
	for _, g := range detected.Gates {
		specs = append(specs, gates.Spec{Name: g.Name, Command: g.Command, Required: g.Required})
	}
	if _, err := gates.FromSpecs(specs); err != nil {
		return nil, fmt.Errorf("the detected toolchain is not usable: %w", err)
	}

	if err := a.Memory.SaveToolchain(ctx, detected); err != nil {
		return nil, fmt.Errorf("failed to record the toolchain: %w", err)
	}

	return detected, nil
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

// claudeDetectSession adapts the session manager for toolchain detection.
// A second small adapter rather than a shared one: each consumer package
// declares the narrow interface it needs, which is what lets them be tested
// without standing up a session manager.
type claudeDetectSession struct {
	claude interfaces.ClaudeSessionManager
}

func (c claudeDetectSession) LaunchSession(ctx context.Context, prompt string) (*toolchain.Response, error) {
	result, err := c.claude.LaunchSession(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return &toolchain.Response{Output: result.Output, Errors: result.Errors}, nil
}

// InspectionMode reports whether verification is by reading rather than by
// running.
//
// This is a real reduction in assurance, not a different route to the same
// one. A model reading a diff can catch a wrong algorithm, a missing error
// path, an injection, an off-by-one. It cannot know the code compiles, that
// the tests pass, or that nothing else in the repository broke — only running
// the tooling establishes those.
//
// It exists because a project whose toolchain cannot run here is otherwise
// blocked entirely, and reviewed-but-unexecuted work committed steadily beats
// no work at all when a human is checking in periodically. What matters is
// that the resulting commits say plainly which of the two happened, so that
// human knows what they are looking at.
func (a *App) InspectionMode() bool {
	return a.Config.GetBool("quality.inspectionOnly")
}
