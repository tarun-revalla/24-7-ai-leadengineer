package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

func setupTestRepo(t *testing.T) string {
	tmpDir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	cmd = exec.Command("git", "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "Initial commit")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to create initial commit: %v", err)
	}

	return tmpDir
}

func TestManagerNew(t *testing.T) {
	m := New(".", "Test User", "test@example.com", true, 3)
	if m == nil {
		t.Fatal("New returned nil")
	}

	if m.committerName != "Test User" {
		t.Errorf("committerName: got %s, want 'Test User'", m.committerName)
	}
}

func TestManagerStage(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	if err := m.Stage(context.Background(), []string{"test.txt"}); err != nil {
		t.Fatalf("Stage failed: %v", err)
	}

	status, _ := m.Status(context.Background())
	if len(status.StagedChanges) == 0 || status.StagedChanges[0] != "test.txt" {
		t.Error("File not staged correctly")
	}
}

func TestManagerStageEmpty(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	err := m.Stage(context.Background(), []string{})
	if err == nil {
		t.Fatal("Stage should fail with empty paths")
	}
}

func TestManagerCommit(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	m.Stage(context.Background(), []string{"test.txt"})

	hash, err := m.Commit(context.Background(), "Test commit")
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	if hash == "" {
		t.Error("Commit hash is empty")
	}
}

func TestManagerCommitEmpty(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	_, err := m.Commit(context.Background(), "")
	if err == nil {
		t.Fatal("Commit should fail with empty message")
	}
}

func TestManagerStatus(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	status, err := m.Status(context.Background())
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if status == nil {
		t.Fatal("Status returned nil")
	}

	if status.IsClean != true {
		t.Error("Status should be clean initially")
	}
}

func TestManagerStatusWithChanges(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	testFile := filepath.Join(tmpDir, "new.txt")
	os.WriteFile(testFile, []byte("new content"), 0644)

	status, _ := m.Status(context.Background())
	if status.IsClean != false {
		t.Error("Status should not be clean with untracked files")
	}

	if len(status.UntrackedFiles) == 0 {
		t.Error("Should have untracked files")
	}
}

func TestManagerGetLastCommit(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	commit, err := m.GetLastCommit(context.Background())
	if err != nil {
		t.Fatalf("GetLastCommit failed: %v", err)
	}

	if commit == nil {
		t.Fatal("Commit is nil")
	}

	if commit.Hash == "" {
		t.Error("Commit hash is empty")
	}

	if commit.Author == "" {
		t.Error("Commit author is empty")
	}
}

func TestManagerGetCommitHistory(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	commits, err := m.GetCommitHistory(context.Background(), 5)
	if err != nil {
		t.Fatalf("GetCommitHistory failed: %v", err)
	}

	if len(commits) == 0 {
		t.Fatal("No commits in history")
	}

	if commits[0].Hash == "" {
		t.Error("First commit has no hash")
	}
}

func TestManagerHasConflicts(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	hasConflicts, err := m.HasConflicts(context.Background())
	if err != nil {
		t.Fatalf("HasConflicts failed: %v", err)
	}

	if hasConflicts != false {
		t.Error("Should not have conflicts initially")
	}
}

func TestManagerGetConflictedFiles(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	files, err := m.GetConflictedFiles(context.Background())
	if err != nil {
		t.Fatalf("GetConflictedFiles failed: %v", err)
	}

	if len(files) > 0 {
		t.Error("Should not have conflicted files initially")
	}
}

func TestManagerResolveConflictManual(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	err := m.ResolveConflict(context.Background(), interfaces.ConflictStrategyManual)
	if err == nil {
		t.Fatal("Manual strategy should return error")
	}
}

func TestManagerExtractCommitHash(t *testing.T) {
	m := New(".", "Test", "test@example.com", true, 3)

	output := "[abc123def456] Test commit message\n 1 file changed\n"
	hash := m.extractCommitHash(output)

	if hash != "abc123def456" {
		t.Errorf("extractCommitHash: got %s, want abc123def456", hash)
	}
}

func TestManagerConcurrency(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	done := make(chan bool)

	for i := 0; i < 5; i++ {
		go func() {
			_, _ = m.Status(context.Background())
			_, _ = m.GetLastCommit(context.Background())
			done <- true
		}()
	}

	for i := 0; i < 5; i++ {
		<-done
	}
}

func TestManagerResolveConflictInvalid(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	err := m.ResolveConflict(context.Background(), interfaces.ConflictStrategy("invalid"))
	if err == nil {
		t.Fatal("Should fail with invalid strategy")
	}
}

func TestManagerExecGit(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	output, err := m.execGit(context.Background(), "log", "-1", "--format=%s")
	if err != nil {
		t.Fatalf("execGit failed: %v", err)
	}

	if output == "" {
		t.Error("execGit returned empty output")
	}
}

func TestManagerCheckConflicts(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	hasConflicts, err := m.checkConflicts(context.Background())
	if err != nil {
		t.Fatalf("checkConflicts failed: %v", err)
	}

	if hasConflicts != false {
		t.Error("Should not have conflicts")
	}
}

