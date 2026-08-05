package executor

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/gates"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/review"
	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// fakeClaude records prompts and returns scripted results.
type fakeClaude struct {
	mu      sync.Mutex
	prompts []string
	results []claudeReply
	calls   int
	// onCall runs before each reply, letting a test mutate the world the way a
	// real implementation would (fixing gates, dirtying the tree).
	onCall func(n int)
}

type claudeReply struct {
	result *interfaces.SessionResult
	err    error
}

func errorReply(msg string) claudeReply {
	return claudeReply{result: &interfaces.SessionResult{Errors: []string{msg}}}
}

func failReply(msg string) claudeReply {
	return claudeReply{err: errors.New(msg)}
}

func (f *fakeClaude) LaunchSession(ctx context.Context, prompt string) (*interfaces.SessionResult, error) {
	f.mu.Lock()
	n := f.calls
	f.calls++
	f.prompts = append(f.prompts, prompt)
	onCall := f.onCall
	f.mu.Unlock()

	if onCall != nil {
		onCall(n)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.results) == 0 {
		return &interfaces.SessionResult{Output: "done"}, nil
	}
	idx := n
	if idx >= len(f.results) {
		idx = len(f.results) - 1
	}
	r := f.results[idx]
	return r.result, r.err
}

func (f *fakeClaude) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeClaude) promptAt(i int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.prompts) {
		return ""
	}
	return f.prompts[i]
}

// fakeGit models a repository's cleanliness and commits.
type fakeGit struct {
	mu         sync.Mutex
	clean      bool
	conflict   bool
	staged     int
	commits    []string
	hash       string
	dirtyPaths []string
	statusErr  error
	commitErr  error
}

func newFakeGit() *fakeGit {
	return &fakeGit{clean: true, hash: "abc123def4567890"}
}

func (g *fakeGit) Status(ctx context.Context) (*interfaces.GitStatus, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.statusErr != nil {
		return nil, g.statusErr
	}

	st := &interfaces.GitStatus{
		Branch:       "main",
		IsClean:      g.clean,
		HasConflicts: g.conflict,
	}
	if !g.clean {
		st.UnstagedChanges = append([]string(nil), g.dirtyPaths...)
		if len(st.UnstagedChanges) == 0 {
			st.UnstagedChanges = []string{"main.go"}
		}
	}
	return st, nil
}

func (g *fakeGit) Stage(ctx context.Context, paths []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.staged++
	return nil
}

func (g *fakeGit) Commit(ctx context.Context, message string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.commitErr != nil {
		return "", g.commitErr
	}

	g.commits = append(g.commits, message)
	g.clean = true
	return g.hash[:7], nil
}

func (g *fakeGit) Diff(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.clean {
		return "", nil
	}
	return "--- a/main.go\n+++ b/main.go\n+the change\n", nil
}

func (g *fakeGit) GetLastCommit(ctx context.Context) (*interfaces.GitCommit, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.commits) == 0 {
		return nil, errors.New("no commits")
	}
	return &interfaces.GitCommit{Hash: g.hash, Message: g.commits[len(g.commits)-1]}, nil
}

func (g *fakeGit) setDirty() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.clean = false
}

func (g *fakeGit) commitCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.commits)
}

func (g *fakeGit) lastMessage() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.commits) == 0 {
		return ""
	}
	return g.commits[len(g.commits)-1]
}

// fakeMemory holds project state in memory.
type fakeMemory struct {
	mu        sync.Mutex
	backlog   *interfaces.Backlog
	current   *interfaces.CurrentTask
	changelog *interfaces.Changelog
	project   *interfaces.ProjectMetadata
}

func newFakeMemory(tasks ...interfaces.BacklogTask) *fakeMemory {
	return &fakeMemory{
		backlog:   &interfaces.Backlog{Tasks: tasks},
		current:   &interfaces.CurrentTask{},
		changelog: &interfaces.Changelog{},
		project:   &interfaces.ProjectMetadata{Name: "Demo", Purpose: "Testing"},
	}
}

func (m *fakeMemory) GetBacklog(ctx context.Context) (*interfaces.Backlog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	clone := *m.backlog
	clone.Tasks = append([]interfaces.BacklogTask(nil), m.backlog.Tasks...)
	return &clone, nil
}

func (m *fakeMemory) SaveBacklog(ctx context.Context, b *interfaces.Backlog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backlog = b
	return nil
}

