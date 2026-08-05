package gates

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecValidateRejectsUnrunnableDeclarations(t *testing.T) {
	tests := []struct {
		name string
		spec Spec
		ok   bool
	}{
		{"complete", Spec{Name: "test", Command: []string{"cargo", "test"}}, true},
		{"no name", Spec{Command: []string{"cargo", "test"}}, false},
		{"blank name", Spec{Name: "   ", Command: []string{"cargo"}}, false},
		{"no command", Spec{Name: "test"}, false},
		{"empty command", Spec{Name: "test", Command: []string{""}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			if tt.ok && err != nil {
				t.Errorf("expected valid, got %v", err)
			}
			if !tt.ok && err == nil {
				t.Error("a gate that cannot be run must be rejected")
			}
		})
	}
}

// Any project that can be checked by running a command is a first-class
// project here — nothing about the language is compiled in.
func TestCommandGateRunsAnArbitraryCommand(t *testing.T) {
	g := CommandGate{Spec: Spec{Name: "build", Command: []string{"true"}, Required: true}}

	res := g.Run(context.Background(), t.TempDir())
	if res.Status != StatusPassed {
		t.Errorf("got %s (%s), want passed", res.Status, res.Detail)
	}
}

func TestCommandGateFailsOnNonZeroExit(t *testing.T) {
	g := CommandGate{Spec: Spec{Name: "test", Command: []string{"false"}, Required: true}}

	res := g.Run(context.Background(), t.TempDir())
	if res.Status != StatusFailed {
		t.Errorf("got %s, want failed", res.Status)
	}
	if !strings.Contains(res.Detail, "false") {
		t.Errorf("the failure should name the command that failed: %q", res.Detail)
	}
}

// A required check whose tool is absent proves nothing, so it blocks; an
// optional one records a visible skip. The project makes that call.
func TestCommandGateHonoursRequiredWhenToolIsMissing(t *testing.T) {
	missingTool := "definitely-not-a-real-binary-xyz"

	required := CommandGate{Spec: Spec{Name: "build", Command: []string{missingTool}, Required: true}}
	if res := required.Run(context.Background(), t.TempDir()); res.Status != StatusFailed {
		t.Errorf("a required gate with no tool must block, got %s", res.Status)
	}

	optional := CommandGate{Spec: Spec{Name: "lint", Command: []string{missingTool}}}
	res := optional.Run(context.Background(), t.TempDir())
	if res.Status != StatusSkipped {
		t.Errorf("an optional gate with no tool should skip, got %s", res.Status)
	}
	if !strings.Contains(res.Detail, "not installed") {
		t.Errorf("a skip must say why: %q", res.Detail)
	}
}

// SkipIfMissing lets a project name the real tool when the command is a
// wrapper: `npx tsc` runs even without TypeScript, so npx's presence proves
// nothing.
func TestSpecSkipIfMissingOverridesTheCommandName(t *testing.T) {
	s := Spec{Name: "typecheck", Command: []string{"npx", "tsc"}, SkipIfMissing: "tsc"}
	if got := s.tool(); got != "tsc" {
		t.Errorf("got %q, want tsc", got)
	}

	s = Spec{Name: "typecheck", Command: []string{"npx", "tsc"}}
	if got := s.tool(); got != "npx" {
		t.Errorf("got %q, want the command's first word", got)
	}
}

// The secret scan cannot be declared away. Every other check is the project's
// call, but a published credential is the one failure that fixing the code
// afterwards does not undo — the key is already out.
func TestFromSpecsAlwaysIncludesTheSecretScan(t *testing.T) {
	gateList, err := FromSpecs([]Spec{{Name: "build", Command: []string{"make"}}})
	if err != nil {
		t.Fatalf("FromSpecs failed: %v", err)
	}

	if gateList[0].Name() != "secrets" {
		t.Errorf("the secret scan should run first, got %q", gateList[0].Name())
	}

	// Even with nothing declared at all.
	bare, err := FromSpecs(nil)
	if err != nil {
		t.Fatalf("FromSpecs failed: %v", err)
	}
	if len(bare) != 1 || bare[0].Name() != "secrets" {
		t.Errorf("an empty declaration should still scan for secrets, got %v", bare)
	}
}

func TestFromSpecsRejectsDuplicateNames(t *testing.T) {
	_, err := FromSpecs([]Spec{
		{Name: "test", Command: []string{"make", "test"}},
		{Name: "test", Command: []string{"make", "integration"}},
	})
	if err == nil {
		t.Fatal("two gates with one name make a report a reader cannot interpret")
	}
}

func TestFromSpecsRejectsAGateNamedSecrets(t *testing.T) {
	_, err := FromSpecs([]Spec{{Name: "secrets", Command: []string{"true"}}})
	if err == nil {
		t.Error("the built-in secret scan must not be shadowed by a declaration")
	}
}

func TestFromSpecsPropagatesValidationErrors(t *testing.T) {
	if _, err := FromSpecs([]Spec{{Name: "build"}}); err == nil {
		t.Error("a malformed spec must be reported when the set is built, not when it runs")
	}
}

// A declared gate describes its own bar in the implement prompt exactly as a
// built-in one does.
func TestDeclaredGatesContributeExpectations(t *testing.T) {
	gateList, err := FromSpecs([]Spec{{
		Name:        "test",
		Command:     []string{"cargo", "test"},
		Required:    true,
		Expectation: "pass cargo test",
	}})
	if err != nil {
		t.Fatalf("FromSpecs failed: %v", err)
	}

	got := strings.Join(Expectations(gateList), "\n")
	if !strings.Contains(got, "pass cargo test") {
		t.Errorf("a declared expectation should reach the prompt:\n%s", got)
	}
}

// Arguments are not passed through a shell, so one containing spaces or
// metacharacters cannot change what executes.
func TestCommandGateDoesNotUseAShell(t *testing.T) {
	dir := t.TempDir()

	// If this went through a shell the `;` would start a second command.
	g := CommandGate{Spec: Spec{
		Name:     "echo",
		Command:  []string{"echo", "hello; touch pwned"},
		Required: true,
	}}

	if res := g.Run(context.Background(), dir); res.Status != StatusPassed {
		t.Fatalf("got %s (%s)", res.Status, res.Detail)
	}

	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Error("the argument was interpreted by a shell")
	}
}
