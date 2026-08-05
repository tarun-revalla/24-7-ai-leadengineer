// Package toolchain works out how a project verifies itself.
//
// This is the piece that removes the tech-stack bottleneck. The alternative —
// a gate set compiled in per language — makes language support a property of
// this binary: a Rust project cannot be worked on until someone writes Go,
// reviews it, and ships a release. That is backwards. Every repository
// already knows how to build and test itself, and Claude can read a
// repository. So the knowledge is discovered from the project and recorded as
// project state, and the binary stays out of it.
//
// What Claude is asked for here is deliberately narrow: not "verify this
// project", but "read this repository and report the commands its own
// contributors would run". Discovery is a judgement call and belongs to the
// model. Enforcement is not, and stays in code that can be tested.
package toolchain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// Claude runs the detection prompt.
type Claude interface {
	LaunchSession(ctx context.Context, prompt string) (*Response, error)
}

// Response is the model's reply, narrowed to what detection reads.
type Response struct {
	Output string
	Errors []string
}

// Detector inspects a project and reports how to verify it.
type Detector struct {
	claude      Claude
	projectPath string
}

// New creates a detector.
func New(claude Claude, projectPath string) *Detector {
	return &Detector{claude: claude, projectPath: projectPath}
}

// Detect reads the repository and returns the toolchain it implies.
func (d *Detector) Detect(ctx context.Context) (*interfaces.Toolchain, error) {
	evidence, err := d.evidence()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect the project: %w", err)
	}

	resp, err := d.claude.LaunchSession(ctx, BuildDetectPrompt(evidence))
	if err != nil {
		return nil, fmt.Errorf("detection failed: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("detection reported errors: %s", strings.Join(resp.Errors, "; "))
	}

	t, err := ParseToolchain(resp.Output)
	if err != nil {
		return nil, err
	}

	t.DetectedAt = time.Now().UTC()
	return t, nil
}

// maxEvidenceEntries bounds the file listing handed to the model. A
// repository root with more entries than this is not more informative, and
// the manifests that identify a toolchain are always near the top.
const maxEvidenceEntries = 200

// evidence gathers what identifies a project's toolchain: the names at the
// repository root, plus the contents of the manifests that turn up there.
//
// Names alone are usually enough to identify the language, but not the
// commands — a Cargo.toml says "Rust" while a Makefile or a justfile may
// redefine what "test" means for that specific project. So manifests are
// read, and the model decides which of them actually governs.
func (d *Detector) evidence() (string, error) {
	entries, err := os.ReadDir(d.projectPath)
	if err != nil {
		return "", err
	}

	var names []string
	for _, e := range entries {
		name := e.Name()
		if name == ".git" || name == "node_modules" || name == "vendor" {
			continue
		}
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > maxEvidenceEntries {
		names = names[:maxEvidenceEntries]
	}

	var b strings.Builder
	b.WriteString("## Repository root\n\n")
	for _, n := range names {
		fmt.Fprintf(&b, "- %s\n", n)
	}

	for _, manifest := range manifestFiles {
		content, err := d.readCapped(manifest)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n```\n%s\n```\n", manifest, content)
	}

	return b.String(), nil
}

// manifestFiles are the files that commonly declare how a project is built
// and tested. Absent ones are skipped; this list only decides what is worth
// reading, never what the project is.
var manifestFiles = []string{
	"Makefile", "justfile", "Justfile", "Taskfile.yml",
	"package.json", "go.mod", "Cargo.toml", "pyproject.toml", "setup.py",
	"mix.exs", "Gemfile", "build.gradle", "build.gradle.kts", "pom.xml",
	"composer.json", "pubspec.yaml", "Package.swift", "build.zig",
	"CMakeLists.txt", "meson.build", "dune-project", "stack.yaml",
	"deno.json", "bun.lockb", "requirements.txt", "tox.ini",
	".github/workflows/ci.yml", ".github/workflows/ci.yaml",
	".github/workflows/test.yml", ".gitlab-ci.yml",
	"CONTRIBUTING.md",
}

// maxManifestBytes bounds one manifest's contribution to the prompt. A
// lockfile-sized document past this point is repetition, not signal.
const maxManifestBytes = 12_000

func (d *Detector) readCapped(rel string) (string, error) {
	data, err := os.ReadFile(filepath.Join(d.projectPath, rel))
	if err != nil {
		return "", err
	}
	if len(data) > maxManifestBytes {
		return string(data[:maxManifestBytes]) + "\n... truncated ...", nil
	}
	return string(data), nil
}