func (m *fakeMemory) GetCurrent(ctx context.Context) (*interfaces.CurrentTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	clone := *m.current
	return &clone, nil
}

func (m *fakeMemory) SaveCurrent(ctx context.Context, c *interfaces.CurrentTask) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = c
	return nil
}

func (m *fakeMemory) GetChangelog(ctx context.Context) (*interfaces.Changelog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	clone := *m.changelog
	clone.Entries = append([]interfaces.ChangelogEntry(nil), m.changelog.Entries...)
	return &clone, nil
}

func (m *fakeMemory) SaveChangelog(ctx context.Context, c *interfaces.Changelog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.changelog = c
	return nil
}

func (m *fakeMemory) GetProject(ctx context.Context) (*interfaces.ProjectMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.project, nil
}

func (m *fakeMemory) taskStatus(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.backlog.Tasks {
		if t.ID == id {
			return t.Status
		}
	}
	return ""
}

func (m *fakeMemory) currentTask() interfaces.CurrentTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *m.current
}

func (m *fakeMemory) changelogEntries() []interfaces.ChangelogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]interfaces.ChangelogEntry(nil), m.changelog.Entries...)
}

// fakeCheckpoints records checkpoint calls.
type fakeCheckpoints struct {
	mu     sync.Mutex
	states []interfaces.CheckpointState
	err    error
}

func (c *fakeCheckpoints) CreateCheckpoint(ctx context.Context, s *interfaces.CheckpointState) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.err != nil {
		return "", c.err
	}

	c.states = append(c.states, *s)
	return "ckpt-" + s.TaskState, nil
}

func (c *fakeCheckpoints) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.states)
}

func (c *fakeCheckpoints) stages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.states))
	for _, s := range c.states {
		out = append(out, s.TaskState)
	}
	return out
}

// scriptedGate returns a fixed outcome, optionally changing after n runs so a
// test can model a repair succeeding.
type scriptedGate struct {
	mu        sync.Mutex
	name      string
	failUntil int
	runs      int
}

func (g *scriptedGate) Name() string   { return g.name }
func (g *scriptedGate) Required() bool { return true }

func (g *scriptedGate) Run(ctx context.Context, dir string) gates.Result {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.runs++
	if g.runs <= g.failUntil {
		return gates.Result{
			Gate:   g.name,
			Status: gates.StatusFailed,
			Detail: "scripted failure",
			Output: "expected 1, got 2",
		}
	}
	return gates.Result{Gate: g.name, Status: gates.StatusPassed}
}

func (g *scriptedGate) runCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.runs
}

// fakeReviewer returns scripted verdicts, optionally approving after a set
// number of revisions so a test can model a revision succeeding.
type fakeReviewer struct {
	mu           sync.Mutex
	calls        int
	diffs        []string
	rejectUntil  int
	blocking     []review.Finding
	err          error
	unparseable  bool
	lastApproved bool
}

func (r *fakeReviewer) Review(ctx context.Context, change review.Change) (*review.Review, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls++
	r.diffs = append(r.diffs, change.Diff)

	if r.err != nil {
		return nil, r.err
	}
	if r.unparseable {
		return nil, review.ErrUnparseable
	}

	if r.calls <= r.rejectUntil {
		findings := r.blocking
		if len(findings) == 0 {
			findings = []review.Finding{{
				Perspective: review.PerspectiveReviewer,
				Severity:    review.SeverityMajor,
				Description: "the error path is unhandled",
			}}
		}
		return &review.Review{Approved: false, Findings: findings}, nil
	}

	r.lastApproved = true
	return &review.Review{Approved: true, Summary: "looks right"}, nil
}

func (r *fakeReviewer) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *fakeReviewer) diffAt(i int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= len(r.diffs) {
		return ""
	}
	return r.diffs[i]
}

// discardLogger swallows log output.
type discardLogger struct{}

func (discardLogger) Debug(string, ...interface{}) {}
func (discardLogger) Info(string, ...interface{})  {}
func (discardLogger) Warn(string, ...interface{})  {}
func (discardLogger) Error(string, ...interface{}) {}

func sampleTask(id string, priority int) interfaces.BacklogTask {
	return interfaces.BacklogTask{
		ID:          id,
		Title:       "Do the thing for " + id,
		Description: "A description of " + id,
		Priority:    priority,
		Status:      "new",
		CreatedAt:   time.Now().UTC(),
	}
}
