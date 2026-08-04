package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

func TestNewManager(t *testing.T) {
	tmpDir := t.TempDir()

	m, err := New(tmpDir, "claude-opus-5", 3, 300)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if m == nil {
		t.Fatal("New returned nil")
	}

	sessionsPath := filepath.Join(tmpDir, ".ai", "sessions")
	if _, err := os.Stat(sessionsPath); err != nil {
		t.Fatalf("Sessions directory not created: %v", err)
	}
}

func TestNewManagerDefault(t *testing.T) {
	m, err := New("", "claude-opus-5", 3, 300)
	if err != nil {
		t.Fatalf("New with empty path failed: %v", err)
	}

	if m == nil {
		t.Fatal("New returned nil")
	}
}

func TestLaunchSession(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	result, err := m.LaunchSession(ctx, "Test prompt")
	if err != nil {
		t.Fatalf("LaunchSession failed: %v", err)
	}

	if result == nil {
		t.Fatal("Result is nil")
	}

	if result.SessionID == "" {
		t.Error("SessionID is empty")
	}

	if result.Output == "" {
		t.Error("Output is empty")
	}
}

func TestLaunchSessionEmptyPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	_, err := m.LaunchSession(ctx, "")
	if err == nil {
		t.Fatal("LaunchSession should fail with empty prompt")
	}
}

func TestResumeSession(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	result1, _ := m.LaunchSession(ctx, "First prompt")
	sessionID := result1.SessionID

	result2, err := m.ResumeSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("ResumeSession failed: %v", err)
	}

	if result2.SessionID != sessionID {
		t.Errorf("SessionID: got %s, want %s", result2.SessionID, sessionID)
	}
}

func TestResumeSessionEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	_, err := m.ResumeSession(ctx, "")
	if err == nil {
		t.Fatal("ResumeSession should fail with empty sessionID")
	}
}

func TestResumeSessionNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	_, err := m.ResumeSession(ctx, "nonexistent")
	if err == nil {
		t.Fatal("ResumeSession should fail for nonexistent session")
	}
}

func TestEndSession(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	result, _ := m.LaunchSession(ctx, "Prompt")
	sessionID := result.SessionID

	err := m.EndSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("EndSession failed: %v", err)
	}
}

func TestGetCurrentSessionID(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	if m.GetCurrentSessionID() != "" {
		t.Error("Current session should be empty initially")
	}

	result, _ := m.LaunchSession(ctx, "Prompt")
	sessionID := m.GetCurrentSessionID()

	if sessionID != result.SessionID {
		t.Errorf("SessionID: got %s, want %s", sessionID, result.SessionID)
	}
}

func TestGetSessionStatus(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	result, _ := m.LaunchSession(ctx, "Prompt")
	sessionID := result.SessionID

	status, err := m.GetSessionStatus(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSessionStatus failed: %v", err)
	}

	if status == nil {
		t.Fatal("Status is nil")
	}

	if status.SessionID != sessionID {
		t.Errorf("SessionID: got %s, want %s", status.SessionID, sessionID)
	}
}

func TestGetSessionStatusCurrent(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	result, _ := m.LaunchSession(ctx, "Prompt")

	status, err := m.GetSessionStatus(ctx, "")
	if err != nil {
		t.Fatalf("GetSessionStatus failed: %v", err)
	}

	if status.SessionID != result.SessionID {
		t.Errorf("Should use current session when empty ID provided")
	}
}

func TestGetSessionStatusNoSession(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	_, err := m.GetSessionStatus(ctx, "")
	if err == nil {
		t.Fatal("GetSessionStatus should fail with no active session")
	}
}

func TestIsQuotaExhausted(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	m.LaunchSession(ctx, "Prompt")

	exhausted, err := m.IsQuotaExhausted(ctx)
	if err != nil {
		t.Fatalf("IsQuotaExhausted failed: %v", err)
	}

	if exhausted != false {
		t.Error("Quota should not be exhausted initially")
	}
}

func TestGetQuotaRemaining(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	m.LaunchSession(ctx, "Prompt")

	quota, err := m.GetQuotaRemaining(ctx)
	if err != nil {
		t.Fatalf("GetQuotaRemaining failed: %v", err)
	}

	if quota < 0 || quota > 1 {
		t.Errorf("Quota should be 0-1, got %f", quota)
	}
}

func TestDetectFailure(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	m.LaunchSession(ctx, "Prompt")

	failureType, err := m.DetectFailure(ctx)
	if err != nil {
		t.Fatalf("DetectFailure failed: %v", err)
	}

	if failureType != "" {
		t.Errorf("Should not detect failure on success, got %s", failureType)
	}
}

func TestRecoverFromFailure(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	m.LaunchSession(ctx, "Prompt")

	err := m.RecoverFromFailure(ctx, interfaces.FailureTypeQuota)
	if err != nil {
		t.Fatalf("RecoverFromFailure failed: %v", err)
	}
}

func TestRecoverFromFailureNoSession(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	err := m.RecoverFromFailure(ctx, interfaces.FailureTypeQuota)
	if err == nil {
		t.Fatal("RecoverFromFailure should fail with no active session")
	}
}

func TestConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	m, _ := New(tmpDir, "claude-opus-5", 3, 300)
	ctx := context.Background()

	done := make(chan bool)

	m.LaunchSession(ctx, "Prompt")

	for i := 0; i < 5; i++ {
		go func() {
			_, _ = m.GetSessionStatus(ctx, "")
			_, _ = m.GetQuotaRemaining(ctx)
			done <- true
		}()
	}

	for i := 0; i < 5; i++ {
		<-done
	}
}

func TestGenerateSessionID(t *testing.T) {
	id1 := generateSessionID()
	id2 := generateSessionID()

	if id1 == "" {
		t.Error("SessionID should not be empty")
	}

	if id1 == id2 {
		t.Error("Generated IDs should be unique")
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		s      string
		substr string
		want   bool
	}{
		{"hello", "ell", true},
		{"hello", "hello", true},
		{"hello", "xyz", false},
		{"", "", false},
		{"hello", "", false},
	}

	for _, tt := range tests {
		got := contains(tt.s, tt.substr)
		if got != tt.want {
			t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
		}
	}
}
