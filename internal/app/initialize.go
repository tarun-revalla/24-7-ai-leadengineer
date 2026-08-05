package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/memory"
	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// InitResult reports what initialisation changed.
type InitResult struct {
	Created []string
	Skipped []string
}

// Initialize prepares a project's state directory.
//
// It is idempotent and never destructive: a document that already exists is
// left untouched and reported as skipped. Re-running init on a live project
// must not discard its backlog, decisions or in-flight task.
func (a *App) Initialize(ctx context.Context, projectName string) (*InitResult, error) {
	result := &InitResult{}

	for _, dir := range []string{"checkpoints", "sessions", "logs", "metrics"} {
		if err := os.MkdirAll(a.StatePath(dir), 0o755); err != nil {
			return nil, fmt.Errorf("failed to create %s directory: %w", dir, err)
		}
	}

	if projectName == "" {
		projectName = filepath.Base(a.ProjectPath)
	}

	now := time.Now().UTC()

	docs := []struct {
		file   string
		exists func() bool
		create func() error
	}{
		{
			file:   "PROJECT.md",
			exists: func() bool { return a.documentReadable(ctx, "PROJECT.md") },
			create: func() error {
				return a.Memory.SaveProject(ctx, &interfaces.ProjectMetadata{
					Name:    projectName,
					Purpose: "Describe what this project is for.",
					Constraints: []string{
						"Never commit code that fails a quality gate",
						"Never continue silently after a failure",
					},
				})
			},
		},
		{
			file:   "BACKLOG.md",
			exists: func() bool { return a.documentReadable(ctx, "BACKLOG.md") },
			create: func() error {
				return a.Memory.SaveBacklog(ctx, &interfaces.Backlog{})
			},
		},
		{
			file:   "CURRENT.md",
			exists: func() bool { return a.documentReadable(ctx, "CURRENT.md") },
			create: func() error {
				return a.Memory.SaveCurrent(ctx, &interfaces.CurrentTask{
					Status:    "not-started",
					StartedAt: now,
				})
			},
		},
		{
			file:   "CHANGELOG.md",
			exists: func() bool { return a.documentReadable(ctx, "CHANGELOG.md") },
			create: func() error {
				return a.Memory.SaveChangelog(ctx, &interfaces.Changelog{})
			},
		},
	}

	for _, d := range docs {
		if d.exists() {
			result.Skipped = append(result.Skipped, d.file)
			continue
		}
		if err := d.create(); err != nil {
			return nil, fmt.Errorf("failed to create %s: %w", d.file, err)
		}
		result.Created = append(result.Created, d.file)
	}

	// Prose-only documents carry no machine state, so they are written
	// directly rather than through the metadata path.
	prose := map[string]string{
		"ROADMAP.md":     memory.DefaultRoadmapBody(),
		"ENGINEERING.md": memory.DefaultEngineeringBody(),
	}
	for name, body := range prose {
		if a.fileExists(name) {
			result.Skipped = append(result.Skipped, name)
			continue
		}
		if err := a.Memory.WriteFile(ctx, name, body); err != nil {
			return nil, fmt.Errorf("failed to create %s: %w", name, err)
		}
		result.Created = append(result.Created, name)
	}

	if !a.fileExists("DECISIONS.md") {
		if err := a.Memory.WriteFile(ctx, "DECISIONS.md", memory.EmptyDecisionsDocument()); err != nil {
			return nil, fmt.Errorf("failed to create DECISIONS.md: %w", err)
		}
		result.Created = append(result.Created, "DECISIONS.md")
	} else {
		result.Skipped = append(result.Skipped, "DECISIONS.md")
	}

	return result, nil
}

// documentReadable reports whether a memory document exists and parses.
//
// A file whose header is unreadable is still treated as present: overwriting
// it would destroy whatever state it holds. Repairing it is a human decision,
// which `status` surfaces.
func (a *App) documentReadable(ctx context.Context, name string) bool {
	if !a.fileExists(name) {
		return false
	}

	_, err := a.Memory.ReadFile(ctx, name)
	return err == nil
}

func (a *App) fileExists(name string) bool {
	_, err := os.Stat(a.StatePath(name))
	return err == nil
}

// IsUninitialized reports whether the project has no usable memory yet.
func IsUninitialized(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, memory.ErrNoFrontMatter)
}
