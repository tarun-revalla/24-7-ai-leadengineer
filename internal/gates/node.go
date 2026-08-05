package gates

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// NodeGates returns the standard checks for a JavaScript or TypeScript
// project, cheapest first.
//
// Node has no universal toolchain the way Go does. There is no `node fmt`, no
// canonical test runner, and a project's "build" is whatever its package.json
// says it is. So these gates read the manifest and run the scripts a project
// actually defines, rather than assuming a command that may not exist.
//
// A script that is not defined is reported as skipped with the reason, not
// silently passed and not treated as a failure: a library with no build step
// is a normal project, not a broken one. What is never skipped is the secret
// scan, which reads files and needs no toolchain at all.
//
// minCoverage of 0 disables the coverage threshold.
func NodeGates(minCoverage float64) []Gate {
	return []Gate{
		SecretScan{},
		NodeInstall{},
		NodeScript{gate: "format", scripts: []string{"format:check", "format-check", "prettier:check"}},
		NodeScript{gate: "typecheck", scripts: []string{"typecheck", "type-check", "tsc"}},
		NodeScript{gate: "build", scripts: []string{"build"}},
		NodeTest{MinCoverage: minCoverage},
		NodeScript{gate: "lint", scripts: []string{"lint"}},
	}
}

// packageJSON is the part of a manifest these gates read.
type packageJSON struct {
	Scripts map[string]string `json:"scripts"`
}

// readPackageJSON loads the manifest from a project directory.
func readPackageJSON(dir string) (*packageJSON, error) {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil, err
	}

	pkg := &packageJSON{}
	if err := json.Unmarshal(data, pkg); err != nil {
		return nil, fmt.Errorf("package.json is not valid JSON: %w", err)
	}
	return pkg, nil
}

// packageManager picks the tool a project is actually using.
//
// The lockfile decides, not what happens to be installed: running `npm
// install` in a pnpm project rewrites the lockfile and can resolve different
// versions than the ones the project was tested against.
func packageManager(dir string) string {
	for _, candidate := range []struct{ lockfile, tool string }{
		{"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"},
		{"bun.lockb", "bun"},
		{"package-lock.json", "npm"},
	} {
		if _, err := os.Stat(filepath.Join(dir, candidate.lockfile)); err == nil {
			return candidate.tool
		}
	}
	return "npm"
}

// firstScript returns the first of names the manifest defines.
func (p *packageJSON) firstScript(names []string) (string, bool) {
	for _, name := range names {
		if _, ok := p.Scripts[name]; ok {
			return name, true
		}
	}
	return "", false
}

// NodeInstall makes sure dependencies are present before anything tries to
// use them.
//
// Without this every later gate fails with a module-not-found error that says
// nothing about the change being verified. It is required: a project whose
// dependencies cannot be installed cannot be verified at all.
type NodeInstall struct{}

func (NodeInstall) Name() string   { return "install" }
func (NodeInstall) Required() bool { return true }

func (g NodeInstall) Run(ctx context.Context, dir string) Result {
	started := time.Now()

	pm := packageManager(dir)
	if !toolAvailable(pm) {
		return missing(g, pm, started)
	}

	if _, err := readPackageJSON(dir); err != nil {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: fmt.Sprintf("could not read package.json: %v", err),
		}
	}

	// The frozen-lockfile form installs exactly what the lockfile pins and
	// fails rather than silently updating it. A verification step that
	// rewrites the lockfile would be changing the thing it is checking.
	args := map[string][]string{
		"pnpm": {"install", "--frozen-lockfile"},
		"yarn": {"install", "--frozen-lockfile"},
		"bun":  {"install", "--frozen-lockfile"},
		"npm":  {"ci"},
	}[pm]

	out, err := command{pm, args}.run(ctx, dir)
	if err != nil {
		// `npm ci` requires a lockfile; a project without one is not broken,
		// it just has not been installed reproducibly, so fall back rather
		// than blocking every task on it.
		if pm == "npm" {
			if out, err = (command{"npm", []string{"install"}}).run(ctx, dir); err == nil {
				return Result{
					Gate: g.Name(), Status: StatusPassed, Duration: time.Since(started),
					Detail: "installed without a lockfile",
				}
			}
		}
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: "dependency installation failed", Output: out,
		}
	}

	return Result{Gate: g.Name(), Status: StatusPassed, Duration: time.Since(started), Detail: pm}
}

// NodeScript runs a package.json script under the project's package manager.
//
// scripts lists the names to look for, in order — projects spell the same
// intent differently ("typecheck", "type-check", "tsc"), and demanding one
// exact name would make the gate skip on projects that do have the check.
type NodeScript struct {
	gate    string
	scripts []string
}

