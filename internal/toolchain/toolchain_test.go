package toolchain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeClaude struct {
	prompts  []string
	response *Response
	err      error
}

func (f *fakeClaude) LaunchSession(ctx context.Context, prompt string) (*Response, error) {
	f.prompts = append(f.prompts, prompt)
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s failed: %v", name, err)
		}
	}
	return dir
}

// A language this binary knows nothing about is a first-class project: the
// verification knowledge comes from the repository, not from a release.
func TestDetectWorksForAnUnknownLanguage(t *testing.T) {
	c := &fakeClaude{response: &Response{Output: `{
		"language": "elixir",
		"gates": [
			{"name": "compile", "command": ["mix", "compile", "--warnings-as-errors"],
			 "required": true, "expectation": "compile with no warnings"},
			{"name": "test", "command": ["mix", "test"], "required": true}
		]
	}`}}

	dir := fixture(t, map[string]string{"mix.exs": "defmodule Demo.MixProject do\nend\n"})

	got, err := New(c, dir).Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if got.Language != "elixir" {
		t.Errorf("language: got %q, want elixir", got.Language)
	}
	if len(got.Gates) != 2 {
		t.Fatalf("gates: got %d, want 2", len(got.Gates))
	}
	if got.Gates[0].Command[0] != "mix" {
		t.Errorf("command: got %v", got.Gates[0].Command)
	}
	if got.DetectedAt.IsZero() {
		t.Error("detection should be timestamped")
	}
}

func TestDetectSendsTheManifestsAsEvidence(t *testing.T) {
	c := &fakeClaude{response: &Response{
		Output: `{"gates":[{"name":"test","command":["cargo","test"],"required":true}]}`,
	}}

	dir := fixture(t, map[string]string{
		"Cargo.toml": "[package]\nname = \"demo\"\n",
		"Makefile":   "test:\n\tcargo test --all-features\n",
		"src/lib.rs": "pub fn add() {}\n",
	})

	if _, err := New(c, dir).Detect(context.Background()); err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	prompt := c.prompts[0]
	for _, want := range []string{"Cargo.toml", "Makefile", "cargo test --all-features", "src/"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt should carry %q as evidence", want)
		}
	}
}

// Directories that are large and say nothing about the toolchain would crowd
// out the evidence that does.
func TestEvidenceSkipsNoiseDirectories(t *testing.T) {
	c := &fakeClaude{response: &Response{
		Output: `{"gates":[{"name":"test","command":["npm","test"],"required":true}]}`,
	}}

	dir := fixture(t, map[string]string{
		"package.json":            `{"name":"x"}`,
		"node_modules/left-pad/i": "x",
		"vendor/thing/x":          "x",
	})

	if _, err := New(c, dir).Detect(context.Background()); err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	prompt := c.prompts[0]
	if strings.Contains(prompt, "node_modules") || strings.Contains(prompt, "vendor") {
		t.Errorf("dependency directories should not be listed as evidence:\n%s", prompt)
	}
}

// A failed detection must not become an empty gate set — that would let every
// change through unverified while reporting success.
func TestUnparseableDetectionIsAnError(t *testing.T) {
	for _, output := range []string{
		"",
		"I had a look and it seems to be a Rust project.",
		"{ not json",
		`{"language": "rust"}`,
		`{"language": "rust", "gates": []}`,
		`{"gates":[{"name":"test"}]}`,
	} {
		c := &fakeClaude{response: &Response{Output: output}}
		dir := fixture(t, map[string]string{"Cargo.toml": ""})

		got, err := New(c, dir).Detect(context.Background())
		if !errors.Is(err, ErrUnparseable) {
			t.Errorf("output %q: got (%v, %v), want ErrUnparseable", output, got, err)
		}
	}
}

