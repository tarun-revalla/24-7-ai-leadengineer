package gates

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Spec declares one quality check as data rather than as compiled-in code.
//
// This is what keeps the system from having a tech-stack bottleneck. A gate
// set written in Go means every new language needs a code change, a release
// and a rebuild before the system can verify anything written in it — the
// language support becomes a property of this binary rather than of the
// project being worked on. A Spec moves that knowledge into the project,
// where it already lives: every repository already knows how to build and
// test itself, in its Makefile, its CI config, or its contributors' heads.
//
// Anything runnable as a command can be a gate. Rust, Elixir, Zig, a
// polyglot monorepo, a Makefile target, a shell script — none of them need
// this binary to know they exist.
type Spec struct {
	// Name identifies the gate in reports. Required.
	Name string `yaml:"name" json:"name"`

	// Command is the argv to run, relative to the project root. Required.
	// It is a list, not a string, so nothing is passed through a shell:
	// arguments carrying spaces or quotes cannot change what is executed.
	Command []string `yaml:"command" json:"command"`

	// Required reports whether missing tooling should fail rather than skip.
	// A check whose tool is absent proves nothing; whether that blocks is a
	// judgement about how central the check is, so the project makes it.
	Required bool `yaml:"required" json:"required"`

	// Expectation states, in a sentence, what this gate asks of a change.
	// Carried into the prompt so the implementer is told the actual bar
	// rather than a generic one. Optional.
	Expectation string `yaml:"expectation,omitempty" json:"expectation,omitempty"`

	// SkipIfMissing names a tool whose absence turns this gate into a skip
	// rather than a run. Defaults to Command[0]. Optional.
	SkipIfMissing string `yaml:"skipIfMissing,omitempty" json:"skipIfMissing,omitempty"`
}

// Validate reports whether a spec can be run at all.
//
// Checked when the set is loaded rather than when a gate runs, so a
// malformed declaration is a startup error the operator sees immediately
// instead of a task failing halfway through for an unrelated-looking reason.
func (s Spec) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("a gate needs a name")
	}
	if len(s.Command) == 0 {
		return fmt.Errorf("gate %q needs a command", s.Name)
	}
	if strings.TrimSpace(s.Command[0]) == "" {
		return fmt.Errorf("gate %q has an empty command", s.Name)
	}
	return nil
}

// tool returns the binary whose absence makes this gate a skip.
func (s Spec) tool() string {
	if s.SkipIfMissing != "" {
		return s.SkipIfMissing
	}
	return s.Command[0]
}

// CommandGate runs a declared Spec.
type CommandGate struct {
	Spec Spec
}

func (g CommandGate) Name() string   { return g.Spec.Name }
func (g CommandGate) Required() bool { return g.Spec.Required }

// Expectation implements Expectant, so a declared gate can describe its own
// bar in the implement prompt exactly as a built-in one does.
func (g CommandGate) Expectation() string { return g.Spec.Expectation }

func (g CommandGate) Run(ctx context.Context, dir string) Result {
	started := time.Now()

	if !toolAvailable(g.Spec.tool()) {
		return missing(g, g.Spec.tool(), started)
	}

	out, err := command{g.Spec.Command[0], g.Spec.Command[1:]}.run(ctx, dir)
	if err != nil {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: fmt.Sprintf("`%s` failed", strings.Join(g.Spec.Command, " ")),
			Output: out,
		}
	}

	return Result{Gate: g.Name(), Status: StatusPassed, Duration: time.Since(started)}
}

// FromSpecs builds a runnable gate set from declarations.
//
// SecretScan is prepended unconditionally and cannot be declared away. Every
// other check is the project's call, but a credential reaching a public
// repository is the one failure that is not recoverable by fixing the code
// afterwards — the key is already published. It needs no toolchain, so there
// is no project for which it cannot run.
func FromSpecs(specs []Spec) ([]Gate, error) {
	out := []Gate{SecretScan{}}

	seen := map[string]bool{"secrets": true}
	for _, s := range specs {
		if err := s.Validate(); err != nil {
			return nil, err
		}
		if seen[s.Name] {
			// Two gates with one name make a report where a reader cannot
			// tell which check failed.
			return nil, fmt.Errorf("duplicate gate name %q", s.Name)
		}
		seen[s.Name] = true

		out = append(out, CommandGate{Spec: s})
	}

	return out, nil
}
