package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/checkpoint"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/quota"
	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// Status is a point-in-time view of the system.
//
// Each section records its own error rather than failing the whole report: an
// unreadable backlog should not hide a healthy git state, and an operator
// diagnosing a broken project needs to see every part that still works.
type Status struct {
	ProjectPath string
	Initialized bool

	Project            *interfaces.ProjectMetadata
	ProjectErr         error
	Current            *interfaces.CurrentTask
	CurrentErr         error
	Backlog            *interfaces.Backlog
	BacklogErr         error
	Git                *interfaces.GitStatus
	GitErr             error
	LastCommit         *interfaces.GitCommit
	Checkpoint         *interfaces.CheckpointState
	CheckpointErr      error
	CheckpointCount    int
	CorruptCheckpoints []string

	Quota          *quota.State
	QuotaAvailable bool
	QuotaRemaining time.Duration
}

// Status gathers the current state of the project.
func (a *App) Status(ctx context.Context) *Status {
	s := &Status{ProjectPath: a.ProjectPath}

	s.Project, s.ProjectErr = a.Memory.GetProject(ctx)
	s.Initialized = s.ProjectErr == nil

	s.Current, s.CurrentErr = a.Memory.GetCurrent(ctx)
	s.Backlog, s.BacklogErr = a.Memory.GetBacklog(ctx)

	s.Git, s.GitErr = a.Git.Status(ctx)
	if s.GitErr == nil {
		// A repository with no commits has no HEAD; that is not an error worth
		// reporting, so the field simply stays empty.
		if c, err := a.Git.GetLastCommit(ctx); err == nil {
			s.LastCommit = c
		}
	}

	s.Checkpoint, s.CheckpointErr = a.Checkpoints.GetLatestCheckpoint(ctx)
	if errors.Is(s.CheckpointErr, checkpoint.ErrNoCheckpoints) {
		// No checkpoints yet is a normal state for a new project.
		s.CheckpointErr = nil
	}

	if list, err := a.Checkpoints.ListCheckpoints(ctx); err == nil {
		s.CheckpointCount = len(list)
		for _, md := range list {
			if err := a.Checkpoints.ValidateCheckpoint(ctx, md.ID); err != nil {
				s.CorruptCheckpoints = append(s.CorruptCheckpoints, md.ID)
			}
		}
	}

	s.Quota = a.Quota.Current(ctx)
	s.QuotaAvailable, s.QuotaRemaining = a.Quota.Available(ctx)

	return s
}

// NextTasks returns the highest-priority unfinished work, most urgent first.
func (s *Status) NextTasks(limit int) []interfaces.BacklogTask {
	if s.Backlog == nil {
		return nil
	}

	open := make([]interfaces.BacklogTask, 0, len(s.Backlog.Tasks))
	for _, t := range s.Backlog.Tasks {
		if t.Status != "done" && t.Status != "completed" {
			open = append(open, t)
		}
	}

	sort.SliceStable(open, func(i, j int) bool {
		return open[i].Priority < open[j].Priority
	})

	if limit > 0 && len(open) > limit {
		open = open[:limit]
	}
	return open
}

