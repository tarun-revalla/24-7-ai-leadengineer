package recovery

import (
	"fmt"
	"strings"
	"time"
)

// Render formats the report for a terminal.
func (r *Report) Render() string {
	var b strings.Builder
	line := func(format string, args ...any) {
		fmt.Fprintf(&b, format, args...)
		b.WriteString("\n")
	}

	if !r.Interrupted {
		line("No interrupted work detected.")
		if r.GitErr != nil {
			line("")
			line("Warning: could not inspect the repository: %v", r.GitErr)
		}
		r.renderCheckpoints(line)
		return b.String()
	}

	line("Task %s was interrupted: %s", r.Task.ID, r.Task.Title)
	if r.Current != nil && r.Current.Status != "" {
		line("  last recorded stage: %s (%.0f%% complete)", r.Current.Status, r.Current.Progress*100)
	}
	if r.Current != nil {
		for _, e := range r.Current.Errors {
			line("  recorded error: %s", e)
		}
	}

	line("")
	switch {
	case r.GitErr != nil:
		line("Could not inspect the repository: %v", r.GitErr)
		line("Resolve that before continuing; the tree's state is unknown.")

	case r.HasConflicts:
		line("Unresolved merge conflicts are present.")
		line("Resolve them (`git status` will list the affected files), then run `leadengineer start`.")

	case r.TreeClean:
		line("The working tree has no uncommitted changes outside .ai/.")
		line("Safe to resume: run `leadengineer start`. It will select this task again")
		line("and begin its current stage from the start.")

	default:
		line("The working tree has uncommitted changes: %d staged, %d modified, %d untracked.",
			r.StagedCount, r.ModifiedCount, r.UntrackedCount)
		line("These may be partial work from the interrupted task, or unrelated changes.")
		line("")
		line("This system will not discard them automatically — review them yourself:")
		line("  git status")
		line("  git diff")
		line("Once the tree is clean (commit what you want to keep, revert or remove the")
		line("rest), run `leadengineer start` to resume.")
	}

	r.renderCheckpoints(line)

	return b.String()
}

func (r *Report) renderCheckpoints(line func(string, ...any)) {
	line("")
	line("Checkpoints")

	if r.CheckpointErr != nil {
		line("  unreadable: %v", r.CheckpointErr)
		return
	}

	if r.LatestCheckpoint == nil {
		line("  none recorded")
		return
	}

	line("  latest:  %s (%s ago)",
		r.LatestCheckpoint.ID, time.Since(r.LatestCheckpoint.Timestamp).Truncate(time.Second))
	if r.LatestCheckpoint.TaskID != "" {
		line("  task:    %s (%.0f%%)", r.LatestCheckpoint.TaskID, r.LatestCheckpoint.Progress*100)
	}
	line("  stored:  %d", r.CheckpointCount)
	if r.CorruptCheckpoints > 0 {
		line("  %d corrupt checkpoint(s) present; the newest intact one is used above", r.CorruptCheckpoints)
	}
}
