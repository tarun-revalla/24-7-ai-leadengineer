package executor

import (
	"fmt"
	"strings"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/gates"
	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// BuildImplementPrompt describes one task and the boundaries around it.
//
// The scope constraints are not decoration. This system decides what to do and
// Claude decides how; a prompt that leaves scope open invites changes the
// backlog never asked for, which then get committed under a task that does not
// describe them.
// expectations describe what the configured gates will require. They are
// passed in rather than hardcoded because the bar depends on the project: a
// change to a TypeScript webapp is not held to gofmt.
func BuildImplementPrompt(
	project *interfaces.ProjectMetadata,
	task interfaces.BacklogTask,
	expectations []string,
) string {
	var b strings.Builder

	b.WriteString("Implement exactly one task in this repository.\n\n")

	if project != nil {
		fmt.Fprintf(&b, "## Project\n\n%s\n", project.Name)
		if project.Purpose != "" {
			fmt.Fprintf(&b, "\n%s\n", project.Purpose)
		}
		if len(project.Constraints) > 0 {
			b.WriteString("\nConstraints that always apply:\n")
			for _, c := range project.Constraints {
				fmt.Fprintf(&b, "- %s\n", c)
			}
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "## Task %s\n\n%s\n", task.ID, task.Title)
	if task.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", task.Description)
	}

	b.WriteString(`
## Scope

- Change only what this task requires. Unrelated refactors, renames and
  reformatting belong to their own tasks.
- Do not commit. The surrounding system stages and commits after its quality
  gates pass.
- Do not modify anything under .ai/; that state belongs to the system.

## Definition of done
`)

	if len(expectations) > 0 {
		b.WriteString("\nThe change will be checked automatically and must:\n\n")
		for _, e := range expectations {
			fmt.Fprintf(&b, "- %s\n", e)
		}
	}

	b.WriteString(`
Add tests for behaviour you introduce, including the failure paths. If you
cannot complete the task, say so plainly and explain what blocks it rather
than leaving partial work behind.
`)

	return b.String()
}

// BuildRepairPrompt asks for failing gates to be fixed.
//
// It carries the tools' own output, since that is what identifies the problem.
// It also states what must not be done: the cheapest way to make a failing
// gate pass is to weaken it, which would defeat the point of running it.
func BuildRepairPrompt(task interfaces.BacklogTask, report *gates.Report) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Quality gates failed for task %s (%s). Fix them.\n\n", task.ID, task.Title)
	b.WriteString(report.FailureContext())

	b.WriteString(`## How to fix

Fix the underlying problem. Do not delete, skip or weaken tests, lower a
coverage threshold, or silence a checker to make a gate pass — a gate that
passes because it stopped checking is worse than one that fails.

Keep the change minimal and confined to what the failures identify. Do not
commit; the gates run again afterwards.
`)

	return b.String()
}

// BuildCommitMessage renders the commit for a completed task.
//
// The subject stays within the conventional 72 columns so git log output and
// hosting interfaces do not truncate it mid-word.
func BuildCommitMessage(task interfaces.BacklogTask) string {
	subject := strings.TrimSpace(task.Title)
	if subject == "" {
		subject = "complete task " + task.ID
	}
	subject = firstLine(subject)

	prefix := task.ID + ": "
	if limit := 72 - len(prefix); len(subject) > limit && limit > 0 {
		subject = strings.TrimSpace(subject[:limit])
	}

	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(subject)

	if desc := strings.TrimSpace(task.Description); desc != "" {
		b.WriteString("\n\n")
		b.WriteString(desc)
	}

	b.WriteString("\n\nAll quality gates passed before this commit.\n")

	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
