package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

func TestParseDocumentSplitsFrontMatterAndBody(t *testing.T) {
	raw := "---\nname: Demo\npriority: 3\n---\n\n# Heading\n\nProse here.\n"

	doc, err := parseDocument(raw)
	if err != nil {
		t.Fatalf("parseDocument failed: %v", err)
	}

	if !strings.Contains(doc.frontMatter, "name: Demo") {
		t.Errorf("front matter not captured: %q", doc.frontMatter)
	}
	if !strings.Contains(doc.body, "# Heading") {
		t.Errorf("body not captured: %q", doc.body)
	}
	if strings.Contains(doc.body, "name: Demo") {
		t.Error("front matter leaked into body")
	}
}

func TestParseDocumentWithoutFrontMatter(t *testing.T) {
	raw := "# Just Markdown\n\nNo header here.\n"

	doc, err := parseDocument(raw)
	if !errors.Is(err, ErrNoFrontMatter) {
		t.Fatalf("expected ErrNoFrontMatter, got %v", err)
	}
	// The body must still be recovered so a write can carry it forward.
	if !strings.Contains(doc.body, "Just Markdown") {
		t.Errorf("body should be preserved even without front matter: %q", doc.body)
	}
}

func TestParseDocumentUnterminatedFrontMatter(t *testing.T) {
	raw := "---\nname: Demo\n\n# Heading without a closing delimiter\n"

	doc, err := parseDocument(raw)
	if !errors.Is(err, ErrNoFrontMatter) {
		t.Fatalf("unterminated front matter must be refused, got %v", err)
	}
	if !strings.Contains(doc.body, "Heading without") {
		t.Error("body should be recoverable from a malformed document")
	}
}

func TestParseDocumentHandlesCRLF(t *testing.T) {
	raw := "---\r\nname: Demo\r\n---\r\n\r\n# Heading\r\n"

	doc, err := parseDocument(raw)
	if err != nil {
		t.Fatalf("parseDocument failed on CRLF input: %v", err)
	}
	if !strings.Contains(doc.frontMatter, "name: Demo") {
		t.Errorf("front matter not parsed from CRLF input: %q", doc.frontMatter)
	}
}

func TestParseDocumentBodyContainingDelimiter(t *testing.T) {
	// A horizontal rule in the prose must not be mistaken for a delimiter.
	raw := "---\nname: Demo\n---\n\n# Heading\n\nSection one.\n\n---\n\nSection two.\n"

	doc, err := parseDocument(raw)
	if err != nil {
		t.Fatalf("parseDocument failed: %v", err)
	}
	if strings.Contains(doc.frontMatter, "Heading") {
		t.Error("body content was absorbed into front matter")
	}
	if !strings.Contains(doc.body, "Section two") {
		t.Errorf("body truncated at a horizontal rule: %q", doc.body)
	}
}

func TestRenderDocumentRoundTrip(t *testing.T) {
	meta := &interfaces.ProjectMetadata{Name: "Demo", Purpose: "Testing"}
	body := "# Demo\n\nSome prose.\n"

	rendered, err := renderDocument(meta, body)
	if err != nil {
		t.Fatalf("renderDocument failed: %v", err)
	}

	doc, err := parseDocument(rendered)
	if err != nil {
		t.Fatalf("re-parse failed: %v", err)
	}

	out := &interfaces.ProjectMetadata{}
	if err := doc.decodeInto(out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if out.Name != "Demo" || out.Purpose != "Testing" {
		t.Errorf("metadata lost in round trip: %+v", out)
	}
	if !strings.Contains(doc.body, "Some prose.") {
		t.Errorf("body lost in round trip: %q", doc.body)
	}
}

func TestDecodeIntoEmptyFrontMatter(t *testing.T) {
	doc := &document{frontMatter: "   \n  ", body: "x"}

	if err := doc.decodeInto(&interfaces.ProjectMetadata{}); !errors.Is(err, ErrNoFrontMatter) {
		t.Errorf("expected ErrNoFrontMatter, got %v", err)
	}
}

// The defect this format was introduced to fix: writing machine state must not
// delete the prose a human (or Claude) wrote in the same file.
func TestSavePreservesHumanWrittenBody(t *testing.T) {
	tmp := t.TempDir()
	m, _ := New(tmp)
	ctx := context.Background()

	original := "# Project\n\nThis paragraph was written by a human and is expensive to lose.\n"
	seeded, _ := renderDocument(&interfaces.ProjectMetadata{Name: "Before"}, original)
	if err := m.WriteFile(ctx, "PROJECT.md", seeded); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if err := m.SaveProject(ctx, &interfaces.ProjectMetadata{Name: "After"}); err != nil {
		t.Fatalf("SaveProject failed: %v", err)
	}

	raw, _ := m.ReadFile(ctx, "PROJECT.md")
	if !strings.Contains(raw, "expensive to lose") {
		t.Fatalf("SaveProject destroyed the document body:\n%s", raw)
	}

	got, err := m.GetProject(ctx)
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}
	if got.Name != "After" {
		t.Errorf("Name: got %q, want After", got.Name)
	}
}

