package gates

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GoGates returns the standard checks for a Go project, in the order a
// developer would want them: cheap structural checks first, so an obvious
// break is reported in seconds rather than after a full test run. SecretScan
// runs early alongside format for the same reason — it is a pure file read,
// no compilation required — and because a leaked credential is worth knowing
// about before spending time on anything else.
//
// minCoverage of 0 disables the coverage threshold.
func GoGates(minCoverage float64) []Gate {
	return []Gate{
		GoFormat{},
		SecretScan{},
		GoBuild{},
		GoVet{},
		GoTest{MinCoverage: minCoverage},
		GolangCILint{},
		GoSecurity{},
	}
}

// GoFormat checks that every file is gofmt-clean.
type GoFormat struct{}

func (GoFormat) Name() string   { return "format" }
func (GoFormat) Required() bool { return true }

func (g GoFormat) Run(ctx context.Context, dir string) Result {
	started := time.Now()

	if !toolAvailable("gofmt") {
		return missing(g, "gofmt", started)
	}

	// gofmt -l lists offending files and exits zero either way, so the file
	// list is the signal rather than the exit status.
	out, err := command{"gofmt", []string{"-l", "."}}.run(ctx, dir)
	if err != nil {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: fmt.Sprintf("gofmt could not run: %v", err), Output: out,
		}
	}

	files := nonEmptyLines(out)
	if len(files) > 0 {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: fmt.Sprintf("%d file(s) need formatting", len(files)),
			Output: strings.Join(files, "\n"),
		}
	}

	return Result{Gate: g.Name(), Status: StatusPassed, Duration: time.Since(started)}
}

// GoBuild checks that the module compiles.
type GoBuild struct{}

func (GoBuild) Name() string   { return "build" }
func (GoBuild) Required() bool { return true }

func (g GoBuild) Run(ctx context.Context, dir string) Result {
	started := time.Now()

	if !toolAvailable("go") {
		return missing(g, "go", started)
	}

	// go build ./... normally discards its output — except when the module
	// resolves to exactly one main package and nothing else, in which case it
	// writes a binary into dir. That case can't be routed around with -o:
	// passing -o when no main package is found makes go skip compilation
	// entirely and report "no main packages to build" instead of the real
	// error, which would silently defang this check for every library-only
	// module — the common case. So the build runs unmodified and any binary
	// it happens to leave is removed afterward: a verification step must not
	// leave the tree different from how it found it, since an untracked
	// binary would make every later run look dirty and refuse to start.
	before, snapErr := snapshotDir(dir)
	if snapErr != nil {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: fmt.Sprintf("failed to inspect the project directory: %v", snapErr),
		}
	}

	out, buildErr := command{"go", []string{"build", "./..."}}.run(ctx, dir)

	cleanupBuildArtifacts(dir, before)

	if buildErr != nil {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: "compilation failed", Output: out,
		}
	}

	return Result{Gate: g.Name(), Status: StatusPassed, Duration: time.Since(started)}
}

// snapshotDir records the regular files directly inside dir.
func snapshotDir(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			seen[e.Name()] = true
		}
	}
	return seen, nil
}

// cleanupBuildArtifacts removes regular files in dir that were not present in
// before. Only additions are touched — a file the build modified rather than
// created is left alone, since that is pre-existing behaviour of go build
// (go.sum, for instance) and not the artifact this exists to clean up.
func cleanupBuildArtifacts(dir string, before map[string]bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() || before[e.Name()] {
			continue
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}

// GoVet runs the standard static checks.
type GoVet struct{}

func (GoVet) Name() string   { return "vet" }
func (GoVet) Required() bool { return true }

func (g GoVet) Run(ctx context.Context, dir string) Result {
	started := time.Now()

	if !toolAvailable("go") {
		return missing(g, "go", started)
	}

	out, err := command{"go", []string{"vet", "./..."}}.run(ctx, dir)
	if err != nil {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: "vet reported problems", Output: out,
		}
	}

	return Result{Gate: g.Name(), Status: StatusPassed, Duration: time.Since(started)}
}

// GoTest runs the test suite with the race detector and enforces a coverage
// floor.
type GoTest struct {
	// MinCoverage is the fraction of statements that must be covered. Zero
	// disables the threshold.
	MinCoverage float64
}

func (GoTest) Name() string   { return "test" }
func (GoTest) Required() bool { return true }

func (g GoTest) Run(ctx context.Context, dir string) Result {
	started := time.Now()

	if !toolAvailable("go") {
		return missing(g, "go", started)
	}

	out, err := command{"go", []string{"test", "./...", "-race", "-cover"}}.run(ctx, dir)
	if err != nil {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: "tests failed", Output: out,
		}
	}

	if g.MinCoverage <= 0 {
		return Result{Gate: g.Name(), Status: StatusPassed, Duration: time.Since(started)}
	}

	cov := parseCoverage(out)
	if !cov.measured {
		// A floor is configured and not one package ran a test. Reporting that
		// as passing would hide the gap entirely.
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: "no coverage was measured", Output: out,
		}
	}

	if cov.lowest < g.MinCoverage {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: fmt.Sprintf("coverage %.1f%% is below the %.1f%% floor",
				cov.lowest*100, g.MinCoverage*100),
			Output: out,
		}
	}

	// Packages with no tests at all are named rather than counted, so the gap
	// is visible without making the floor unsatisfiable. Same principle as a
	// skipped optional gate: report what was not checked.
	detail := fmt.Sprintf("coverage %.1f%%", cov.lowest*100)
	if n := len(cov.untested); n > 0 {
		detail += fmt.Sprintf("; %d package(s) have no tests: %s",
			n, strings.Join(cov.untested, ", "))
	}

	return Result{
		Gate: g.Name(), Status: StatusPassed, Duration: time.Since(started),
		Detail: detail,
	}
}

