package gates

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nodeFixture writes a project directory without the Go module fixture adds.
func nodeFixture(t *testing.T, files map[string]string) string {
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

func TestNodeGatesCoverTheExpectedChecks(t *testing.T) {
	names := map[string]bool{}
	for _, g := range NodeGates(0.8) {
		names[g.Name()] = true
	}

	for _, want := range []string{"secrets", "install", "format", "typecheck", "build", "test", "lint"} {
		if !names[want] {
			t.Errorf("the Node gate set is missing %q", want)
		}
	}
}

// The lockfile decides, not what happens to be installed: running npm install
// in a pnpm project rewrites the lockfile and can resolve different versions
// than the ones the project was tested against.
func TestPackageManagerFollowsTheLockfile(t *testing.T) {
	tests := []struct {
		lockfile string
		want     string
	}{
		{"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"},
		{"bun.lockb", "bun"},
		{"package-lock.json", "npm"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			dir := nodeFixture(t, map[string]string{
				"package.json": `{"name":"x"}`,
				tt.lockfile:    "",
			})
			if got := packageManager(dir); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPackageManagerDefaultsToNpm(t *testing.T) {
	dir := nodeFixture(t, map[string]string{"package.json": `{"name":"x"}`})
	if got := packageManager(dir); got != "npm" {
		t.Errorf("got %q, want npm with no lockfile present", got)
	}
}

func TestFirstScriptPrefersTheEarlierName(t *testing.T) {
	pkg := &packageJSON{Scripts: map[string]string{"type-check": "tsc", "tsc": "tsc"}}

	got, ok := pkg.firstScript([]string{"typecheck", "type-check", "tsc"})
	if !ok {
		t.Fatal("a defined script should have been found")
	}
	// Projects spell the same intent differently; the first match in the
	// caller's preference order wins.
	if got != "type-check" {
		t.Errorf("got %q, want type-check", got)
	}
}

func TestFirstScriptReportsAbsence(t *testing.T) {
	pkg := &packageJSON{Scripts: map[string]string{"start": "node ."}}
	if _, ok := pkg.firstScript([]string{"build"}); ok {
		t.Error("an undefined script must not be reported as present")
	}
}

// A library with no build step is a normal project, not a broken one, so a
// missing script is a labelled skip rather than a failure.
func TestNodeScriptSkipsWhenTheScriptIsUndefined(t *testing.T) {
	if !toolAvailable("npm") {
		t.Skip("npm is not installed")
	}

	dir := nodeFixture(t, map[string]string{
		"package.json": `{"name":"x","scripts":{"start":"node ."}}`,
	})

	res := NodeScript{gate: "build", scripts: []string{"build"}}.Run(context.Background(), dir)
	if res.Status != StatusSkipped {
		t.Fatalf("got %s (%s), want skipped", res.Status, res.Detail)
	}
	if !strings.Contains(res.Detail, "build") {
		t.Errorf("a skip must say what was not run: %q", res.Detail)
	}
}

// Required() being false governs only the missing-script case. A script that
// exists and fails must still fail the report, or the gate would be decorative.
func TestNodeScriptFailsWhenTheScriptFails(t *testing.T) {
	if !toolAvailable("npm") {
		t.Skip("npm is not installed")
	}

	dir := nodeFixture(t, map[string]string{
		"package.json": `{"name":"x","scripts":{"build":"exit 1"}}`,
	})

	res := NodeScript{gate: "build", scripts: []string{"build"}}.Run(context.Background(), dir)
	if res.Status != StatusFailed {
		t.Errorf("got %s (%s), want failed", res.Status, res.Detail)
	}
}

func TestNodeScriptPassesWhenTheScriptSucceeds(t *testing.T) {
	if !toolAvailable("npm") {
		t.Skip("npm is not installed")
	}

	dir := nodeFixture(t, map[string]string{
		"package.json": `{"name":"x","scripts":{"build":"exit 0"}}`,
	})

	res := NodeScript{gate: "build", scripts: []string{"build"}}.Run(context.Background(), dir)
	if res.Status != StatusPassed {
		t.Errorf("got %s (%s), want passed", res.Status, res.Detail)
	}
}

func TestNodeGatesFailOnAMissingManifest(t *testing.T) {
	if !toolAvailable("npm") {
		t.Skip("npm is not installed")
	}

	dir := nodeFixture(t, map[string]string{"index.js": "console.log(1)\n"})

	res := NodeScript{gate: "build", scripts: []string{"build"}}.Run(context.Background(), dir)
	if res.Status != StatusFailed {
		t.Errorf("a project with no package.json cannot be verified: got %s", res.Status)
	}
}

func TestNodeGatesFailOnAnUnparseableManifest(t *testing.T) {
	dir := nodeFixture(t, map[string]string{"package.json": "{not json"})

	if _, err := readPackageJSON(dir); err == nil {
		t.Error("invalid JSON in package.json must be reported")
	}
}

// A project with no tests at all is a gap worth blocking on: the premise of
// the system is that unverified work is not committed.
func TestNodeTestIsRequired(t *testing.T) {
	if !(NodeTest{}).Required() {
		t.Error("the test gate must be required")
	}
}

func TestNodeTestFailsWithNoTestScript(t *testing.T) {
	if !toolAvailable("npm") {
		t.Skip("npm is not installed")
	}

	dir := nodeFixture(t, map[string]string{
		"package.json": `{"name":"x","scripts":{"build":"exit 0"}}`,
	})

	res := NodeTest{}.Run(context.Background(), dir)
	if res.Status != StatusFailed {
		t.Errorf("got %s (%s), want failed", res.Status, res.Detail)
	}
}

func TestReadCoverageSummary(t *testing.T) {
	dir := nodeFixture(t, map[string]string{
		"coverage/coverage-summary.json": `{"total":{"statements":{"pct":87.5}}}`,
	})

	got, ok := readCoverageSummary(dir)
	if !ok {
		t.Fatal("the summary should have been read")
	}
	if got < 0.874 || got > 0.876 {
		t.Errorf("got %.3f, want 0.875", got)
	}
}

func TestReadCoverageSummaryWhenAbsent(t *testing.T) {
	dir := nodeFixture(t, map[string]string{"package.json": `{"name":"x"}`})
	if _, ok := readCoverageSummary(dir); ok {
		t.Error("no summary file should report no coverage")
	}
}

func TestReadCoverageSummaryIgnoresGarbage(t *testing.T) {
	dir := nodeFixture(t, map[string]string{
		"coverage/coverage-summary.json": "not json at all",
	})
	if _, ok := readCoverageSummary(dir); ok {
		t.Error("an unreadable summary must not be treated as a measurement")
	}
}

// Telling someone working on a TypeScript webapp that their change must be
// gofmt-clean is worse than saying nothing, so the prompt's bar comes from
// the gates that will actually run.
func TestExpectationsDescribeTheConfiguredGates(t *testing.T) {
	goExpectations := strings.Join(Expectations(GoGates(0.8)), "\n")
	if !strings.Contains(goExpectations, "gofmt") {
		t.Errorf("the Go gate set should mention gofmt:\n%s", goExpectations)
	}

	nodeExpectations := strings.Join(Expectations(NodeGates(0.8)), "\n")
	if strings.Contains(nodeExpectations, "gofmt") || strings.Contains(nodeExpectations, "go vet") {
		t.Errorf("the Node gate set must not demand Go tooling:\n%s", nodeExpectations)
	}
	if !strings.Contains(nodeExpectations, "typecheck") {
		t.Errorf("the Node gate set should mention typechecking:\n%s", nodeExpectations)
	}

	// The secret scan applies to every project type.
	for _, set := range [][]Gate{GoGates(0.8), NodeGates(0.8)} {
		if !strings.Contains(strings.Join(Expectations(set), "\n"), "credentials") {
			t.Error("every gate set should state that credentials must not be committed")
		}
	}
}

func TestExpectationsIncludeTheCoverageFloor(t *testing.T) {
	with := strings.Join(Expectations(NodeGates(0.8)), "\n")
	if !strings.Contains(with, "80%") {
		t.Errorf("a configured floor should be stated:\n%s", with)
	}

	without := strings.Join(Expectations(NodeGates(0)), "\n")
	if strings.Contains(without, "%") {
		t.Errorf("no floor configured should not invent one:\n%s", without)
	}
}

// A gate that cannot describe itself is omitted rather than guessed at.
func TestExpectationsSkipGatesThatCannotDescribeThemselves(t *testing.T) {
	got := Expectations([]Gate{&scriptedTestGate{}})
	if len(got) != 0 {
		t.Errorf("got %v, want nothing for a gate with no expectation", got)
	}
}

// scriptedTestGate implements Gate but not Expectant.
type scriptedTestGate struct{}

func (scriptedTestGate) Name() string   { return "scripted" }
func (scriptedTestGate) Required() bool { return true }
func (scriptedTestGate) Run(context.Context, string) Result {
	return Result{Gate: "scripted", Status: StatusPassed}
}
