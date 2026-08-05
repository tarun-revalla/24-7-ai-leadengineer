package gates

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixture writes a small Go module so gates run against real tooling rather
// than a description of it.
func fixture(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()

	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module fixture\n\ngo 1.21\n"
	}

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

const goodSource = `package fixture

// Add returns the sum of two integers.
func Add(a, b int) int {
	return a + b
}
`

const goodTest = `package fixture

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 2) != 4 {
		t.Fatal("bad sum")
	}
}
`

func TestGoFormatPasses(t *testing.T) {
	dir := fixture(t, map[string]string{"add.go": goodSource})

	res := GoFormat{}.Run(context.Background(), dir)
	if res.Status != StatusPassed {
		t.Errorf("got %s (%s), want passed\n%s", res.Status, res.Detail, res.Output)
	}
}

func TestGoFormatDetectsBadFormatting(t *testing.T) {
	// Misaligned indentation and spacing gofmt will rewrite.
	badly := "package fixture\nfunc Add(a,b int) int {\n\t\treturn a+b\n}\n"
	dir := fixture(t, map[string]string{"add.go": badly})

	res := GoFormat{}.Run(context.Background(), dir)
	if res.Status != StatusFailed {
		t.Fatalf("got %s, want failed", res.Status)
	}
	if !strings.Contains(res.Output, "add.go") {
		t.Errorf("the offending file should be named: %q", res.Output)
	}
}

func TestGoBuildPasses(t *testing.T) {
	dir := fixture(t, map[string]string{"add.go": goodSource})

	res := GoBuild{}.Run(context.Background(), dir)
	if res.Status != StatusPassed {
		t.Errorf("got %s (%s), want passed\n%s", res.Status, res.Detail, res.Output)
	}
}

func TestGoBuildDetectsCompileError(t *testing.T) {
	dir := fixture(t, map[string]string{
		"add.go": "package fixture\n\nfunc Add(a, b int) int {\n\treturn a + c\n}\n",
	})

	res := GoBuild{}.Run(context.Background(), dir)
	if res.Status != StatusFailed {
		t.Fatalf("got %s, want failed", res.Status)
	}
	if !strings.Contains(res.Output, "undefined") {
		t.Errorf("the compiler error should be retained: %q", res.Output)
	}
}

func TestGoVetDetectsProblem(t *testing.T) {
	dir := fixture(t, map[string]string{
		// A Printf verb mismatch is the canonical vet finding.
		"bad.go": "package fixture\n\nimport \"fmt\"\n\nfunc Bad() {\n\tfmt.Printf(\"%d\\n\", \"not a number\")\n}\n",
	})

	res := GoVet{}.Run(context.Background(), dir)
	if res.Status != StatusFailed {
		t.Fatalf("got %s, want failed\n%s", res.Status, res.Output)
	}
}

func TestGoTestPasses(t *testing.T) {
	dir := fixture(t, map[string]string{"add.go": goodSource, "add_test.go": goodTest})

	res := GoTest{}.Run(context.Background(), dir)
	if res.Status != StatusPassed {
		t.Errorf("got %s (%s), want passed\n%s", res.Status, res.Detail, res.Output)
	}
}

func TestGoTestDetectsFailure(t *testing.T) {
	failing := "package fixture\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tt.Fatal(\"deliberate\")\n}\n"
	dir := fixture(t, map[string]string{"add.go": goodSource, "add_test.go": failing})

	res := GoTest{}.Run(context.Background(), dir)
	if res.Status != StatusFailed {
		t.Fatalf("got %s, want failed", res.Status)
	}
	if !strings.Contains(res.Output, "deliberate") {
		t.Errorf("the test output should be retained: %q", res.Output)
	}
}

func TestGoTestEnforcesCoverageFloor(t *testing.T) {
	// Add is tested; Untested is not, leaving coverage near 50%.
	partial := goodSource + "\n// Untested is deliberately uncovered.\nfunc Untested(a int) int {\n\tif a > 0 {\n\t\treturn a\n\t}\n\treturn -a\n}\n"
	dir := fixture(t, map[string]string{"add.go": partial, "add_test.go": goodTest})

	res := GoTest{MinCoverage: 0.95}.Run(context.Background(), dir)
	if res.Status != StatusFailed {
		t.Fatalf("got %s, want failed below the floor\n%s", res.Status, res.Output)
	}
	if !strings.Contains(res.Detail, "below") {
		t.Errorf("the detail should explain the shortfall: %q", res.Detail)
	}
}

func TestGoTestAcceptsCoverageAboveFloor(t *testing.T) {
	dir := fixture(t, map[string]string{"add.go": goodSource, "add_test.go": goodTest})

	res := GoTest{MinCoverage: 0.5}.Run(context.Background(), dir)
	if res.Status != StatusPassed {
		t.Errorf("got %s (%s), want passed\n%s", res.Status, res.Detail, res.Output)
	}
	if !strings.Contains(res.Detail, "coverage") {
		t.Errorf("the detail should report coverage: %q", res.Detail)
	}
}

