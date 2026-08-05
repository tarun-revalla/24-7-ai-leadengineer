// Package gates runs the quality checks a change must pass before it is
// committed.
//
// A gate that could not be run is never reported as passing. Required gates
// fail when their tooling is absent, because a check that did not execute
// proves nothing; optional gates record that they were skipped and why, so the
// gap is visible rather than silent.
package gates

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Status is the outcome of running one gate.
type Status string

const (
	StatusPassed  Status = "passed"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

// Result records what one gate did.
type Result struct {
	Gate     string
	Status   Status
	Duration time.Duration
	// Detail explains a failure or a skip in one line.
	Detail string
	// Output is the tool's own output, retained so a failure can be handed
	// back to Claude with enough context to act on.
	Output string
}

// Passed reports whether the gate allows a commit to proceed. A skipped
// optional gate does not block.
func (r Result) Passed() bool {
	return r.Status != StatusFailed
}

// Gate is a single quality check.
type Gate interface {
	// Name identifies the gate in reports.
	Name() string
	// Required reports whether missing tooling should fail rather than skip.
	Required() bool
	// Run executes the check against a project directory.
	Run(ctx context.Context, projectPath string) Result
}

// Report is the combined outcome of a gate run.
type Report struct {
	Results  []Result
	Duration time.Duration
}

// Passed reports whether every gate allows the commit.
func (r *Report) Passed() bool {
	for _, res := range r.Results {
		if !res.Passed() {
			return false
		}
	}
	return true
}

// Failures returns the gates that blocked.
func (r *Report) Failures() []Result {
	var out []Result
	for _, res := range r.Results {
		if res.Status == StatusFailed {
			out = append(out, res)
		}
	}
	return out
}

// Summary renders a one-line-per-gate report.
func (r *Report) Summary() string {
	var b strings.Builder
	for _, res := range r.Results {
		mark := "ok  "
		switch res.Status {
		case StatusFailed:
			mark = "FAIL"
		case StatusSkipped:
			mark = "skip"
		}

		fmt.Fprintf(&b, "%s  %-12s %s", mark, res.Gate, res.Duration.Truncate(time.Millisecond))
		if res.Detail != "" {
			fmt.Fprintf(&b, "  %s", res.Detail)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// FailureContext renders failing gates and their output, for handing back to
// Claude as the description of what needs fixing.
func (r *Report) FailureContext() string {
	var b strings.Builder
	for _, res := range r.Failures() {
		fmt.Fprintf(&b, "## %s failed\n\n", res.Gate)
		if res.Detail != "" {
			fmt.Fprintf(&b, "%s\n\n", res.Detail)
		}
		if out := strings.TrimSpace(res.Output); out != "" {
			fmt.Fprintf(&b, "```\n%s\n```\n\n", truncate(out, 8000))
		}
	}
	return b.String()
}

// Expectant is implemented by a gate that can state, in a sentence, what it
// will require of a change.
//
// Optional because the alternative is a hardcoded "definition of done" in the
// prompt, which is wrong the moment a project is not the language it was
// written for — telling someone working on a TypeScript webapp that their
// change must be gofmt-clean is worse than saying nothing.
type Expectant interface {
	Expectation() string
}

// Expectations collects what a gate set will require, for a prompt that has to
// state the bar a change is actually held to. Gates that cannot describe
// themselves are omitted rather than guessed at.
func Expectations(gs []Gate) []string {
	var out []string
	for _, g := range gs {
		if e, ok := g.(Expectant); ok {
			if s := strings.TrimSpace(e.Expectation()); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// Runner executes a set of gates in order.
type Runner struct {
	gates []Gate
}

// NewRunner creates a runner over the supplied gates.
func NewRunner(gates ...Gate) *Runner {
	return &Runner{gates: gates}
}

// Gates returns the configured gates.
func (r *Runner) Gates() []Gate { return r.gates }

// Run executes every gate and collects the results.
//
// Execution continues past a failure so one run reports every problem, rather
// than surfacing them one commit at a time. Cancellation stops the run.
func (r *Runner) Run(ctx context.Context, projectPath string) *Report {
	started := time.Now()
	report := &Report{}

	for _, g := range r.gates {
		if err := ctx.Err(); err != nil {
			report.Results = append(report.Results, Result{
				Gate:   g.Name(),
				Status: StatusFailed,
				Detail: fmt.Sprintf("not run: %v", err),
			})
			break
		}

		report.Results = append(report.Results, g.Run(ctx, projectPath))
	}

	report.Duration = time.Since(started)
	return report
}

// command runs a tool and captures combined output.
type command struct {
	name string
	args []string
}

func (c command) run(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, c.name, c.args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	return string(out), err
}

// toolAvailable reports whether a binary is on PATH.
func toolAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// missing builds the result for a gate whose tooling is absent.
func missing(g Gate, tool string, started time.Time) Result {
	res := Result{
		Gate:     g.Name(),
		Duration: time.Since(started),
		Detail:   fmt.Sprintf("%s is not installed", tool),
	}

	if g.Required() {
		// A required check that did not run proves nothing, so it blocks.
		res.Status = StatusFailed
		return res
	}

	res.Status = StatusSkipped
	return res
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... output truncated ..."
}
