package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// Manager manages Claude Code sessions.
type Manager struct {
	projectPath      string
	model            string
	maxRetries       int
	timeoutSeconds   int
	sessionsDir      string
	currentSessionID string
	mu               sync.RWMutex
}

// SessionMetadata contains session information.
type SessionMetadata struct {
	SessionID      string    `json:"session_id"`
	StartedAt      time.Time `json:"started_at"`
	LastActivity   time.Time `json:"last_activity"`
	Status         string    `json:"status"` // active, paused, completed, failed
	TokensUsed     int       `json:"tokens_used"`
	QuotaRemaining float64   `json:"quota_remaining"`
	Output         string    `json:"output"`
	Errors         []string  `json:"errors"`
}

// New creates a new Claude session manager.
func New(projectPath, model string, maxRetries, timeoutSeconds int) (*Manager, error) {
	if projectPath == "" {
		projectPath = "."
	}

	sessionsDir := filepath.Join(projectPath, ".ai", "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sessions directory: %w", err)
	}

	return &Manager{
		projectPath:    projectPath,
		model:          model,
		maxRetries:     maxRetries,
		timeoutSeconds: timeoutSeconds,
		sessionsDir:    sessionsDir,
	}, nil
}

// LaunchSession launches a new Claude session.
func (m *Manager) LaunchSession(ctx context.Context, prompt string) (*interfaces.SessionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if prompt == "" {
		return nil, fmt.Errorf("prompt cannot be empty")
	}

	sessionID := generateSessionID()
	m.currentSessionID = sessionID

	result := &interfaces.SessionResult{
		SessionID:  sessionID,
		ExecutedAt: time.Now(),
	}

	// Create session metadata file
	metadata := &SessionMetadata{
		SessionID:      sessionID,
		StartedAt:      time.Now(),
		LastActivity:   time.Now(),
		Status:         "active",
		QuotaRemaining: 1.0,
	}

	if err := m.saveMetadata(sessionID, metadata); err != nil {
		return nil, fmt.Errorf("failed to save session metadata: %w", err)
	}

	// Execute Claude with prompt
	output, err := m.executePrompt(ctx, prompt)
	result.Output = output

	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		metadata.Status = "failed"
		metadata.Errors = result.Errors
	} else {
		metadata.Status = "completed"
	}

	metadata.LastActivity = time.Now()
	_ = m.saveMetadata(sessionID, metadata)

	return result, nil
}

// ResumeSession resumes a previous session.
func (m *Manager) ResumeSession(ctx context.Context, sessionID string) (*interfaces.SessionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if sessionID == "" {
		return nil, fmt.Errorf("session ID cannot be empty")
	}

	metadata, err := m.loadMetadata(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to load session: %w", err)
	}

	m.currentSessionID = sessionID

	result := &interfaces.SessionResult{
		SessionID:  sessionID,
		Output:     metadata.Output,
		Errors:     metadata.Errors,
		TokensUsed: metadata.TokensUsed,
		ExecutedAt: metadata.LastActivity,
	}

	metadata.LastActivity = time.Now()
	_ = m.saveMetadata(sessionID, metadata)

	return result, nil
}

// EndSession ends a session.
func (m *Manager) EndSession(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if sessionID == "" {
		return fmt.Errorf("session ID cannot be empty")
	}

	metadata, err := m.loadMetadata(sessionID)
	if err != nil {
		return err
	}

	metadata.Status = "completed"
	metadata.LastActivity = time.Now()

	return m.saveMetadata(sessionID, metadata)
}

// GetCurrentSessionID returns the current session ID.
func (m *Manager) GetCurrentSessionID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentSessionID
}

// GetSessionStatus returns current session status.
func (m *Manager) GetSessionStatus(ctx context.Context, sessionID string) (*interfaces.SessionStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if sessionID == "" {
		sessionID = m.currentSessionID
	}

	if sessionID == "" {
		return nil, fmt.Errorf("no active session")
	}

	metadata, err := m.loadMetadata(sessionID)
	if err != nil {
		return nil, err
	}

	return &interfaces.SessionStatus{
		SessionID:      sessionID,
		IsActive:       metadata.Status == "active",
		LastActivity:   metadata.LastActivity,
		TokensUsed:     metadata.TokensUsed,
		QuotaRemaining: metadata.QuotaRemaining,
		QuotaResetTime: m.estimateQuotaReset(metadata.QuotaRemaining),
	}, nil
}

