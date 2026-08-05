// Package review asks Claude to critique a change before it is committed.
//
// Quality gates prove a change compiles, passes its tests and is formatted.
// They cannot tell whether the change actually does what the task asked, whether
// it introduced a security hole that happens to compile, or whether it solved
// the problem in a way the next person will regret. That judgement is what this
// package asks for, from several perspectives at once, and it runs between the
// gates passing and the commit being made.
//
// Two design choices are load-bearing. The review is one prompt covering every
// perspective rather than one call per perspective: six calls cost six times the
// quota for six partial views of the same diff, where a single reviewer holding
// the whole change can see how a performance choice creates a security problem.
// And a review that cannot be parsed is never treated as approval — a
// malformed or missing verdict blocks, because "the reviewer did not answer"
// and "the reviewer approved" must never collapse into the same outcome.
package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Severity ranks a finding. Only Critical and Major block a commit; Minor
// findings are reported so they are visible without stopping work that is
// otherwise sound.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityMajor    Severity = "major"
	SeverityMinor    Severity = "minor"
)

// Blocking reports whether a finding at this severity should stop a commit.
func (s Severity) Blocking() bool {
	return s == SeverityCritical || s == SeverityMajor
}

// Perspective is one reviewer's viewpoint on a change.
type Perspective string

const (
	PerspectiveDeveloper     Perspective = "developer"
	PerspectiveReviewer      Perspective = "reviewer"
	PerspectiveSecurity      Perspective = "security"
	PerspectivePerformance   Perspective = "performance"
	PerspectiveQA            Perspective = "qa"
	PerspectiveDocumentation Perspective = "documentation"
)

// Perspectives is the full set applied to every review, in the order they
// appear in the prompt and the report.
var Perspectives = []Perspective{
	PerspectiveDeveloper,
	PerspectiveReviewer,
	PerspectiveSecurity,
	PerspectivePerformance,
	PerspectiveQA,
	PerspectiveDocumentation,
}

// Finding is one problem a reviewer identified.
type Finding struct {
	Perspective Perspective
	Severity    Severity
	Location    string
	Description string
	Suggestion  string
}

// Review is the outcome of one review pass.
type Review struct {
	Approved bool
	Findings []Finding
	Summary  string
	Duration time.Duration
	// Raw is the reviewer's unparsed response, kept so a human can read what
	// was actually said when the parsed form looks wrong.
	Raw string
}

// Blocking returns the findings that prevent a commit.
func (r *Review) Blocking() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Severity.Blocking() {
			out = append(out, f)
		}
	}
	return out
}

// ErrUnparseable reports that the reviewer's response could not be read as a
// verdict. It is deliberately distinct from a rejection: a caller must be able
// to tell "the reviewer found problems" from "the review did not happen".
var ErrUnparseable = errors.New("the review response could not be parsed")

// Claude runs the review prompt. Narrowed to one method so the reviewer can be
// tested without a session manager.
type Claude interface {
	LaunchSession(ctx context.Context, prompt string) (*Response, error)
}

// Response is the reviewer's reply, narrowed from the session result to the
// two fields a review depends on.
type Response struct {
	Output string
	Errors []string
}

// Change is what the reviewer is asked to assess.
type Change struct {
	TaskID      string
	Title       string
	Description string
	Diff        string
}

// Reviewer performs multi-perspective self-review.
type Reviewer struct {
	claude Claude
}

// New creates a reviewer.
func New(claude Claude) *Reviewer {
	return &Reviewer{claude: claude}
}

// Review asks for a critique of one change.
//
// An empty diff is approved without consulting Claude: there is nothing to
// review, and asking anyway would spend quota to be told so.
func (r *Reviewer) Review(ctx context.Context, change Change) (*Review, error) {
	started := time.Now()

	if strings.TrimSpace(change.Diff) == "" {
		return &Review{
			Approved: true,
			Summary:  "No changes to review.",
			Duration: time.Since(started),
		}, nil
	}

	resp, err := r.claude.LaunchSession(ctx, BuildReviewPrompt(change))
	if err != nil {
		return nil, fmt.Errorf("review failed: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("review reported errors: %s", strings.Join(resp.Errors, "; "))
	}

	result, err := ParseReview(resp.Output)
	if err != nil {
		return nil, err
	}

	result.Duration = time.Since(started)
	return result, nil
}
