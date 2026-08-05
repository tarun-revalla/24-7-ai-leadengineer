package review

import (
	"fmt"
	"strings"
)

// BuildReviewPrompt asks for a critique of one change from every perspective.
//
// The prompt spends most of its length on what each perspective is responsible
// for, because a request to "review this code" produces a summary of what the
// code does. Naming the questions each viewpoint should ask is what turns it
// into a review. The response format is specified exactly, since a verdict that
// cannot be parsed is treated as a failure rather than an approval.
func BuildReviewPrompt(change Change) string {
	var b strings.Builder

	b.WriteString("Review the following change critically. Do not modify any files; " +
		"this is a review, and your entire response is the review.\n\n")

	fmt.Fprintf(&b, "## Task %s\n\n%s\n", change.TaskID, change.Title)
	if desc := strings.TrimSpace(change.Description); desc != "" {
		fmt.Fprintf(&b, "\n%s\n", desc)
	}

	b.WriteString("\n## The change\n\n```diff\n")
	b.WriteString(change.Diff)
	if !strings.HasSuffix(change.Diff, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("```\n")

	b.WriteString(`
## Perspectives

Assess the change from each of these, and attribute every finding to the one
it came from:

- **developer** — Does this actually do what the task asked? Is the approach
  sound, or does it work by accident? Is there a materially simpler way?
- **reviewer** — Would you approve this in a pull request? Look for unhandled
  errors, missed edge cases, misleading names, and logic that reads correctly
  but is not.
- **security** — Injection, path traversal, unvalidated input crossing a trust
  boundary, credentials or tokens in source or logs, unsafe permissions,
  secrets echoed into output.
- **performance** — Unbounded growth, work repeated inside a loop that belongs
  outside it, an operation whose cost scales with something that has no limit.
- **qa** — Is the new behaviour tested, including its failure paths? Would
  these tests actually fail if the behaviour regressed, or do they only assert
  that the code ran?
- **documentation** — Is anything non-obvious left unexplained? Does an
  existing comment or document now contradict the code?

## What counts as a finding

Report a problem only when you can name the concrete consequence. Style
preferences, hypothetical future requirements, and restatements of what the
code does are not findings. An empty findings list is the correct answer for a
sound change — approving good work is as much a part of reviewing as blocking
bad work.

Severity:
- **critical** — data loss, a security hole, a repository left corrupt, or the
  change not doing what the task asked.
- **major** — a real defect that will surface in ordinary use.
- **minor** — worth mentioning, not worth blocking.

critical and major block the commit; minor does not.

## Response format

Respond with a single JSON object and nothing else:

` + "```json" + `
{
  "approved": true,
  "summary": "One or two sentences on the change overall.",
  "findings": [
    {
      "perspective": "security",
      "severity": "critical",
      "location": "internal/thing/file.go:42",
      "description": "What is wrong and what it causes.",
      "suggestion": "What to do about it."
    }
  ]
}
` + "```" + `

Set "approved" to false if any critical or major finding is present, true
otherwise. The "findings" array is empty when there is nothing to report.
`)

	return b.String()
}

// BuildReviewRepairPrompt asks for the blocking findings to be fixed.
//
// Only blocking findings are carried across. Handing back minor findings too
// would invite a repair cycle spent on changes nobody asked for, in a pass
// whose purpose is to clear what is actually blocking the commit.
func BuildReviewRepairPrompt(change Change, r *Review) string {
	var b strings.Builder

	fmt.Fprintf(&b, "A review of task %s (%s) found problems that must be fixed.\n\n",
		change.TaskID, change.Title)

	for _, f := range r.Blocking() {
		fmt.Fprintf(&b, "## %s (%s)\n\n", f.Description, f.Severity)
		if f.Location != "" {
			fmt.Fprintf(&b, "Location: %s\n\n", f.Location)
		}
		fmt.Fprintf(&b, "Raised from the %s perspective.\n", f.Perspective)
		if f.Suggestion != "" {
			fmt.Fprintf(&b, "\nSuggested fix: %s\n", f.Suggestion)
		}
		b.WriteString("\n")
	}

	b.WriteString(`## How to fix

Address each finding at its root. Do not delete or weaken tests, and do not
suppress a checker to make a problem disappear.

Keep the change confined to what the findings identify. Do not commit; the
quality gates and this review both run again afterwards.
`)

	return b.String()
}

// Render formats a review for a human reader.
func (r *Review) Render() string {
	var b strings.Builder

	verdict := "approved"
	if !r.Approved {
		verdict = "CHANGES REQUESTED"
	}
	fmt.Fprintf(&b, "Review: %s", verdict)
	if r.Duration > 0 {
		fmt.Fprintf(&b, " (%s)", r.Duration.Truncate(1e6))
	}
	b.WriteString("\n")

	if r.Summary != "" {
		fmt.Fprintf(&b, "\n%s\n", r.Summary)
	}

	if len(r.Findings) == 0 {
		return b.String()
	}

	b.WriteString("\nFindings:\n")
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "  [%s] %s", f.Severity, f.Perspective)
		if f.Location != "" {
			fmt.Fprintf(&b, " %s", f.Location)
		}
		fmt.Fprintf(&b, "\n    %s\n", f.Description)
		if f.Suggestion != "" {
			fmt.Fprintf(&b, "    → %s\n", f.Suggestion)
		}
	}

	return b.String()
}

// FindingSummary names the blocking findings in one line, for an error message.
func FindingSummary(findings []Finding) string {
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, fmt.Sprintf("%s: %s", f.Perspective, f.Description))
	}
	return strings.Join(parts, "; ")
}
