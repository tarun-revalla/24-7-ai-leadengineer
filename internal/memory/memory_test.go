package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

func TestNewManager(t *testing.T) {
	tmpDir := t.TempDir()

	m, err := New(tmpDir)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if m == nil {
		t.Fatal("New returned nil")
	}

	aiPath := filepath.Join(tmpDir, ".ai")
	if _, err := os.Stat(aiPath); err != nil {
		t.Fatalf(".ai directory not created: %v", err)
	}
}

func TestNewManagerDefault(t *testing.T) {
	oldWd, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	m, err := New("")
	if err != nil {
		t.Fatalf("New with empty path failed: %v", err)
	}

	if m == nil {
		t.Fatal("New returned nil")
	}
}

func TestManagerSaveAndGetProject(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir)
	ctx := context.Background()

	proj := &interfaces.ProjectMetadata{
		Name:        "TestProject",
		Description: "A test project",
		Purpose:     "Testing",
	}

	if err := m.SaveProject(ctx, proj); err != nil {
		t.Fatalf("SaveProject failed: %v", err)
	}

	retrieved, err := m.GetProject(ctx)
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}

	if retrieved.Name != "TestProject" {
		t.Errorf("Name: got %s, want TestProject", retrieved.Name)
	}
	if retrieved.Description != "A test project" {
		t.Errorf("Description: got %s, want 'A test project'", retrieved.Description)
	}
}

func TestManagerSaveAndGetBacklog(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir)
	ctx := context.Background()

	backlog := &interfaces.Backlog{
		Tasks: []interfaces.BacklogTask{
			{
				ID:       "task-1",
				Title:    "First task",
				Priority: 1,
			},
		},
	}

	if err := m.SaveBacklog(ctx, backlog); err != nil {
		t.Fatalf("SaveBacklog failed: %v", err)
	}

	retrieved, err := m.GetBacklog(ctx)
	if err != nil {
		t.Fatalf("GetBacklog failed: %v", err)
	}

	if len(retrieved.Tasks) != 1 {
		t.Errorf("Tasks count: got %d, want 1", len(retrieved.Tasks))
	}
	if retrieved.Tasks[0].Title != "First task" {
		t.Errorf("Task title: got %s, want 'First task'", retrieved.Tasks[0].Title)
	}
}

func TestManagerSaveAndGetCurrent(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir)
	ctx := context.Background()

	current := &interfaces.CurrentTask{
		TaskID:   "task-1",
		Title:    "Current work",
		Progress: 0.5,
		Status:   "in-progress",
	}

	if err := m.SaveCurrent(ctx, current); err != nil {
		t.Fatalf("SaveCurrent failed: %v", err)
	}

	retrieved, err := m.GetCurrent(ctx)
	if err != nil {
		t.Fatalf("GetCurrent failed: %v", err)
	}

	if retrieved.TaskID != "task-1" {
		t.Errorf("TaskID: got %s, want task-1", retrieved.TaskID)
	}
	if retrieved.Progress != 0.5 {
		t.Errorf("Progress: got %f, want 0.5", retrieved.Progress)
	}
}

func TestManagerSaveAndGetChangelog(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir)
	ctx := context.Background()

	changelog := &interfaces.Changelog{
		Entries: []interfaces.ChangelogEntry{
			{
				TaskID:  "task-1",
				Title:   "Completed task",
				Commit:  "abc123",
				Summary: "Task summary",
				Date:    time.Now(),
			},
		},
	}

	if err := m.SaveChangelog(ctx, changelog); err != nil {
		t.Fatalf("SaveChangelog failed: %v", err)
	}

	retrieved, err := m.GetChangelog(ctx)
	if err != nil {
		t.Fatalf("GetChangelog failed: %v", err)
	}

	if len(retrieved.Entries) != 1 {
		t.Errorf("Entries count: got %d, want 1", len(retrieved.Entries))
	}
	if retrieved.Entries[0].Title != "Completed task" {
		t.Errorf("Entry title: got %s, want 'Completed task'", retrieved.Entries[0].Title)
	}
}

func TestManagerSaveAndGetDecision(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir)
	ctx := context.Background()

	decision := interfaces.Decision{
		ID:        "ARCH-001",
		Title:     "Use Go",
		Status:    "Accepted",
		Context:   "Language choice",
		Rationale: "Good for systems",
	}

	if err := m.SaveDecision(ctx, decision); err != nil {
		t.Fatalf("SaveDecision failed: %v", err)
	}

	decisions, err := m.GetDecisions(ctx)
	if err != nil {
		t.Fatalf("GetDecisions failed: %v", err)
	}

	if len(decisions) != 1 {
		t.Errorf("Decisions count: got %d, want 1", len(decisions))
	}
	if decisions[0].Title != "Use Go" {
		t.Errorf("Decision title: got %s, want 'Use Go'", decisions[0].Title)
	}
}

func TestManagerReadWriteFile(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir)
	ctx := context.Background()

	content := "This is test content"
	if err := m.WriteFile(ctx, "test.txt", content); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	retrieved, err := m.ReadFile(ctx, "test.txt")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if retrieved != content {
		t.Errorf("Content: got %s, want %s", retrieved, content)
	}
}

func TestManagerSyncWithClaude(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir)
	ctx := context.Background()

	proj := &interfaces.ProjectMetadata{Name: "Test"}
	backlog := &interfaces.Backlog{}
	current := &interfaces.CurrentTask{}

	m.SaveProject(ctx, proj)
	m.SaveBacklog(ctx, backlog)
	m.SaveCurrent(ctx, current)
	m.WriteFile(ctx, "ROADMAP.md", "# Roadmap")
	m.WriteFile(ctx, "DECISIONS.md", "decisions: []")

	err := m.SyncWithClaude(ctx)
	if err != nil {
		t.Fatalf("SyncWithClaude failed: %v", err)
	}
}

func TestManagerSyncWithClaudeFailsMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir)
	ctx := context.Background()

	err := m.SyncWithClaude(ctx)
	if err == nil {
		t.Fatal("SyncWithClaude should fail when files are missing")
	}
}

func TestManagerTimestampUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir)
	ctx := context.Background()

	proj := &interfaces.ProjectMetadata{Name: "Test"}
	beforeSave := time.Now()

	m.SaveProject(ctx, proj)

	retrieved, _ := m.GetProject(ctx)
	if retrieved.UpdatedAt.Before(beforeSave) || retrieved.UpdatedAt.After(time.Now().Add(time.Second)) {
		t.Errorf("UpdatedAt not set correctly")
	}
}

func TestManagerConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir)
	ctx := context.Background()

	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(id int) {
			proj := &interfaces.ProjectMetadata{
				Name: "Test",
			}
			m.SaveProject(ctx, proj)
			_, _ = m.GetProject(ctx)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestManagerMultipleDecisions(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir)
	ctx := context.Background()

	decision1 := interfaces.Decision{
		ID:     "ARCH-001",
		Title:  "Decision 1",
		Status: "Accepted",
	}

	decision2 := interfaces.Decision{
		ID:     "ARCH-002",
		Title:  "Decision 2",
		Status: "Proposed",
	}

	m.SaveDecision(ctx, decision1)
	m.SaveDecision(ctx, decision2)

	decisions, err := m.GetDecisions(ctx)
	if err != nil {
		t.Fatalf("GetDecisions failed: %v", err)
	}

	if len(decisions) != 2 {
		t.Errorf("Expected 2 decisions, got %d", len(decisions))
	}
}