// GolangCILint runs golangci-lint when it is installed.
type GolangCILint struct{}

func (GolangCILint) Name() string { return "lint" }

// Required is false: golangci-lint is a third-party tool that a project may
// legitimately not use. Its absence is recorded rather than fatal.
func (GolangCILint) Required() bool { return false }

func (g GolangCILint) Run(ctx context.Context, dir string) Result {
	started := time.Now()

	if !toolAvailable("golangci-lint") {
		return missing(g, "golangci-lint", started)
	}

	out, err := command{"golangci-lint", []string{"run", "./..."}}.run(ctx, dir)
	if err != nil {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: "lint reported problems", Output: out,
		}
	}

	return Result{Gate: g.Name(), Status: StatusPassed, Duration: time.Since(started)}
}

// GoSecurity runs gosec's static security analysis when it is installed.
//
// Optional, unlike SecretScan: gosec is a separate binary a deployment may
// not have installed, and its absence must be visible rather than silently
// treated as "no issues" — but it should not block a project that has not
// set it up. Where gosec looks for coding patterns known to be risky
// (unchecked errors on security-relevant calls, weak crypto, command
// injection shapes), SecretScan looks for already-leaked credentials; they
// catch different things and neither substitutes for the other.
type GoSecurity struct{}

func (GoSecurity) Name() string   { return "security" }
func (GoSecurity) Required() bool { return false }

func (g GoSecurity) Run(ctx context.Context, dir string) Result {
	started := time.Now()

	if !toolAvailable("gosec") {
		return missing(g, "gosec", started)
	}

	out, err := command{"gosec", []string{"-quiet", "./..."}}.run(ctx, dir)
	if err != nil {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: "gosec reported findings", Output: out,
		}
	}

	return Result{Gate: g.Name(), Status: StatusPassed, Duration: time.Since(started)}
}

// coveragePattern matches the percentage in "coverage: 87.5% of statements".
var coveragePattern = regexp.MustCompile(`coverage:\s+([0-9.]+)%\s+of statements`)

// coverageResult is what one `go test -cover` run reported.
type coverageResult struct {
	// lowest is the smallest coverage among packages that actually ran tests.
	lowest float64
	// measured is false when no package ran a test at all.
	measured bool
	// untested names packages that have statements but no test file. These do
	// not carry a coverage figure to compare against a floor — nothing ran —
	// so they are reported separately rather than counted as 0%.
	untested []string
}

// parseCoverage reads per-package coverage out of a test run.
//
// The minimum is used rather than an average because a floor is meant to
// guarantee that no package falls below it; averaging lets a well-covered
// package hide an untested one.
//
// `go test -cover` writes two different lines that both contain a coverage
// percentage, and conflating them makes the floor unsatisfiable. A package
// that ran tests reports "ok <pkg> <time> coverage: N%". A package with
// statements but no test file reports a bare "<pkg> coverage: 0.0%" — nothing
// executed, so that 0% measures nothing. Treating the second as a real
// reading means any module with an untested main package fails a coverage
// floor forever, no matter how well tested the rest of it is.
func parseCoverage(output string) coverageResult {
	var result coverageResult
	lowest := -1.0

	for _, line := range strings.Split(output, "\n") {
		m := coveragePattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		if !strings.HasPrefix(strings.TrimSpace(line), "ok") {
			result.untested = append(result.untested, packageName(line))
			continue
		}

		value, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		if lowest < 0 || value < lowest {
			lowest = value
		}
	}

	if lowest >= 0 {
		result.lowest = lowest / 100
		result.measured = true
	}

	return result
}

// packageName pulls the import path out of a `go test` result line, which
// separates its columns with tabs.
func packageName(line string) string {
	for _, field := range strings.Split(strings.TrimSpace(line), "\t") {
		if field = strings.TrimSpace(field); strings.Contains(field, "/") {
			return field
		}
	}
	return strings.TrimSpace(line)
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// Expectation statements, so the implement prompt states the bar this gate
// set actually enforces rather than a hardcoded one.

func (GoFormat) Expectation() string { return "be gofmt-clean" }
func (GoBuild) Expectation() string  { return "compile with `go build ./...`" }
func (GoVet) Expectation() string    { return "pass `go vet ./...`" }

func (g GoTest) Expectation() string {
	if g.MinCoverage > 0 {
		return fmt.Sprintf("pass `go test ./... -race`, keeping every package at or above %.0f%% coverage",
			g.MinCoverage*100)
	}
	return "pass `go test ./... -race`"
}

func (GolangCILint) Expectation() string { return "pass golangci-lint, if it is installed" }