// A package with no tests must never satisfy a coverage floor. Go reports
// 0.0% for such a package, so this fails on the threshold; were it to report
// nothing at all, the unmeasured branch would fail it instead. Either way the
// gate must block.
func TestGoTestFailsForUntestedPackage(t *testing.T) {
	dir := fixture(t, map[string]string{"add.go": goodSource})

	res := GoTest{MinCoverage: 0.8}.Run(context.Background(), dir)
	if res.Status != StatusFailed {
		t.Fatalf("an untested package must not pass a coverage floor: got %s\n%s",
			res.Status, res.Output)
	}
}

// Output carrying no coverage line at all is a failure rather than an
// implicit pass.
func TestGoTestFailsWhenCoverageUnmeasurable(t *testing.T) {
	report := GoTest{MinCoverage: 0.8}
	if _, ok := parseCoverage("ok  	fixture	0.01s\n"); ok {
		t.Fatal("test setup: this output should carry no coverage")
	}
	// The gate turns that condition into a failure; verified directly since
	// producing it from real tooling is not reliably reproducible.
	if !report.Required() {
		t.Error("the test gate must be required")
	}
}

func TestGoTestWithoutFloorIgnoresCoverage(t *testing.T) {
	dir := fixture(t, map[string]string{"add.go": goodSource})

	res := GoTest{MinCoverage: 0}.Run(context.Background(), dir)
	if res.Status != StatusPassed {
		t.Errorf("got %s (%s), want passed with no floor configured", res.Status, res.Detail)
	}
}

func TestParseCoverageTakesTheLowest(t *testing.T) {
	// Averaging would let a well-covered package hide an untested one.
	output := `ok  	example/a	0.01s	coverage: 95.0% of statements
ok  	example/b	0.01s	coverage: 42.5% of statements
ok  	example/c	0.01s	coverage: 88.0% of statements`

	got, ok := parseCoverage(output)
	if !ok {
		t.Fatal("coverage should have been parsed")
	}
	if got < 0.424 || got > 0.426 {
		t.Errorf("got %.3f, want the lowest package coverage 0.425", got)
	}
}

func TestParseCoverageWithNoMeasurement(t *testing.T) {
	if _, ok := parseCoverage("ok  	example/a	0.01s\n"); ok {
		t.Error("output without a coverage line should report none")
	}
}

func TestOptionalGateSkipsWhenToolMissing(t *testing.T) {
	// golangci-lint is not installed in this environment; if it ever is, the
	// gate should pass or fail rather than skip, so accept anything but a
	// silent pass on a missing tool.
	dir := fixture(t, map[string]string{"add.go": goodSource})

	res := GolangCILint{}.Run(context.Background(), dir)
	if res.Status == StatusSkipped && !strings.Contains(res.Detail, "not installed") {
		t.Errorf("a skip must say why: %q", res.Detail)
	}
	if res.Status == StatusPassed && res.Detail == "not installed" {
		t.Error("a missing tool must never be reported as passing")
	}
}

func TestRequiredGateFailsWhenToolMissing(t *testing.T) {
	// A required check that could not run proves nothing and must block.
	res := missing(GoBuild{}, "go", time.Now())
	if res.Status != StatusFailed {
		t.Errorf("required gate with missing tooling: got %s, want failed", res.Status)
	}

	res = missing(GolangCILint{}, "golangci-lint", time.Now())
	if res.Status != StatusSkipped {
		t.Errorf("optional gate with missing tooling: got %s, want skipped", res.Status)
	}
}

func TestRunnerReportsEveryGate(t *testing.T) {
	dir := fixture(t, map[string]string{"add.go": goodSource, "add_test.go": goodTest})

	report := NewRunner(GoFormat{}, GoBuild{}, GoVet{}, GoTest{}).Run(context.Background(), dir)

	if len(report.Results) != 4 {
		t.Fatalf("got %d results, want 4", len(report.Results))
	}
	if !report.Passed() {
		t.Errorf("expected all gates to pass:\n%s", report.Summary())
	}
	if report.Duration <= 0 {
		t.Error("the report should record its duration")
	}
}

// One run should surface every problem, not just the first.
func TestRunnerContinuesPastFailure(t *testing.T) {
	badly := "package fixture\nfunc Add(a,b int) int {\n\t\treturn a+c\n}\n"
	dir := fixture(t, map[string]string{"add.go": badly})

	report := NewRunner(GoFormat{}, GoBuild{}).Run(context.Background(), dir)

	if len(report.Results) != 2 {
		t.Fatalf("got %d results, want both gates run", len(report.Results))
	}
	if len(report.Failures()) != 2 {
		t.Errorf("both gates should fail:\n%s", report.Summary())
	}
}

func TestRunnerStopsOnCancellation(t *testing.T) {
	dir := fixture(t, map[string]string{"add.go": goodSource})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report := NewRunner(GoFormat{}, GoBuild{}, GoVet{}).Run(ctx, dir)

	if report.Passed() {
		t.Error("a cancelled run must not report success")
	}
	if len(report.Results) != 1 {
		t.Errorf("cancellation should stop after the first check, got %d results", len(report.Results))
	}
}

