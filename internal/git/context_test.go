package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A cancelled context must abort git operations rather than run them to
// completion, so a hung network call can be reclaimed.
func TestCancelledContextAbortsOperation(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := m.GetCommitHistory(ctx, 5); err == nil {
		t.Error("GetCommitHistory should fail on a cancelled context")
	}
	if _, err := m.Status(ctx); err == nil {
		t.Error("Status should fail on a cancelled context")
	}
	if _, err := m.GetLastCommit(ctx); err == nil {
		t.Error("GetLastCommit should fail on a cancelled context")
	}
}

func TestCancelledContextAbandonsPushRetry(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 4)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// No remote is configured, so the initial push fails and retry begins.
	// With a cancelled context it must abandon immediately rather than sleep
	// through the full backoff schedule.
	err := m.Push(ctx, "main")
	if err == nil {
		t.Fatal("Push should fail without a remote")
	}
	if !strings.Contains(err.Error(), "abandoned") && !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected cancellation to end the retry loop, got %v", err)
	}
}

// A failing git command must not be reported as a clean repository: acting on
// that would let the system skip a commit it needed to make.
func TestStatusFailsLoudlyOutsideRepository(t *testing.T) {
	notARepo := t.TempDir()
	m := New(notARepo, "Test User", "test@example.com", true, 3)

	status, err := m.Status(context.Background())
	if err == nil {
		t.Fatalf("Status must fail outside a repository, got %+v", status)
	}
	if status != nil {
		t.Error("no status should be returned alongside an error")
	}
}

func TestGetLastCommitFailsOutsideRepository(t *testing.T) {
	notARepo := t.TempDir()
	m := New(notARepo, "Test User", "test@example.com", true, 3)

	if _, err := m.GetLastCommit(context.Background()); err == nil {
		t.Fatal("GetLastCommit must fail outside a repository")
	}
}

func TestStatusReportsDirtyTree(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)
	ctx := context.Background()

	os.WriteFile(filepath.Join(tmpDir, "untracked.txt"), []byte("x"), 0o644)

	status, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status.IsClean {
		t.Error("tree with an untracked file must not report clean")
	}
	if len(status.UntrackedFiles) != 1 {
		t.Errorf("UntrackedFiles: got %v, want one entry", status.UntrackedFiles)
	}
}

// Commit subjects legitimately contain the separator used by the log format.
func TestCommitMessageContainingSeparator(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)
	ctx := context.Background()

	os.WriteFile(filepath.Join(tmpDir, "f.txt"), []byte("x"), 0o644)
	m.Stage(ctx, []string{"f.txt"})

	subject := "fix: handle a|b pipe case"
	if _, err := m.Commit(ctx, subject); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	commit, err := m.GetLastCommit(ctx)
	if err != nil {
		t.Fatalf("GetLastCommit failed: %v", err)
	}
	if commit.Message != subject {
		t.Errorf("Message: got %q, want %q", commit.Message, subject)
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"   \n  ", 0},
		{"one", 1},
		{"one\ntwo", 2},
		{"one\ntwo\n", 2},
	}

	for _, tt := range tests {
		if got := splitLines(tt.in); len(got) != tt.want {
			t.Errorf("splitLines(%q) = %v, want %d entries", tt.in, got, tt.want)
		}
	}
}

func TestParseCommitLineRejectsMalformed(t *testing.T) {
	for _, line := range []string{"", "only-a-hash", "a|b", "a|b|c"} {
		if _, ok := parseCommitLine(line); ok {
			t.Errorf("parseCommitLine(%q) should have been rejected", line)
		}
	}
}