func TestSavePreservesBodyAcrossRepeatedWrites(t *testing.T) {
	tmp := t.TempDir()
	m, _ := New(tmp)
	ctx := context.Background()

	m.SaveCurrent(ctx, &interfaces.CurrentTask{TaskID: "t1"})

	raw, _ := m.ReadFile(ctx, "CURRENT.md")
	if !strings.Contains(raw, "# Current Work") {
		t.Fatalf("default body not seeded:\n%s", raw)
	}

	for i := 0; i < 5; i++ {
		if err := m.SaveCurrent(ctx, &interfaces.CurrentTask{TaskID: "t1", Progress: float64(i) / 5}); err != nil {
			t.Fatalf("SaveCurrent failed: %v", err)
		}
	}

	raw, _ = m.ReadFile(ctx, "CURRENT.md")
	if strings.Count(raw, "# Current Work") != 1 {
		t.Errorf("body duplicated or lost across writes:\n%s", raw)
	}
	if strings.Count(raw, delimiter) != 2 {
		t.Errorf("expected exactly one front matter block, got:\n%s", raw)
	}
}

// A document holding only prose is a configuration error, not empty state.
// Reporting it as empty would let the system run on silently missing context.
func TestReadingProseOnlyDocumentFails(t *testing.T) {
	tmp := t.TempDir()
	m, _ := New(tmp)
	ctx := context.Background()

	m.WriteFile(ctx, "PROJECT.md", "# Project\n\nHand written, no front matter.\n")

	if _, err := m.GetProject(ctx); !errors.Is(err, ErrNoFrontMatter) {
		t.Errorf("expected ErrNoFrontMatter, got %v", err)
	}
}

func TestSyncWithClaudeRejectsUnparseableDocument(t *testing.T) {
	tmp := t.TempDir()
	m, _ := New(tmp)
	ctx := context.Background()

	m.SaveProject(ctx, &interfaces.ProjectMetadata{Name: "Demo"})
	m.SaveBacklog(ctx, &interfaces.Backlog{})
	m.SaveCurrent(ctx, &interfaces.CurrentTask{})
	m.WriteFile(ctx, "ROADMAP.md", "# Roadmap")
	m.WriteFile(ctx, "DECISIONS.md", "---\ndecisions: []\n---\n")

	if err := m.SyncWithClaude(ctx); err != nil {
		t.Fatalf("SyncWithClaude failed on healthy memory: %v", err)
	}

	// Replace a document with prose only; sync must refuse rather than hand
	// Claude empty context.
	m.WriteFile(ctx, "CURRENT.md", "# Current\n\nno front matter\n")
	if err := m.SyncWithClaude(ctx); err == nil {
		t.Error("SyncWithClaude should fail when a document cannot be parsed")
	}
}

func TestSaveDecisionAppendsWithoutLoss(t *testing.T) {
	tmp := t.TempDir()
	m, _ := New(tmp)
	ctx := context.Background()

	for _, id := range []string{"ARCH-001", "ARCH-002", "ARCH-003"} {
		if err := m.SaveDecision(ctx, interfaces.Decision{ID: id, Title: id, Status: "accepted"}); err != nil {
			t.Fatalf("SaveDecision(%s) failed: %v", id, err)
		}
	}

	got, err := m.GetDecisions(ctx)
	if err != nil {
		t.Fatalf("GetDecisions failed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d decisions, want 3", len(got))
	}
	if got[0].ID != "ARCH-001" || got[2].ID != "ARCH-003" {
		t.Errorf("decision order not preserved: %v", got)
	}
}

// Appending to a document that cannot be read would silently drop whatever
// decisions it already held.
func TestSaveDecisionRefusesUnreadableDocument(t *testing.T) {
	tmp := t.TempDir()
	m, _ := New(tmp)
	ctx := context.Background()

	m.WriteFile(ctx, "DECISIONS.md", "---\ndecisions: [ this is not valid yaml\n---\n")

	err := m.SaveDecision(ctx, interfaces.Decision{ID: "ARCH-001"})
	if err == nil {
		t.Fatal("SaveDecision should refuse to append to an unparseable document")
	}
	if !strings.Contains(err.Error(), "refusing to append") {
		t.Errorf("unexpected error: %v", err)
	}
}