// IsQuotaExhausted checks if quota is exhausted.
func (m *Manager) IsQuotaExhausted(ctx context.Context) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.currentSessionID == "" {
		return false, fmt.Errorf("no active session")
	}

	metadata, err := m.loadMetadata(m.currentSessionID)
	if err != nil {
		return false, err
	}

	return metadata.QuotaRemaining <= 0, nil
}

// GetQuotaRemaining returns remaining quota.
func (m *Manager) GetQuotaRemaining(ctx context.Context) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.currentSessionID == "" {
		return 0, fmt.Errorf("no active session")
	}

	metadata, err := m.loadMetadata(m.currentSessionID)
	if err != nil {
		return 0, err
	}

	return metadata.QuotaRemaining, nil
}

// DetectFailure detects if a failure occurred.
func (m *Manager) DetectFailure(ctx context.Context) (interfaces.FailureType, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.currentSessionID == "" {
		return interfaces.FailureTypeCrash, fmt.Errorf("no active session")
	}

	metadata, err := m.loadMetadata(m.currentSessionID)
	if err != nil {
		return interfaces.FailureTypeCrash, err
	}

	if len(metadata.Errors) > 0 {
		lastError := metadata.Errors[len(metadata.Errors)-1]

		if contains(lastError, "quota") {
			return interfaces.FailureTypeQuota, nil
		}
		if contains(lastError, "network") {
			return interfaces.FailureTypeNetwork, nil
		}
		if contains(lastError, "timeout") {
			return interfaces.FailureTypeTimeout, nil
		}
		return interfaces.FailureTypeCrash, nil
	}

	return "", nil
}

// RecoverFromFailure attempts recovery from a failure.
func (m *Manager) RecoverFromFailure(ctx context.Context, failureType interfaces.FailureType) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentSessionID == "" {
		return fmt.Errorf("no active session to recover")
	}

	metadata, err := m.loadMetadata(m.currentSessionID)
	if err != nil {
		return err
	}

	switch failureType {
	case interfaces.FailureTypeQuota:
		metadata.Status = "paused"
		metadata.Errors = append(metadata.Errors, "quota exhausted - waiting for reset")

	case interfaces.FailureTypeNetwork:
		metadata.Status = "paused"
		metadata.Errors = append(metadata.Errors, "network error - will retry")

	case interfaces.FailureTypeTimeout:
		metadata.Status = "paused"
		metadata.Errors = append(metadata.Errors, "timeout - will retry")

	case interfaces.FailureTypeCrash:
		metadata.Status = "paused"
		metadata.Errors = append(metadata.Errors, "crash detected - will retry")

	default:
		return fmt.Errorf("unknown failure type: %s", failureType)
	}

	metadata.LastActivity = time.Now()
	return m.saveMetadata(m.currentSessionID, metadata)
}

// Helper methods

func (m *Manager) executePrompt(ctx context.Context, prompt string) (string, error) {
	// For now, simulate Claude execution
	// In production, this would call actual Claude Code CLI
	return fmt.Sprintf("Executed prompt: %s (simulated)", prompt), nil
}

func (m *Manager) saveMetadata(sessionID string, metadata *SessionMetadata) error {
	path := filepath.Join(m.sessionsDir, sessionID+".json")

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

func (m *Manager) loadMetadata(sessionID string) (*SessionMetadata, error) {
	path := filepath.Join(m.sessionsDir, sessionID+".json")

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	metadata := &SessionMetadata{}
	if err := json.Unmarshal(data, metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return metadata, nil
}

func (m *Manager) estimateQuotaReset(quotaRemaining float64) time.Time {
	// Estimate quota reset in ~1 hour
	return time.Now().Add(time.Hour)
}

func generateSessionID() string {
	return fmt.Sprintf("sess_%d", time.Now().UnixNano())
}

func contains(s, substr string) bool {
	if len(substr) == 0 {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
