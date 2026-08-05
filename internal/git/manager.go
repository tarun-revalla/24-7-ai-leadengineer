package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// Manager must satisfy the contract the rest of the system depends on.
var _ interfaces.GitManager = (*Manager)(nil)

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
func (m *Manager) Stage(ctx context.Context, paths []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(paths) == 0 {
		return fmt.Errorf("no paths to stage")
	}

	args := append([]string{"add"}, paths...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = m.projectPath

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stage files: %w", err)
	}

	return nil
}

// Commit commits staged changes.
func (m *Manager) Commit(ctx context.Context, message string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if message == "" {
		return "", fmt.Errorf("commit message cannot be empty")
	}

	cmd := exec.CommandContext(ctx, "git", "-c", fmt.Sprintf("user.name=%s", m.committerName),
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
func (m *Manager) Push(ctx context.Context, branch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if branch == "" {
		branch = "main"
	}

	cmd := exec.CommandContext(ctx, "git", "push", "origin", branch)
	cmd.Dir = m.projectPath

	if err := cmd.Run(); err != nil {
		if !m.autoRetry {
			return fmt.Errorf("failed to push: %w", err)
		}

		return m.retryPush(ctx, branch)
	}

	return nil
}

// Pull pulls latest changes.
func (m *Manager) Pull(ctx context.Context, branch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if branch == "" {
		branch = "main"
	}

	cmd := exec.CommandContext(ctx, "git", "pull", "origin", branch)
	cmd.Dir = m.projectPath

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull: %w", err)
	}

	return nil
}

// Status returns current git status.
func (m *Manager) Status(ctx context.Context) (*interfaces.GitStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Every command below is checked. A discarded error here would be reported
	// as an empty result, which reads as "clean tree, no changes" — the system
	// would then believe there is nothing to commit when git in fact failed.
	status := &interfaces.GitStatus{}

	branch, err := m.currentBranch(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to determine current branch: %w", err)
	}
	status.Branch = branch

	porcelain, err := m.execGit(ctx, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("failed to read repository status: %w", err)
	}
	status.IsClean = strings.TrimSpace(porcelain) == ""

	staged, err := m.execGit(ctx, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, fmt.Errorf("failed to list staged changes: %w", err)
	}
	status.StagedChanges = splitLines(staged)

	unstaged, err := m.execGit(ctx, "diff", "--name-only")
	if err != nil {
		return nil, fmt.Errorf("failed to list unstaged changes: %w", err)
	}
	status.UnstagedChanges = splitLines(unstaged)

	untracked, err := m.execGit(ctx, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("failed to list untracked files: %w", err)
	}
	status.UntrackedFiles = splitLines(untracked)

	status.HasConflicts, err = m.checkConflicts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check for conflicts: %w", err)
	}

	return status, nil
}

// splitLines turns command output into a slice, returning nil rather than a
// one-element slice containing "" when the output is empty.
func splitLines(out string) []string {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// GetLastCommit returns the latest commit.
func (m *Manager) GetLastCommit(ctx context.Context) (*interfaces.GitCommit, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// A single log call keeps the fields consistent with one another; four
	// separate calls could straddle a concurrent commit and mix two commits.
	out, err := m.execGit(ctx, "log", "-1", "--format=%H|%an|%s|%aI", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("failed to read last commit: %w", err)
	}

	commit, ok := parseCommitLine(strings.TrimSpace(out))
	if !ok {
		return nil, fmt.Errorf("unexpected git log output: %q", strings.TrimSpace(out))
	}

	return &commit, nil
}

// parseCommitLine parses one hash|author|subject|date record. The subject may
// itself contain the separator, so the split is bounded and the subject is
// reassembled from the middle fields.
func parseCommitLine(line string) (interfaces.GitCommit, bool) {
	if line == "" {
		return interfaces.GitCommit{}, false
	}

	parts := strings.Split(line, "|")
	if len(parts) < 4 {
		return interfaces.GitCommit{}, false
	}

	t, _ := time.Parse(time.RFC3339, parts[len(parts)-1])

	return interfaces.GitCommit{
		Hash:      parts[0],
		Author:    parts[1],
		Message:   strings.Join(parts[2:len(parts)-1], "|"),
		Timestamp: t,
	}, true
}

// GetCommitHistory returns recent commits.
func (m *Manager) GetCommitHistory(ctx context.Context, limit int) ([]interfaces.GitCommit, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	output, err := m.execGit(ctx, "log", fmt.Sprintf("--max-count=%d", limit), "--format=%H|%an|%s|%aI")
	if err != nil {
		return nil, err
	}

	lines := splitLines(output)
	commits := make([]interfaces.GitCommit, 0, len(lines))

	for _, line := range lines {
		if commit, ok := parseCommitLine(line); ok {
			commits = append(commits, commit)
		}
	}

	return commits, nil
}

// HasConflicts checks if there are merge conflicts.
func (m *Manager) HasConflicts(ctx context.Context) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.checkConflicts(ctx)
}

// ResolveConflict resolves conflicts using specified strategy.
func (m *Manager) ResolveConflict(ctx context.Context, strategy interfaces.ConflictStrategy) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch strategy {
	case interfaces.ConflictStrategyOurs:
		cmd := exec.CommandContext(ctx, "git", "checkout", "--ours", ".")
		cmd.Dir = m.projectPath
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to resolve conflict (ours): %w", err)
		}

	case interfaces.ConflictStrategyTheirs:
		cmd := exec.CommandContext(ctx, "git", "checkout", "--theirs", ".")
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
func (m *Manager) GetConflictedFiles(ctx context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	output, err := m.execGit(ctx, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}

	return splitLines(output), nil
}

// Helper methods

// execGit runs a git command bound to ctx, so a cancelled or expired context
// terminates the subprocess rather than leaving it running.
func (m *Manager) execGit(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = m.projectPath

	output, err := cmd.Output()
	return string(output), err
}

// currentBranch resolves the checked-out branch name.
//
// "rev-parse --abbrev-ref HEAD" alone fails on a repository with no commits
// yet — HEAD points at an unborn branch, which is exactly the state a project
// is in right after `git init`, before its first commit. "symbolic-ref" gives
// the branch name in that case, so it is tried first; it fails in turn on a
// detached HEAD, where rev-parse's answer ("HEAD") is what's wanted instead.
// Only a repository broken in some other way fails both.
func (m *Manager) currentBranch(ctx context.Context) (string, error) {
	if out, err := m.execGit(ctx, "symbolic-ref", "--short", "HEAD"); err == nil {
		return strings.TrimSpace(out), nil
	}

	out, err := m.execGit(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// retryPush retries a failed push with exponential backoff, abandoning the
// attempt as soon as ctx is cancelled rather than sleeping through it.
func (m *Manager) retryPush(ctx context.Context, branch string) error {
	for attempt := 1; attempt <= m.retryAttempts; attempt++ {
		backoff := time.Duration(m.retryBackoffMs) * time.Millisecond << (attempt - 1)

		select {
		case <-ctx.Done():
			return fmt.Errorf("push retry abandoned: %w", ctx.Err())
		case <-time.After(backoff):
		}

		cmd := exec.CommandContext(ctx, "git", "push", "origin", branch)
		cmd.Dir = m.projectPath

		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	return fmt.Errorf("push failed after %d retries", m.retryAttempts)
}

func (m *Manager) checkConflicts(ctx context.Context) (bool, error) {
	output, err := m.execGit(ctx, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return false, err
	}

	return strings.TrimSpace(output) != "", nil
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
