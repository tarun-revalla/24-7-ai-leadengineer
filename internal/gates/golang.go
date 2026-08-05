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

	coverage, measured := parseCoverage(out)
	if !measured {
		// Every package lacking tests is itself a coverage failure when a
		// floor is configured; reporting it as passing would hide the gap.
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: "no coverage was measured", Output: out,
		}
	}

	if coverage < g.MinCoverage {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: fmt.Sprintf("coverage %.1f%% is below the %.1f%% floor",
				coverage*100, g.MinCoverage*100),
			Output: out,
		}
	}

	return Result{
		Gate: g.Name(), Status: StatusPassed, Duration: time.Since(started),
		Detail: fmt.Sprintf("coverage %.1f%%", coverage*100),
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

// parseCoverage returns the lowest package coverage in a test run.
//
// The minimum is used rather than an average because a floor is meant to
// guarantee that no package falls below it; averaging lets a well-covered
// package hide an untested one.
func parseCoverage(output string) (float64, bool) {
	matches := coveragePattern.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return 0, false
	}

	lowest := -1.0
	for _, m := range matches {
		value, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		if lowest < 0 || value < lowest {
			lowest = value
		}
	}

	if lowest < 0 {
		return 0, false
	}

	return lowest / 100, true
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
