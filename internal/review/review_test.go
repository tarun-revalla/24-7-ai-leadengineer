package review

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeClaude struct {
	prompts  []string
	response *Response
	err      error
}

func (f *fakeClaude) LaunchSession(ctx context.Context, prompt string) (*Response, error) {
	f.prompts = append(f.prompts, prompt)
	if f.err != nil {
		return nil, f.err
	}
	if f.response != nil {
		return f.response, nil
	}
	return &Response{Output: `{"approved": true, "summary": "fine"}`}, nil
}

func sampleChange() Change {
	return Change{
		TaskID:      "T-1",
		Title:       "Add a thing",
		Description: "The thing must exist",
		Diff:        "--- a/main.go\n+++ b/main.go\n+func thing() {}\n",
	}
}

func TestReviewApprovesSoundChange(t *testing.T) {
	c := &fakeClaude{response: &Response{
		Output: `{"approved": true, "summary": "Does what the task asked.", "findings": []}`,
	}}

	r, err := New(c).Review(context.Background(), sampleChange())
	if err != nil {
		t.Fatalf("Review failed: %v", err)
	}
	if !r.Approved {
		t.Error("a clean review should approve")
	}
	if len(r.Findings) != 0 {
		t.Errorf("unexpected findings: %+v", r.Findings)
	}
}

func TestReviewBlocksOnCriticalFinding(t *testing.T) {
	c := &fakeClaude{response: &Response{Output: `{
		"approved": false,
		"summary": "Leaks a token.",
		"findings": [{
			"perspective": "security",
			"severity": "critical",
			"location": "main.go:12",
			"description": "The API token is written to the log",
			"suggestion": "Redact it"
		}]
	}`}}

	r, err := New(c).Review(context.Background(), sampleChange())
	if err != nil {
		t.Fatalf("Review failed: %v", err)
	}
	if r.Approved {
		t.Error("a critical finding must block")
	}
	if len(r.Blocking()) != 1 {
		t.Fatalf("blocking findings: got %d, want 1", len(r.Blocking()))
	}
	if r.Blocking()[0].Perspective != PerspectiveSecurity {
		t.Errorf("perspective: got %q, want security", r.Blocking()[0].Perspective)
	}
}

// Minor findings exist to be reported, not to stop work that is otherwise
// sound; treating them as blocking would make every review a repair cycle.
func TestMinorFindingsDoNotBlock(t *testing.T) {
	c := &fakeClaude{response: &Response{Output: `{
		"approved": true,
		"findings": [{"perspective": "documentation", "severity": "minor",
		              "description": "A comment could be clearer"}]
	}`}}

	r, err := New(c).Review(context.Background(), sampleChange())
	if err != nil {
		t.Fatalf("Review failed: %v", err)
	}
	if !r.Approved {
		t.Error("a minor finding should not block the commit")
	}
	if len(r.Findings) != 1 {
		t.Error("a minor finding should still be reported")
	}
}

// A response that approves while reporting a critical problem is
// self-contradictory. The reading that does not commit is the safe one.
func TestContradictoryApprovalIsOverruled(t *testing.T) {
	c := &fakeClaude{response: &Response{Output: `{
		"approved": true,
		"findings": [{"perspective": "security", "severity": "critical",
		              "description": "Command injection via user input"}]
	}`}}

	r, err := New(c).Review(context.Background(), sampleChange())
	if err != nil {
		t.Fatalf("Review failed: %v", err)
	}
	if r.Approved {
		t.Error("approval must not survive a critical finding in the same response")
	}
}

// The central safety property of this package: a review that did not produce a
// readable verdict must never be mistaken for one that approved.
func TestUnparseableResponseIsNotApproval(t *testing.T) {
	for _, output := range []string{
		"",
		"I looked at the change and it seems fine to me.",
		"{ this is not json",
		`{"summary": "no verdict field"}`,
	} {
		c := &fakeClaude{response: &Response{Output: output}}

		r, err := New(c).Review(context.Background(), sampleChange())
		if !errors.Is(err, ErrUnparseable) {
			t.Errorf("output %q: got (%v, %v), want ErrUnparseable", output, r, err)
		}
		if r != nil && r.Approved {
			t.Errorf("output %q was treated as an approval", output)
		}
	}
}

