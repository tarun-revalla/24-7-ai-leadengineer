package git

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// Manager manages git operations.
type Manager struct {
	projectPath    string
	committerName  string
	committerEmail string
	autoRetry      bool
	retryAttempts  int
	retryBackoffMs int
	mu             sync.RWMutex
}

// New creates a new git manager.
func New(projectPath, committerName, committerEmail string, autoRetry bool, retryAttempts int) *Manager {
	return &Manager{
		projectPath:    projectPath,
		committerName:  committerName,
		committerEmail: committerEmail,
		autoRetry:      autoRetry,
		retryAttempts:  retryAttempts,
		retryBackoffMs: 1000,
	}
}

// Stage stages files for commit.
func (m *Manager) Stage(ctx interface{}, paths []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(paths) == 0 {
		return fmt.Errorf("no paths to stage")
	}

	args := append([]string{"add"}, paths...)
	cmd := exec.Command("git", args...)
	cmd.Dir = m.projectPath

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stage files: %w", err)
	}

	return nil
}

// Commit commits staged changes.
func (m *Manager) Commit(ctx interface{}, message string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if message == "" {
		return "", fmt.Errorf("commit message cannot be empty")
	}

	cmd := exec.Command("git", "-c", fmt.Sprintf("user.name=%s", m.committerName),
		"-c", fmt.Sprintf("user.email=%s", m.committerEmail),
		"commit", "-m", message)
	cmd.Dir = m.projectPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to commit: %w", err)
	}

	hash := m.extractCommitHash(string(output))
	return hash, nil
}

// Push pushes commits to remote.
func (m *Manager) Push(ctx interface{}, branch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if branch == "" {
		branch = "main"
	}

	cmd := exec.Command("git", "push", "origin", branch)
	cmd.Dir = m.projectPath

	if err := cmd.Run(); err != nil {
		if !m.autoRetry {
			return fmt.Errorf("failed to push: %w", err)
		}

		return m.retryPush(branch)
	}

	return nil
}

// Pull pulls latest changes.
func (m *Manager) Pull(ctx interface{}, branch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if branch == "" {
		branch = "main"
	}

	cmd := exec.Command("git", "pull", "origin", branch)
	cmd.Dir = m.projectPath

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull: %w", err)
	}

	return nil
}

// Status returns current git status.
func (m *Manager) Status(ctx interface{}) (*interfaces.GitStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := &interfaces.GitStatus{}

	// Get current branch
	branch, _ := m.execGit("rev-parse", "--abbrev-ref", "HEAD")
	status.Branch = strings.TrimSpace(branch)

	// Check if clean
	clean, _ := m.execGit("status", "--porcelain")
	status.IsClean = clean == ""

	// Get staged changes
	staged, _ := m.execGit("diff", "--cached", "--name-only")
	status.StagedChanges = strings.Split(strings.TrimSpace(staged), "\n")
	if status.StagedChanges[0] == "" {
		status.StagedChanges = nil
	}

	// Get unstaged changes
	unstaged, _ := m.execGit("diff", "--name-only")
	status.UnstagedChanges = strings.Split(strings.TrimSpace(unstaged), "\n")
	if status.UnstagedChanges[0] == "" {
		status.UnstagedChanges = nil
	}

	// Get untracked files
	untracked, _ := m.execGit("ls-files", "--others", "--exclude-standard")
	status.UntrackedFiles = strings.Split(strings.TrimSpace(untracked), "\n")
	if status.UntrackedFiles[0] == "" {
		status.UntrackedFiles = nil
	}

	// Check for conflicts
	status.HasConflicts, _ = m.checkConflicts()

	return status, nil
}

// GetLastCommit returns the latest commit.
func (m *Manager) GetLastCommit(ctx interface{}) (*interfaces.GitCommit, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	hash, _ := m.execGit("rev-parse", "HEAD")
	hash = strings.TrimSpace(hash)

	author, _ := m.execGit("log", "-1", "--format=%an", "HEAD")
	author = strings.TrimSpace(author)

	message, _ := m.execGit("log", "-1", "--format=%s", "HEAD")
	message = strings.TrimSpace(message)

	timestamp, _ := m.execGit("log", "-1", "--format=%aI", "HEAD")
	timestamp = strings.TrimSpace(timestamp)

	t, _ := time.Parse(time.RFC3339, timestamp)

	return &interfaces.GitCommit{
		Hash:      hash,
		Author:    author,
		Message:   message,
		Timestamp: t,
	}, nil
}

// GetCommitHistory returns recent commits.
func (m *Manager) GetCommitHistory(ctx interface{}, limit int) ([]interfaces.GitCommit, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	output, err := m.execGit("log", fmt.Sprintf("--max-count=%d", limit), "--format=%H|%an|%s|%aI")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	commits := make([]interfaces.GitCommit, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}

		t, _ := time.Parse(time.RFC3339, parts[3])
		commits = append(commits, interfaces.GitCommit{
			Hash:      parts[0],
			Author:    parts[1],
			Message:   parts[2],
			Timestamp: t,
		})
	}

	return commits, nil
}

// HasConflicts checks if there are merge conflicts.
func (m *Manager) HasConflicts(ctx interface{}) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.checkConflicts()
}

// ResolveConflict resolves conflicts using specified strategy.
func (m *Manager) ResolveConflict(ctx interface{}, strategy interfaces.ConflictStrategy) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch strategy {
	case interfaces.ConflictStrategyOurs:
		cmd := exec.Command("git", "checkout", "--ours", ".")
		cmd.Dir = m.projectPath
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to resolve conflict (ours): %w", err)
		}

	case interfaces.ConflictStrategyTheirs:
		cmd := exec.Command("git", "checkout", "--theirs", ".")
		cmd.Dir = m.projectPath
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to resolve conflict (theirs): %w", err)
		}

	case interfaces.ConflictStrategyManual:
		return fmt.Errorf("manual conflict resolution required")

	default:
		return fmt.Errorf("unknown conflict strategy: %s", strategy)
	}

	return nil
}

// GetConflictedFiles returns list of conflicted files.
func (m *Manager) GetConflictedFiles(ctx interface{}) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	output, err := m.execGit("diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}

	files := strings.Split(strings.TrimSpace(output), "\n")
	if files[0] == "" {
		return nil, nil
	}

	return files, nil
}

// Helper methods

func (m *Manager) execGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = m.projectPath

	output, err := cmd.Output()
	return string(output), err
}

func (m *Manager) retryPush(branch string) error {
	for attempt := 1; attempt <= m.retryAttempts; attempt++ {
		time.Sleep(time.Duration(m.retryBackoffMs*attempt) * time.Millisecond)

		cmd := exec.Command("git", "push", "origin", branch)
		cmd.Dir = m.projectPath

		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	return fmt.Errorf("push failed after %d retries", m.retryAttempts)
}

func (m *Manager) checkConflicts() (bool, error) {
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = m.projectPath

	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	return strings.TrimSpace(string(output)) != "", nil
}

func (m *Manager) extractCommitHash(output string) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "[") && strings.Contains(line, "]") {
			start := strings.Index(line, "[") + 1
			end := strings.Index(line, "]")
			if start > 0 && end > start {
				return line[start:end]
			}
		}
	}
	return ""
}