// A gate with a name but no command cannot be run. Dropping it beats keeping
// it and failing every task on a malformed declaration.
func TestGatesWithoutCommandsAreDropped(t *testing.T) {
	got, err := ParseToolchain(`{"gates":[
		{"name":"test","command":["make","test"],"required":true},
		{"name":"broken"},
		{"command":["make","lint"]}
	]}`)
	if err != nil {
		t.Fatalf("ParseToolchain failed: %v", err)
	}
	if len(got.Gates) != 1 || got.Gates[0].Name != "test" {
		t.Errorf("only the runnable gate should survive: %+v", got.Gates)
	}
}

func TestParseExtractsJSONFromFencedProse(t *testing.T) {
	output := "Looks like a Go project.\n\n```json\n" +
		`{"language":"go","gates":[{"name":"test","command":["go","test","./..."],"required":true}]}` +
		"\n```\n\nHope that helps."

	got, err := ParseToolchain(output)
	if err != nil {
		t.Fatalf("ParseToolchain failed: %v", err)
	}
	if len(got.Gates) != 1 {
		t.Fatalf("gates: got %d, want 1", len(got.Gates))
	}
}

func TestParseHandlesBracesInsideCommands(t *testing.T) {
	got, err := ParseToolchain(`{"gates":[{"name":"test",
		"command":["sh","-c","echo {}"],"required":true}]}`)
	if err != nil {
		t.Fatalf("ParseToolchain failed: %v", err)
	}
	if got.Gates[0].Command[2] != "echo {}" {
		t.Errorf("the brace in the argument was mishandled: %v", got.Gates[0].Command)
	}
}

func TestSessionFailureIsReported(t *testing.T) {
	c := &fakeClaude{err: errors.New("claude crashed")}
	dir := fixture(t, map[string]string{"go.mod": "module x\n"})

	if _, err := New(c, dir).Detect(context.Background()); err == nil {
		t.Fatal("a failed session must be reported, not treated as no toolchain")
	}
}

func TestReportedErrorsFailDetection(t *testing.T) {
	c := &fakeClaude{response: &Response{Errors: []string{"context window exceeded"}}}
	dir := fixture(t, map[string]string{"go.mod": "module x\n"})

	_, err := New(c, dir).Detect(context.Background())
	if err == nil {
		t.Fatal("errors reported by Claude must fail detection")
	}
	if !strings.Contains(err.Error(), "context window exceeded") {
		t.Errorf("the reported error should surface: %v", err)
	}
}

// A command that does not exist produces a gate failing every task for a
// reason unrelated to the change — worse than no gate, because it looks real.
func TestPromptForbidsInventedCommands(t *testing.T) {
	prompt := BuildDetectPrompt("## Repository root\n\n- Cargo.toml\n")

	for _, want := range []string{
		"Do not invent commands",
		"Base every command on evidence",
		"non-interactive",
		"argv as a list",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt is missing %q", want)
		}
	}
	// The secret scan always runs separately; asking for it invites a
	// duplicate gate.
	if !strings.Contains(prompt, "Do not include a secret") {
		t.Error("the prompt should not ask for a secret scan")
	}
}

func TestOversizedManifestsAreTruncated(t *testing.T) {
	c := &fakeClaude{response: &Response{
		Output: `{"gates":[{"name":"test","command":["make"],"required":true}]}`,
	}}

	huge := strings.Repeat("x = 1\n", maxManifestBytes)
	dir := fixture(t, map[string]string{"Cargo.toml": huge})

	if _, err := New(c, dir).Detect(context.Background()); err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if !strings.Contains(c.prompts[0], "truncated") {
		t.Error("an oversized manifest should be truncated and said to be")
	}
}

func TestRenderShowsTheGates(t *testing.T) {
	got, err := ParseToolchain(`{"language":"rust","gates":[
		{"name":"build","command":["cargo","build"],"required":true},
		{"name":"lint","command":["cargo","clippy"],"required":false}
	]}`)
	if err != nil {
		t.Fatalf("ParseToolchain failed: %v", err)
	}

	out := Render(got)
	for _, want := range []string{"rust", "build", "cargo build", "required", "lint", "optional", "secret"} {
		if !strings.Contains(out, want) {
			t.Errorf("the rendered toolchain is missing %q:\n%s", want, out)
		}
	}
}