func (g NodeScript) Name() string { return g.gate }

// Required is false because the script may legitimately not exist. That does
// not weaken the gate: a script that IS defined and fails still fails the
// report. Required only governs what happens when there is nothing to run.
func (NodeScript) Required() bool { return false }

func (g NodeScript) Run(ctx context.Context, dir string) Result {
	started := time.Now()

	pm := packageManager(dir)
	if !toolAvailable(pm) {
		return missing(g, pm, started)
	}

	pkg, err := readPackageJSON(dir)
	if err != nil {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: fmt.Sprintf("could not read package.json: %v", err),
		}
	}

	script, ok := pkg.firstScript(g.scripts)
	if !ok {
		return Result{
			Gate: g.Name(), Status: StatusSkipped, Duration: time.Since(started),
			Detail: fmt.Sprintf("package.json defines no %s script", g.scripts[0]),
		}
	}

	out, runErr := command{pm, []string{"run", script}}.run(ctx, dir)
	if runErr != nil {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: fmt.Sprintf("%s run %s failed", pm, script), Output: out,
		}
	}

	return Result{
		Gate: g.Name(), Status: StatusPassed, Duration: time.Since(started),
		Detail: script,
	}
}

// NodeTest runs the project's test script and enforces a coverage floor.
type NodeTest struct {
	// MinCoverage is the fraction of statements that must be covered. Zero
	// disables the threshold.
	MinCoverage float64
}

func (NodeTest) Name() string { return "test" }

// Required is true: unlike a build step, a project with no tests at all is a
// gap worth blocking on, and this system's whole premise is that unverified
// work is not committed.
func (NodeTest) Required() bool { return true }

func (g NodeTest) Run(ctx context.Context, dir string) Result {
	started := time.Now()

	pm := packageManager(dir)
	if !toolAvailable(pm) {
		return missing(g, pm, started)
	}

	pkg, err := readPackageJSON(dir)
	if err != nil {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: fmt.Sprintf("could not read package.json: %v", err),
		}
	}

	script, ok := pkg.firstScript([]string{"test:coverage", "coverage", "test"})
	if !ok {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: "package.json defines no test script",
		}
	}

	out, runErr := command{pm, []string{"run", script}}.run(ctx, dir)
	if runErr != nil {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: "tests failed", Output: out,
		}
	}

	if g.MinCoverage <= 0 {
		return Result{Gate: g.Name(), Status: StatusPassed, Duration: time.Since(started)}
	}

	coverage, found := readCoverageSummary(dir)
	if !found {
		// Every JavaScript runner reports coverage differently, and scraping
		// their console output would break on a version bump. istanbul's
		// summary file is the one format they agree on, so its absence means
		// coverage was not measured — reported as a pass with the gap named,
		// rather than a failure for a project whose tests all passed.
		return Result{
			Gate: g.Name(), Status: StatusPassed, Duration: time.Since(started),
			Detail: "tests passed; coverage not measured " +
				"(enable the json-summary reporter to enforce the floor)",
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

// coverageSummary is istanbul's coverage-summary.json, which jest, vitest, c8
// and nyc all emit under the json-summary reporter.
type coverageSummary struct {
	Total struct {
		Statements struct {
			Pct float64 `json:"pct"`
		} `json:"statements"`
	} `json:"total"`
}

// readCoverageSummary returns the overall statement coverage, if a summary was
// written.
func readCoverageSummary(dir string) (float64, bool) {
	for _, rel := range []string{
		filepath.Join("coverage", "coverage-summary.json"),
		filepath.Join("coverage", "coverage-final.json"),
	} {
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			continue
		}

		var summary coverageSummary
		if err := json.Unmarshal(data, &summary); err != nil {
			continue
		}
		if pct := summary.Total.Statements.Pct; pct > 0 {
			return pct / 100, true
		}
	}
	return 0, false
}

// Expectation states what this gate will require, for the implement prompt.
func (g NodeScript) Expectation() string {
	switch g.gate {
	case "format":
		return "be formatted (the format:check script, if the project defines one)"
	case "typecheck":
		return "typecheck cleanly (the typecheck script, if the project defines one)"
	case "build":
		return "build successfully (the build script, if the project defines one)"
	case "lint":
		return "pass the lint script, if the project defines one"
	}
	return ""
}

func (g NodeTest) Expectation() string {
	if g.MinCoverage > 0 {
		return fmt.Sprintf("pass the test script, at or above %.0f%% statement coverage",
			g.MinCoverage*100)
	}
	return "pass the test script"
}

// SecretScan applies to every project type, so its expectation lives with the
// gate rather than in either language's set.
func (SecretScan) Expectation() string {
	return "contain no credentials, keys or tokens"
}