func TestManagerExtractCommitHashEmpty(t *testing.T) {
	m := New(".", "Test", "test@example.com", true, 3)

	output := "No commit hash here\n"
	hash := m.extractCommitHash(output)

	if hash != "" {
		t.Errorf("extractCommitHash should return empty for invalid input, got %s", hash)
	}
}

func TestManagerGetCommitHistoryDefault(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	commits, err := m.GetCommitHistory(context.Background(), 0)
	if err != nil {
		t.Fatalf("GetCommitHistory failed: %v", err)
	}

	if len(commits) == 0 {
		t.Fatal("Should have commits with default limit")
	}
}

func TestManagerStatusAllFields(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	status, err := m.Status(context.Background())
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if status.Branch == "" {
		t.Error("Branch should not be empty")
	}

	if status.IsClean != true {
		t.Error("Should be clean after init")
	}

	if status.HasConflicts != false {
		t.Error("Should not have conflicts")
	}
}

func TestManagerPullNoRemote(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	err := m.Pull(context.Background(), "main")
	if err == nil {
		t.Fatal("Pull should fail without remote")
	}
}

func TestManagerPullDefaultBranch(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	err := m.Pull(context.Background(), "")
	if err == nil {
		t.Fatal("Pull should fail without remote")
	}
}

func TestManagerCommitWithSignature(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Custom Author", "custom@example.com", true, 3)

	testFile := filepath.Join(tmpDir, "file.txt")
	os.WriteFile(testFile, []byte("content"), 0644)

	m.Stage(context.Background(), []string{"file.txt"})
	hash, err := m.Commit(context.Background(), "Commit with custom signature")

	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	if hash == "" {
		t.Error("Commit hash should not be empty")
	}

	commit, _ := m.GetLastCommit(context.Background())
	if commit.Author != "Custom Author" {
		t.Errorf("Author: got %s, want Custom Author", commit.Author)
	}
}

func TestManagerMultipleCommits(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	for i := 0; i < 3; i++ {
		f := filepath.Join(tmpDir, fmt.Sprintf("file%d.txt", i))
		os.WriteFile(f, []byte(fmt.Sprintf("content %d", i)), 0644)
		m.Stage(context.Background(), []string{fmt.Sprintf("file%d.txt", i)})
		m.Commit(context.Background(), fmt.Sprintf("Commit %d", i))
	}

	commits, _ := m.GetCommitHistory(context.Background(), 10)
	if len(commits) < 3 {
		t.Errorf("Should have at least 3 commits, got %d", len(commits))
	}
}

func TestDiffIncludesTrackedModifications(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	path := filepath.Join(tmpDir, "tracked.txt")
	os.WriteFile(path, []byte("original\n"), 0644)
	m.Stage(context.Background(), []string{"tracked.txt"})
	m.Commit(context.Background(), "add tracked file")

	os.WriteFile(path, []byte("modified\n"), 0644)

	diff, err := m.Diff(context.Background())
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}

	if !strings.Contains(diff, "tracked.txt") {
		t.Errorf("diff should name the changed file:\n%s", diff)
	}
	if !strings.Contains(diff, "+modified") {
		t.Errorf("diff should show the new content:\n%s", diff)
	}
}

// An untracked file is the most common shape of a new task's work, so a diff
// that omitted it would hide most of what a reviewer needs to see.
func TestDiffIncludesUntrackedFiles(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	os.WriteFile(filepath.Join(tmpDir, "brand_new.go"), []byte("package main\n"), 0644)

	diff, err := m.Diff(context.Background())
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}

	if !strings.Contains(diff, "brand_new.go") {
		t.Errorf("diff should include untracked files:\n%s", diff)
	}
	if !strings.Contains(diff, "package main") {
		t.Errorf("diff should show the untracked file's content:\n%s", diff)
	}
}

func TestDiffIgnoresGitignoredFiles(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte("build/\n"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "build"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "build", "artifact.bin"), []byte("output"), 0644)

	diff, err := m.Diff(context.Background())
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}

	if strings.Contains(diff, "artifact.bin") {
		t.Errorf("ignored files must not appear in the diff:\n%s", diff)
	}
}

func TestDiffOnCleanTreeIsEmpty(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	diff, err := m.Diff(context.Background())
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if strings.TrimSpace(diff) != "" {
		t.Errorf("a clean tree should produce an empty diff, got:\n%s", diff)
	}
}

func TestDiffTruncatesOversizedOutput(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	huge := strings.Repeat("a line of source code\n", maxDiffSize/20)
	os.WriteFile(filepath.Join(tmpDir, "huge.txt"), []byte(huge), 0644)

	diff, err := m.Diff(context.Background())
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}

	if len(diff) > maxDiffSize+64 {
		t.Errorf("diff should be bounded, got %d bytes", len(diff))
	}
	if !strings.Contains(diff, "truncated") {
		t.Error("a truncated diff must say so, or a reviewer reads a partial change as complete")
	}
}