// Render formats the status for a terminal.
func (s *Status) Render() string {
	out := &lineWriter{}

	out.line("Project:  %s", s.ProjectPath)

	if !s.Initialized {
		out.line("")
		out.line("Not initialised. Run `leadengineer init` to create the state directory.")
		if s.ProjectErr != nil {
			out.line("  reason: %v", s.ProjectErr)
		}
		return out.String()
	}

	out.line("Name:     %s", s.Project.Name)
	if s.Project.Purpose != "" {
		out.line("Purpose:  %s", s.Project.Purpose)
	}

	out.section("Current task")
	switch {
	case s.CurrentErr != nil:
		out.line("  unreadable: %v", s.CurrentErr)
	case s.Current.TaskID == "":
		out.line("  none in progress")
	default:
		out.line("  %s  %s", s.Current.TaskID, s.Current.Title)
		out.line("  status:   %s (%.0f%% complete)", s.Current.Status, s.Current.Progress*100)
		if !s.Current.StartedAt.IsZero() {
			out.line("  started:  %s (%s ago)",
				s.Current.StartedAt.Format(time.RFC3339),
				truncateDuration(time.Since(s.Current.StartedAt)))
		}
		for _, e := range s.Current.Errors {
			out.line("  error:    %s", e)
		}
	}

	out.section("Backlog")
	if s.BacklogErr != nil {
		out.line("  unreadable: %v", s.BacklogErr)
	} else {
		next := s.NextTasks(5)
		out.line("  %d open of %d total", len(s.NextTasks(0)), len(s.Backlog.Tasks))
		for _, t := range next {
			out.line("  [P%d] %-12s %s", t.Priority, t.ID, t.Title)
		}
	}

	out.section("Repository")
	if s.GitErr != nil {
		out.line("  unreadable: %v", s.GitErr)
	} else {
		state := "clean"
		if !s.Git.IsClean {
			state = "dirty"
		}
		out.line("  branch:   %s (%s)", s.Git.Branch, state)
		if n := len(s.Git.StagedChanges); n > 0 {
			out.line("  staged:   %d file(s)", n)
		}
		if n := len(s.Git.UnstagedChanges); n > 0 {
			out.line("  modified: %d file(s)", n)
		}
		if n := len(s.Git.UntrackedFiles); n > 0 {
			out.line("  untracked: %d file(s)", n)
		}
		if s.Git.HasConflicts {
			out.line("  CONFLICTS present - resolution required")
		}
		if s.LastCommit != nil {
			out.line("  head:     %s %s", shortHash(s.LastCommit.Hash), s.LastCommit.Message)
		}
	}

	out.section("Checkpoints")
	if s.CheckpointErr != nil {
		out.line("  unreadable: %v", s.CheckpointErr)
	} else if s.Checkpoint == nil {
		out.line("  none recorded")
	} else {
		out.line("  latest:   %s", s.Checkpoint.ID)
		out.line("  taken:    %s (%s ago)",
			s.Checkpoint.Timestamp.Format(time.RFC3339),
			truncateDuration(time.Since(s.Checkpoint.Timestamp)))
		if s.Checkpoint.TaskID != "" {
			out.line("  task:     %s (%.0f%%)", s.Checkpoint.TaskID, s.Checkpoint.Progress*100)
		}
		out.line("  stored:   %d", s.CheckpointCount)
	}
	if n := len(s.CorruptCheckpoints); n > 0 {
		out.line("  %d corrupt checkpoint(s) present; recovery will skip them", n)
	}

	out.section("Claude quota")
	if s.QuotaAvailable {
		out.line("  available")
		if s.Quota != nil && s.Quota.ConsecutiveHits > 0 {
			out.line("  last limit: %s", s.Quota.DetectedAt.Format(time.RFC3339))
		}
	} else {
		out.line("  in cooldown, %s remaining", truncateDuration(s.QuotaRemaining))
		out.line("  resumes:  %s", s.Quota.ResumeAt.Format(time.RFC3339))
		if s.Quota.Reason != "" {
			out.line("  reason:   %s", s.Quota.Reason)
		}
		if s.Quota.ConsecutiveHits > 1 {
			out.line("  hits:     %d consecutive (cooldown widened)", s.Quota.ConsecutiveHits)
		}
		if s.Quota.LastCheckpoint != "" {
			out.line("  saved as: %s", s.Quota.LastCheckpoint)
		}
	}

	return out.String()
}

func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// truncateDuration renders a duration at a resolution appropriate to its size.
func truncateDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return d.Truncate(time.Second).String()
	case d < time.Hour:
		return d.Truncate(time.Minute).String()
	default:
		return d.Truncate(time.Hour).String()
	}
}

// lineWriter accumulates report lines.
type lineWriter struct {
	buf []byte
}

func (w *lineWriter) line(format string, args ...any) {
	w.buf = append(w.buf, []byte(fmt.Sprintf(format, args...))...)
	w.buf = append(w.buf, '\n')
}

func (w *lineWriter) section(title string) {
	w.line("")
	w.line("%s", title)
}

func (w *lineWriter) String() string {
	return string(w.buf)
}
