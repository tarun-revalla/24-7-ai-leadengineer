package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
	"gopkg.in/yaml.v3"
)

const aiDir = ".ai"

// Manager implements the Memory interface for persistent project state.
type Manager struct {
	projectPath string
	mu          sync.RWMutex
}

// New creates a new memory manager.
func New(projectPath string) (*Manager, error) {
	if projectPath == "" {
		projectPath = "."
	}

	aiPath := filepath.Join(projectPath, aiDir)
	if err := os.MkdirAll(aiPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create .ai directory: %w", err)
	}

	return &Manager{
		projectPath: projectPath,
	}, nil
}

// GetProject returns project metadata.
func (m *Manager) GetProject(ctx context.Context) (*interfaces.ProjectMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.readFile("PROJECT.md")
	if err != nil {
		return nil, err
	}

	proj := &interfaces.ProjectMetadata{}
	if err := yaml.Unmarshal([]byte(data), proj); err != nil {
		return nil, fmt.Errorf("failed to parse PROJECT.md: %w", err)
	}

	return proj, nil
}

// GetBacklog returns the task backlog.
func (m *Manager) GetBacklog(ctx context.Context) (*interfaces.Backlog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.readFile("BACKLOG.md")
	if err != nil {
		return nil, err
	}

	backlog := &interfaces.Backlog{}
	if err := yaml.Unmarshal([]byte(data), backlog); err != nil {
		return nil, fmt.Errorf("failed to parse BACKLOG.md: %w", err)
	}

	return backlog, nil
}

// GetCurrent returns the current task.
func (m *Manager) GetCurrent(ctx context.Context) (*interfaces.CurrentTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.readFile("CURRENT.md")
	if err != nil {
		return nil, err
	}

	current := &interfaces.CurrentTask{}
	if err := yaml.Unmarshal([]byte(data), current); err != nil {
		return nil, fmt.Errorf("failed to parse CURRENT.md: %w", err)
	}

	return current, nil
}

// GetChangelog returns the changelog.
func (m *Manager) GetChangelog(ctx context.Context) (*interfaces.Changelog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.readFile("CHANGELOG.md")
	if err != nil {
		return nil, err
	}

	changelog := &interfaces.Changelog{}
	if err := yaml.Unmarshal([]byte(data), changelog); err != nil {
		return nil, fmt.Errorf("failed to parse CHANGELOG.md: %w", err)
	}

	return changelog, nil
}

// GetDecisions returns architectural decisions.
func (m *Manager) GetDecisions(ctx context.Context) ([]interfaces.Decision, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.readFile("DECISIONS.md")
	if err != nil {
		return nil, err
	}

	var decisions []interfaces.Decision
	if err := yaml.Unmarshal([]byte(data), &decisions); err != nil {
		return nil, fmt.Errorf("failed to parse DECISIONS.md: %w", err)
	}

	return decisions, nil
}

// ReadFile reads an arbitrary file from the .ai directory.
func (m *Manager) ReadFile(ctx context.Context, name string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.readFile(name)
}

// SaveProject saves project metadata.
func (m *Manager) SaveProject(ctx context.Context, proj *interfaces.ProjectMetadata) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	proj.UpdatedAt = time.Now()

	data, err := yaml.Marshal(proj)
	if err != nil {
		return fmt.Errorf("failed to marshal project: %w", err)
	}

	return m.writeFile("PROJECT.md", string(data))
}

// SaveBacklog saves the task backlog.
func (m *Manager) SaveBacklog(ctx context.Context, backlog *interfaces.Backlog) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	backlog.UpdatedAt = time.Now()

	data, err := yaml.Marshal(backlog)
	if err != nil {
		return fmt.Errorf("failed to marshal backlog: %w", err)
	}

	return m.writeFile("BACKLOG.md", string(data))
}

// SaveCurrent saves the current task.
func (m *Manager) SaveCurrent(ctx context.Context, current *interfaces.CurrentTask) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	current.UpdatedAt = time.Now()

	data, err := yaml.Marshal(current)
	if err != nil {
		return fmt.Errorf("failed to marshal current: %w", err)
	}

	return m.writeFile("CURRENT.md", string(data))
}

// SaveChangelog saves the changelog.
func (m *Manager) SaveChangelog(ctx context.Context, changelog *interfaces.Changelog) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	changelog.UpdatedAt = time.Now()

	data, err := yaml.Marshal(changelog)
	if err != nil {
		return fmt.Errorf("failed to marshal changelog: %w", err)
	}

	return m.writeFile("CHANGELOG.md", string(data))
}

// SaveDecision saves an architectural decision.
func (m *Manager) SaveDecision(ctx context.Context, decision interfaces.Decision) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	decision.Date = time.Now()

	decisions, _ := m.getDecisions()
	decisions = append(decisions, decision)

	data, err := yaml.Marshal(decisions)
	if err != nil {
		return fmt.Errorf("failed to marshal decisions: %w", err)
	}

	return m.writeFile("DECISIONS.md", string(data))
}

// WriteFile writes an arbitrary file to the .ai directory.
func (m *Manager) WriteFile(ctx context.Context, name string, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeFile(name, content)
}

// SyncWithClaude prepares memory for Claude context.
func (m *Manager) SyncWithClaude(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	files := []string{"PROJECT.md", "ROADMAP.md", "BACKLOG.md", "CURRENT.md", "DECISIONS.md"}

	for _, file := range files {
		if _, err := m.readFile(file); err != nil {
			return fmt.Errorf("cannot sync with Claude: missing %s", file)
		}
	}

	return nil
}

// readFile reads a file from the .ai directory.
func (m *Manager) readFile(name string) (string, error) {
	path := filepath.Join(m.projectPath, aiDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", name, err)
	}
	return string(data), nil
}

// writeFile writes a file to the .ai directory.
func (m *Manager) writeFile(name string, content string) error {
	aiPath := filepath.Join(m.projectPath, aiDir)
	if err := os.MkdirAll(aiPath, 0755); err != nil {
		return fmt.Errorf("failed to create .ai directory: %w", err)
	}

	path := filepath.Join(aiPath, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", name, err)
	}
	return nil
}

// getDecisions is an internal helper that doesn't lock.
func (m *Manager) getDecisions() ([]interfaces.Decision, error) {
	data, err := m.readFile("DECISIONS.md")
	if err != nil {
		return nil, err
	}

	var decisions []interfaces.Decision
	if err := yaml.Unmarshal([]byte(data), &decisions); err != nil {
		return nil, err
	}
	return decisions, nil
}