func TestEmptyDiffSkipsTheReview(t *testing.T) {
	c := &fakeClaude{}
	change := sampleChange()
	change.Diff = "   \n  "

	r, err := New(c).Review(context.Background(), change)
	if err != nil {
		t.Fatalf("Review failed: %v", err)
	}
	if !r.Approved {
		t.Error("there is nothing to reject when nothing changed")
	}
	if len(c.prompts) != 0 {
		t.Error("no quota should be spent reviewing an empty change")
	}
}

func TestSessionFailureIsReported(t *testing.T) {
	c := &fakeClaude{err: errors.New("claude crashed")}

	if _, err := New(c).Review(context.Background(), sampleChange()); err == nil {
		t.Fatal("a failed session must be reported, not silently approved")
	}
}

func TestReportedErrorsFailTheReview(t *testing.T) {
	c := &fakeClaude{response: &Response{Errors: []string{"context window exceeded"}}}

	_, err := New(c).Review(context.Background(), sampleChange())
	if err == nil {
		t.Fatal("errors reported by Claude must fail the review")
	}
	if !strings.Contains(err.Error(), "context window exceeded") {
		t.Errorf("the reported error should surface: %v", err)
	}
}

func TestParseExtractsJSONFromFencedProse(t *testing.T) {
	output := "I reviewed the change.\n\n```json\n" +
		`{"approved": false, "summary": "s", "findings": [` +
		`{"perspective": "qa", "severity": "major", "description": "No test covers the error path"}]}` +
		"\n```\n\nLet me know if you want more detail."

	r, err := ParseReview(output)
	if err != nil {
		t.Fatalf("ParseReview failed: %v", err)
	}
	if r.Approved {
		t.Error("a major finding must block")
	}
	if len(r.Findings) != 1 || r.Findings[0].Perspective != PerspectiveQA {
		t.Errorf("findings not parsed from the fenced block: %+v", r.Findings)
	}
}

// A description mentioning a brace must not throw off the object scan.
func TestParseHandlesBracesInsideStrings(t *testing.T) {
	output := `{"approved": true, "summary": "The map literal map[string]int{} is fine"}`

	r, err := ParseReview(output)
	if err != nil {
		t.Fatalf("ParseReview failed: %v", err)
	}
	if !strings.Contains(r.Summary, "map[string]int{}") {
		t.Errorf("summary lost its content: %q", r.Summary)
	}
}

func TestParseHandlesEscapedQuotes(t *testing.T) {
	output := `{"approved": true, "summary": "It says \"hello\" now"}`

	r, err := ParseReview(output)
	if err != nil {
		t.Fatalf("ParseReview failed: %v", err)
	}
	if !strings.Contains(r.Summary, `"hello"`) {
		t.Errorf("escaped quotes were mishandled: %q", r.Summary)
	}
}

// An unrecognised severity means the reviewer flagged something the format did
// not anticipate. Downgrading it to non-blocking would let a typo wave a real
// problem through.
func TestUnknownSeverityIsTreatedAsBlocking(t *testing.T) {
	r, err := ParseReview(`{"approved": true, "findings": [
		{"perspective": "developer", "severity": "showstopper", "description": "Wrong output"}]}`)
	if err != nil {
		t.Fatalf("ParseReview failed: %v", err)
	}
	if r.Findings[0].Severity != SeverityMajor {
		t.Errorf("severity: got %q, want major", r.Findings[0].Severity)
	}
	if r.Approved {
		t.Error("an unknown severity must block rather than be waved through")
	}
}

func TestUnknownPerspectiveFallsBackToReviewer(t *testing.T) {
	r, err := ParseReview(`{"approved": true, "findings": [
		{"perspective": "architecture", "severity": "minor", "description": "Consider a package split"}]}`)
	if err != nil {
		t.Fatalf("ParseReview failed: %v", err)
	}
	if r.Findings[0].Perspective != PerspectiveReviewer {
		t.Errorf("perspective: got %q, want the reviewer fallback", r.Findings[0].Perspective)
	}
	if !r.Approved {
		t.Error("an unknown perspective must not change whether the finding blocks")
	}
}

