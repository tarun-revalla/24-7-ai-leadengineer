package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// decisionSet wraps the decision list so DECISIONS.md has a mapping at its
// root, matching every other memory document.
type decisionSet struct {
	Decisions []interfaces.Decision `yaml:"decisions"`
}

const aiDir = ".ai"

// Manager must satisfy the contract the rest of the system depends on.
var _ interfaces.Memory = (*Manager)(nil)

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

	proj := &interfaces.ProjectMetadata{}
	if err := m.readMeta("PROJECT.md", proj); err != nil {
		return nil, err
	}

	return proj, nil
}

// GetBacklog returns the task backlog.
func (m *Manager) GetBacklog(ctx context.Context) (*interfaces.Backlog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	backlog := &interfaces.Backlog{}
	if err := m.readMeta("BACKLOG.md", backlog); err != nil {
		return nil, err
	}

	return backlog, nil
}

// GetCurrent returns the current task.
func (m *Manager) GetCurrent(ctx context.Context) (*interfaces.CurrentTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	current := &interfaces.CurrentTask{}
	if err := m.readMeta("CURRENT.md", current); err != nil {
		return nil, err
	}

	return current, nil
}

// GetChangelog returns the changelog.
func (m *Manager) GetChangelog(ctx context.Context) (*interfaces.Changelog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	changelog := &interfaces.Changelog{}
	if err := m.readMeta("CHANGELOG.md", changelog); err != nil {
		return nil, err
	}

	return changelog, nil
}

// GetDecisions returns architectural decisions.
func (m *Manager) GetDecisions(ctx context.Context) ([]interfaces.Decision, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.decisions()
}

// GetToolchain returns how this project verifies itself.
//
// A project that has never been detected has no TOOLCHAIN.md, which is not an
// error: the caller decides whether to fall back to a built-in gate set or to
// run detection.
func (m *Manager) GetToolchain(ctx context.Context) (*interfaces.Toolchain, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	t := &interfaces.Toolchain{}
	if err := m.readMeta("TOOLCHAIN.md", t); err != nil {
		return nil, err
	}
	return t, nil
}

// SaveToolchain records how this project verifies itself.
func (m *Manager) SaveToolchain(ctx context.Context, t *interfaces.Toolchain) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if t == nil {
		return errors.New("toolchain cannot be nil")
	}
	return m.writeMeta("TOOLCHAIN.md", t, defaultToolchainBody)
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

	proj.UpdatedAt = time.Now().UTC()

	return m.writeMeta("PROJECT.md", proj, defaultProjectBody)
}

// SaveBacklog saves the task backlog.
func (m *Manager) SaveBacklog(ctx context.Context, backlog *interfaces.Backlog) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	backlog.UpdatedAt = time.Now().UTC()

	return m.writeMeta("BACKLOG.md", backlog, defaultBacklogBody)
}

// SaveCurrent saves the current task.
func (m *Manager) SaveCurrent(ctx context.Context, current *interfaces.CurrentTask) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	current.UpdatedAt = time.Now().UTC()

	return m.writeMeta("CURRENT.md", current, defaultCurrentBody)
}

// SaveChangelog saves the changelog.
func (m *Manager) SaveChangelog(ctx context.Context, changelog *interfaces.Changelog) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	changelog.UpdatedAt = time.Now().UTC()

	return m.writeMeta("CHANGELOG.md", changelog, defaultChangelogBody)
}

// SaveDecision saves an architectural decision.
func (m *Manager) SaveDecision(ctx context.Context, decision interfaces.Decision) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if decision.Date.IsZero() {
		decision.Date = time.Now().UTC()
	}

	existing, err := m.decisions()
	if err != nil && !errors.Is(err, ErrNoFrontMatter) && !os.IsNotExist(errors.Unwrap(err)) {
		// Appending to a document that failed to parse would discard the
		// decisions already recorded there.
		return fmt.Errorf("refusing to append to unreadable DECISIONS.md: %w", err)
	}

	existing = append(existing, decision)

	return m.writeMeta("DECISIONS.md", &decisionSet{Decisions: existing}, defaultDecisionsBody)
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

	// Existence alone is not enough: a file the system cannot parse would
	// hand Claude stale or empty context while appearing healthy.
	checks := []struct {
		file string
		into any
	}{
		{"PROJECT.md", &interfaces.ProjectMetadata{}},
		{"BACKLOG.md", &interfaces.Backlog{}},
		{"CURRENT.md", &interfaces.CurrentTask{}},
	}

	for _, c := range checks {
		if err := m.readMeta(c.file, c.into); err != nil {
			return fmt.Errorf("cannot sync with Claude: %w", err)
		}
	}

	for _, name := range []string{"ROADMAP.md", "DECISIONS.md"} {
		if _, err := m.readFile(name); err != nil {
			return fmt.Errorf("cannot sync with Claude: %w", err)
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

// decisions reads the decision set. Callers must hold at least a read lock.
func (m *Manager) decisions() ([]interfaces.Decision, error) {
	set := &decisionSet{}
	if err := m.readMeta("DECISIONS.md", set); err != nil {
		return nil, err
	}
	return set.Decisions, nil
}

// readMeta loads a document's front matter into out.
func (m *Manager) readMeta(name string, out any) error {
	raw, err := m.readFile(name)
	if err != nil {
		return err
	}

	doc, err := parseDocument(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	if err := doc.decodeInto(out); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	return nil
}

// writeMeta rewrites a document's front matter while preserving its body.
// The body is the human- and Claude-facing documentation; a state update must
// never discard it. When the document does not yet exist, defaultBody seeds it.
func (m *Manager) writeMeta(name string, meta any, defaultBody string) error {
	body := defaultBody

	if raw, err := m.readFile(name); err == nil {
		// Parse errors are deliberately tolerated here: an unparseable header
		// still yields the body, which is what must be carried forward.
		if doc, _ := parseDocument(raw); doc != nil && strings.TrimSpace(doc.body) != "" {
			body = doc.body
		}
	}

	rendered, err := renderDocument(meta, body)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	return m.writeFile(name, rendered)
}