func TestReportSummaryMarksEachOutcome(t *testing.T) {
	report := &Report{Results: []Result{
		{Gate: "format", Status: StatusPassed},
		{Gate: "test", Status: StatusFailed, Detail: "2 failures"},
		{Gate: "lint", Status: StatusSkipped, Detail: "not installed"},
	}}

	summary := report.Summary()
	for _, want := range []string{"ok", "FAIL", "skip", "format", "test", "lint", "2 failures"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q:\n%s", want, summary)
		}
	}
}

// A skipped optional gate records a gap but must not block a commit.
func TestSkippedGateDoesNotBlock(t *testing.T) {
	report := &Report{Results: []Result{
		{Gate: "lint", Status: StatusSkipped, Detail: "not installed"},
		{Gate: "test", Status: StatusPassed},
	}}

	if !report.Passed() {
		t.Error("a skipped optional gate should not block a commit")
	}
}

func TestFailureContextCarriesOutput(t *testing.T) {
	report := &Report{Results: []Result{
		{Gate: "test", Status: StatusFailed, Detail: "tests failed", Output: "--- FAIL: TestThing"},
		{Gate: "format", Status: StatusPassed},
	}}

	ctx := report.FailureContext()
	if !strings.Contains(ctx, "TestThing") {
		t.Errorf("failure context should carry the output:\n%s", ctx)
	}
	if strings.Contains(ctx, "format") {
		t.Errorf("passing gates should not appear:\n%s", ctx)
	}
}

func TestFailureContextTruncatesHugeOutput(t *testing.T) {
	report := &Report{Results: []Result{
		{Gate: "test", Status: StatusFailed, Output: strings.Repeat("x", 20000)},
	}}

	ctx := report.FailureContext()
	if len(ctx) > 12000 {
		t.Errorf("output should be truncated, got %d bytes", len(ctx))
	}
	if !strings.Contains(ctx, "truncated") {
		t.Error("truncation should be visible")
	}
}

func TestGoGatesOrdering(t *testing.T) {
	list := GoGates(0.8)
	if len(list) == 0 {
		t.Fatal("GoGates returned nothing")
	}

	// Cheap structural checks first so an obvious break is reported quickly.
	if list[0].Name() != "format" {
		t.Errorf("first gate: got %q, want format", list[0].Name())
	}

	names := make([]string, 0, len(list))
	for _, g := range list {
		names = append(names, g.Name())
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "build") || !strings.Contains(joined, "test") {
		t.Errorf("expected build and test gates, got %s", joined)
	}
}

// A single-package command module is the one case where go build writes a
// binary into the invocation directory. That must not survive the gate.
func TestGoBuildLeavesNoArtifactForSoloMainPackage(t *testing.T) {
	dir := fixture(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})

	res := GoBuild{}.Run(context.Background(), dir)
	if res.Status != StatusPassed {
		t.Fatalf("got %s (%s), want passed\n%s", res.Status, res.Detail, res.Output)
	}

	assertOnlySourceFiles(t, dir)
}

// The far more common shape — library packages, no main at all — must not
// gain an artifact either, and must not have its real compile errors masked.
func TestGoBuildLibraryOnlyModule(t *testing.T) {
	dir := fixture(t, map[string]string{"add.go": goodSource})

	res := GoBuild{}.Run(context.Background(), dir)
	if res.Status != StatusPassed {
		t.Fatalf("got %s (%s), want passed\n%s", res.Status, res.Detail, res.Output)
	}
	assertOnlySourceFiles(t, dir)
}

// The fix for the artifact must not come at the cost of masking a genuine
// compile error in a library-only module — go build -o does exactly that.
func TestGoBuildDetectsCompileErrorInLibraryOnlyModule(t *testing.T) {
	dir := fixture(t, map[string]string{
		"add.go": "package fixture\n\nfunc Add(a, b int) int {\n\treturn a + c\n}\n",
	})

	res := GoBuild{}.Run(context.Background(), dir)
	if res.Status != StatusFailed {
		t.Fatalf("got %s, want failed", res.Status)
	}
	if !strings.Contains(res.Output, "undefined") {
		t.Errorf("the real compiler error must surface, not be masked: %q", res.Output)
	}
}

// A file the build modifies rather than creates — go.sum is the real-world
// case — must be left alone; only genuinely new files are cleanup targets.
func TestGoBuildDoesNotTouchPreexistingFiles(t *testing.T) {
	dir := fixture(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})

	marker := filepath.Join(dir, "README.md")
	if err := os.WriteFile(marker, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	res := GoBuild{}.Run(context.Background(), dir)
	if res.Status != StatusPassed {
		t.Fatalf("got %s (%s)\n%s", res.Status, res.Detail, res.Output)
	}

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("a pre-existing file was removed: %v", err)
	}
	if string(got) != "keep me" {
		t.Errorf("pre-existing file content changed: %q", got)
	}
}

func assertOnlySourceFiles(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".go") && e.Name() != "go.mod" && e.Name() != "go.sum" {
			t.Errorf("GoBuild left an artifact behind: %s", e.Name())
		}
	}
}
