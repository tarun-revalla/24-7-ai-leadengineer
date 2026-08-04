package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	if err := m.Stage(nil, []string{"test.txt"}); err != nil {
		t.Fatalf("Stage failed: %v", err)
	}

	status, _ := m.Status(nil)
	if len(status.StagedChanges) == 0 || status.StagedChanges[0] != "test.txt" {
		t.Error("File not staged correctly")
	}
}

func TestManagerStageEmpty(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	err := m.Stage(nil, []string{})
	if err == nil {
		t.Fatal("Stage should fail with empty paths")
	}
}

func TestManagerCommit(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	m.Stage(nil, []string{"test.txt"})

	hash, err := m.Commit(nil, "Test commit")
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

	_, err := m.Commit(nil, "")
	if err == nil {
		t.Fatal("Commit should fail with empty message")
	}
}

func TestManagerStatus(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	status, err := m.Status(nil)
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

	status, _ := m.Status(nil)
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

	commit, err := m.GetLastCommit(nil)
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

	commits, err := m.GetCommitHistory(nil, 5)
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

	hasConflicts, err := m.HasConflicts(nil)
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

	files, err := m.GetConflictedFiles(nil)
	if err != nil {
		t.Fatalf("GetConflictedFiles failed: %v", err)
	}

	if files != nil && len(files) > 0 {
		t.Error("Should not have conflicted files initially")
	}
}

func TestManagerResolveConflictManual(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	err := m.ResolveConflict(nil, interfaces.ConflictStrategyManual)
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
			_, _ = m.Status(nil)
			_, _ = m.GetLastCommit(nil)
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

	err := m.ResolveConflict(nil, interfaces.ConflictStrategy("invalid"))
	if err == nil {
		t.Fatal("Should fail with invalid strategy")
	}
}

func TestManagerExecGit(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	output, err := m.execGit("log", "-1", "--format=%s")
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

	hasConflicts, err := m.checkConflicts()
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

	commits, err := m.GetCommitHistory(nil, 0)
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

	status, err := m.Status(nil)
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

	err := m.Pull(nil, "main")
	if err == nil {
		t.Fatal("Pull should fail without remote")
	}
}

func TestManagerPullDefaultBranch(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Test User", "test@example.com", true, 3)

	err := m.Pull(nil, "")
	if err == nil {
		t.Fatal("Pull should fail without remote")
	}
}

func TestManagerCommitWithSignature(t *testing.T) {
	tmpDir := setupTestRepo(t)
	m := New(tmpDir, "Custom Author", "custom@example.com", true, 3)

	testFile := filepath.Join(tmpDir, "file.txt")
	os.WriteFile(testFile, []byte("content"), 0644)

	m.Stage(nil, []string{"file.txt"})
	hash, err := m.Commit(nil, "Commit with custom signature")

	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	if hash == "" {
		t.Error("Commit hash should not be empty")
	}

	commit, _ := m.GetLastCommit(nil)
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
		m.Stage(nil, []string{fmt.Sprintf("file%d.txt", i)})
		m.Commit(nil, fmt.Sprintf("Commit %d", i))
	}

	commits, _ := m.GetCommitHistory(nil, 10)
	if len(commits) < 3 {
		t.Errorf("Should have at least 3 commits, got %d", len(commits))
	}
}
