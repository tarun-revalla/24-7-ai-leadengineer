package memory

import "github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"

// Default bodies seed a memory document the first time it is written.
//
// The body is prose for humans and for Claude's context; the front matter above
// it is the machine state. Once a document exists its body is preserved across
// writes, so these values apply only at creation.

const defaultProjectBody = `# Project

Purpose, scope and constraints. Claude reads this file to understand what this
project is and what it must not do.

## Constraints

Record hard limits here — things that must remain true regardless of what any
individual task asks for.
`

const defaultBacklogBody = `# Backlog

Work not yet started, highest priority first. The planner selects from the
entries in the front matter above; this body is for the reasoning behind them.

Priority runs 1 (critical) to 10 (trivial).
`

const defaultCurrentBody = `# Current Work

The task in flight. On restart the system reads the front matter above to
determine what was interrupted and where to resume.

Leave notes here that would help a fresh session pick up mid-task.
`

const defaultChangelogBody = `# Changelog

Completed work, newest first. Each entry records the task, the commit that
carried it, and a summary of what changed.
`

const defaultDecisionsBody = `# Decisions

Architectural decisions and their rationale. Recording why a choice was made
prevents a later session from silently reversing it.
`

const defaultRoadmapBody = `# Roadmap

Milestones and their intended order. The backlog holds individual tasks; this
file holds the shape of the work they add up to.
`

const defaultEngineeringBody = `# Engineering Standards

Conventions this project holds itself to: formatting, linting, test coverage,
commit style, and the quality gates every task must pass before it is committed.
`

// DefaultRoadmapBody returns the seed content for a new ROADMAP.md.
func DefaultRoadmapBody() string { return defaultRoadmapBody }

// DefaultEngineeringBody returns the seed content for a new ENGINEERING.md.
func DefaultEngineeringBody() string { return defaultEngineeringBody }

// EmptyDecisionsDocument returns a DECISIONS.md holding no decisions.
func EmptyDecisionsDocument() string {
	rendered, err := renderDocument(&decisionSet{Decisions: []interfaces.Decision{}}, defaultDecisionsBody)
	if err != nil {
		// The input is a fixed literal, so encoding cannot fail in practice;
		// returning the body alone still yields a valid, readable file.
		return defaultDecisionsBody
	}
	return rendered
}