// A finding with no description cannot be acted on, and counting it would
// block a commit with nothing to show the operator.
func TestFindingsWithoutDescriptionAreDropped(t *testing.T) {
	r, err := ParseReview(`{"approved": true, "findings": [
		{"perspective": "qa", "severity": "critical", "description": "   "}]}`)
	if err != nil {
		t.Fatalf("ParseReview failed: %v", err)
	}
	if len(r.Findings) != 0 {
		t.Errorf("an empty finding should be dropped: %+v", r.Findings)
	}
	if !r.Approved {
		t.Error("an empty finding must not block")
	}
}

func TestSeverityBlocking(t *testing.T) {
	for severity, want := range map[Severity]bool{
		SeverityCritical: true,
		SeverityMajor:    true,
		SeverityMinor:    false,
	} {
		if got := severity.Blocking(); got != want {
			t.Errorf("%s.Blocking() = %v, want %v", severity, got, want)
		}
	}
}

func TestReviewPromptCoversEveryPerspective(t *testing.T) {
	prompt := BuildReviewPrompt(sampleChange())

	for _, p := range Perspectives {
		if !strings.Contains(prompt, string(p)) {
			t.Errorf("the prompt omits the %s perspective", p)
		}
	}
	for _, want := range []string{"T-1", "func thing()", "approved", "critical"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt is missing %q", want)
		}
	}
	// A reviewer that edits files turns a read-only pass into an unreviewed
	// change.
	if !strings.Contains(prompt, "Do not modify any files") {
		t.Error("the prompt must forbid editing during review")
	}
}

func TestRepairPromptCarriesOnlyBlockingFindings(t *testing.T) {
	r := &Review{Findings: []Finding{
		{Perspective: PerspectiveSecurity, Severity: SeverityCritical, Description: "Token in the log"},
		{Perspective: PerspectiveDocumentation, Severity: SeverityMinor, Description: "Comment could be clearer"},
	}}

	prompt := BuildReviewRepairPrompt(sampleChange(), r)

	if !strings.Contains(prompt, "Token in the log") {
		t.Error("the repair prompt must carry the blocking finding")
	}
	if strings.Contains(prompt, "Comment could be clearer") {
		t.Error("minor findings must not be sent for repair; the commit is not waiting on them")
	}
	if !strings.Contains(prompt, "Do not commit") {
		t.Error("the repair prompt must not authorise a commit")
	}
}

func TestRenderShowsVerdictAndFindings(t *testing.T) {
	r := &Review{
		Approved: false,
		Summary:  "Two problems.",
		Findings: []Finding{{
			Perspective: PerspectiveSecurity,
			Severity:    SeverityCritical,
			Location:    "main.go:12",
			Description: "Token in the log",
			Suggestion:  "Redact it",
		}},
	}

	out := r.Render()
	for _, want := range []string{"CHANGES REQUESTED", "Two problems.", "critical", "security", "main.go:12", "Redact it"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered review is missing %q:\n%s", want, out)
		}
	}
}

func TestRenderApprovedReview(t *testing.T) {
	out := (&Review{Approved: true, Summary: "Looks right."}).Render()

	if !strings.Contains(out, "approved") {
		t.Errorf("an approval should say so:\n%s", out)
	}
	if strings.Contains(out, "Findings") {
		t.Errorf("a clean review should not print an empty findings section:\n%s", out)
	}
}

func TestFindingSummaryNamesPerspectiveAndProblem(t *testing.T) {
	got := FindingSummary([]Finding{
		{Perspective: PerspectiveSecurity, Description: "Token in the log"},
		{Perspective: PerspectiveQA, Description: "No test for the error path"},
	})

	for _, want := range []string{"security", "Token in the log", "qa", "No test for the error path"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q is missing %q", got, want)
		}
	}
}
