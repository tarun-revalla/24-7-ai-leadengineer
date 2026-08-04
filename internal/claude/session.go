package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// DefaultBinary is the Claude Code executable invoked when none is configured.
const DefaultBinary = "claude"

// Manager must satisfy the contract the rest of the system depends on.
var _ interfaces.ClaudeSessionManager = (*Manager)(nil)

// Manager manages Claude Code sessions.
type Manager struct {
	projectPath      string
	model            string
	maxRetries       int
	timeoutSeconds   int
	sessionsDir      string
	currentSessionID string
	binary           string
	executor         CommandExecutor
	mu               sync.RWMutex
}

// Option customizes a Manager at construction time.
type Option func(*Manager)

// WithExecutor substitutes the command executor. Used by tests to drive the
// session manager without a Claude binary on PATH.
func WithExecutor(e CommandExecutor) Option {
	return func(m *Manager) { m.executor = e }
}

// WithBinary overrides the Claude executable name or path.
func WithBinary(binary string) Option {
	return func(m *Manager) { m.binary = binary }
}

// SessionMetadata contains session information.
type SessionMetadata struct {
	SessionID string `json:"session_id"`
	// CLISessionID is the identifier Claude Code itself assigned to the
	// conversation. It is what --resume expects, and differs from SessionID,
	// which this system generates to name its own metadata file.
	CLISessionID string    `json:"cli_session_id,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	LastActivity time.Time `json:"last_activity"`
	Status       string    `json:"status"` // active, paused, completed, failed
	// LastFailure records how the most recent invocation failed, as classified
	// at the point of failure. Storing it avoids re-deriving the category from
	// error prose later, which drifts from the real classifier.
	LastFailure    string   `json:"last_failure,omitempty"`
	TokensUsed     int      `json:"tokens_used"`
	Turns          int      `json:"turns"`
	CostUSD        float64  `json:"cost_usd"`
	QuotaRemaining float64  `json:"quota_remaining"`
	Output         string   `json:"output"`
	Errors         []string `json:"errors"`
}

// New creates a new Claude session manager.
func New(projectPath, model string, maxRetries, timeoutSeconds int, opts ...Option) (*Manager, error) {
	if projectPath == "" {
		projectPath = "."
	}

	sessionsDir := filepath.Join(projectPath, ".ai", "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sessions directory: %w", err)
	}

	m := &Manager{
		projectPath:    projectPath,
		model:          model,
		maxRetries:     maxRetries,
		timeoutSeconds: timeoutSeconds,
		sessionsDir:    sessionsDir,
		binary:         DefaultBinary,
		executor:       NewCLIExecutor(),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m, nil
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

	return m.runAndRecord(ctx, sessionID, prompt, "", metadata)
}

// runAndRecord invokes Claude, applies retry policy, and persists the outcome
// to the session's metadata. The caller must hold m.mu.
func (m *Manager) runAndRecord(
	ctx context.Context,
	sessionID string,
	prompt string,
	resumeID string,
	metadata *SessionMetadata,
) (*interfaces.SessionResult, error) {
	started := time.Now()

	result := &interfaces.SessionResult{
		SessionID:  sessionID,
		ExecutedAt: started,
	}

	attempts := m.maxRetries
	if attempts < 1 {
		attempts = 1
	}

	var outcome *promptOutcome

	for attempt := 1; attempt <= attempts; attempt++ {
		o, err := m.executePrompt(ctx, prompt, resumeID)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			metadata.Status = "failed"
			metadata.Errors = result.Errors
			metadata.LastActivity = time.Now()
			result.Duration = time.Since(started)
			_ = m.saveMetadata(sessionID, metadata)
			return result, err
		}

		outcome = o

		// A usage limit will not clear by retrying immediately; surface it so
		// the caller can checkpoint and sleep until the window resets.
		if outcome.failure == interfaces.FailureTypeQuota || outcome.failure == "" {
			break
		}

		if attempt < attempts {
			result.Errors = append(result.Errors,
				fmt.Sprintf("attempt %d/%d failed: %s", attempt, attempts, outcome.failureText))
		}
	}

	result.Output = outcome.output
	result.Duration = time.Since(started)

	metadata.Output = outcome.output
	metadata.LastActivity = time.Now()

	if outcome.cliSessionID != "" {
		metadata.CLISessionID = outcome.cliSessionID
	}
	if outcome.turns > 0 {
		metadata.Turns = outcome.turns
	}
	metadata.CostUSD += outcome.costUSD

	if outcome.failure != "" {
		result.Errors = append(result.Errors, outcome.failureText)
		metadata.Errors = result.Errors
		metadata.LastFailure = string(outcome.failure)

		if outcome.failure == interfaces.FailureTypeQuota {
			metadata.Status = "paused"
			metadata.QuotaRemaining = 0
		} else {
			metadata.Status = "failed"
		}
	} else {
		metadata.Status = "completed"
		metadata.Errors = result.Errors
		metadata.LastFailure = ""
	}

	if err := m.saveMetadata(sessionID, metadata); err != nil {
		return result, fmt.Errorf("failed to persist session metadata: %w", err)
	}

	return result, nil
}

// ResumeSession restores a previous session as the current one and returns its
// last recorded state. It does not invoke Claude — use ContinueSession to send
// a new prompt into an existing conversation.
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

// ContinueSession sends a new prompt into an existing Claude conversation,
// preserving the prior turns. The session must have been launched previously.
func (m *Manager) ContinueSession(ctx context.Context, sessionID, prompt string) (*interfaces.SessionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if sessionID == "" {
		return nil, fmt.Errorf("session ID cannot be empty")
	}
	if prompt == "" {
		return nil, fmt.Errorf("prompt cannot be empty")
	}

	metadata, err := m.loadMetadata(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to load session: %w", err)
	}

	m.currentSessionID = sessionID
	metadata.Status = "active"

	resumeID := firstNonEmpty(metadata.CLISessionID, sessionID)
	return m.runAndRecord(ctx, sessionID, prompt, resumeID, metadata)
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

	if metadata.LastFailure != "" {
		return interfaces.FailureType(metadata.LastFailure), nil
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

// promptOutcome is the interpreted result of one Claude CLI invocation.
type promptOutcome struct {
	output       string
	cliSessionID string
	turns        int
	costUSD      float64
	failure      interfaces.FailureType
	failureText  string
}

// executePrompt invokes the Claude Code CLI once and interprets its output.
// Transport and limit failures are reported through promptOutcome.failure
// rather than as errors; an error means the CLI could not be run at all.
func (m *Manager) executePrompt(ctx context.Context, prompt string, resumeID string) (*promptOutcome, error) {
	args := []string{"-p", prompt, "--output-format", "json"}
	if m.model != "" {
		args = append(args, "--model", m.model)
	}
	if resumeID != "" {
		args = append(args, "--resume", resumeID)
	}

	runCtx := ctx
	if m.timeoutSeconds > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(m.timeoutSeconds)*time.Second)
		defer cancel()
	}

	res, err := m.executor.Execute(runCtx, m.binary, args, m.projectPath)
	if err != nil {
		return nil, fmt.Errorf("claude invocation failed: %w", err)
	}

	combined := res.Stdout + "\n" + res.Stderr
	outcome := &promptOutcome{output: res.Stdout}

	if parsed, ok := parseCLIResponse(res.Stdout); ok {
		outcome.cliSessionID = parsed.SessionID
		outcome.turns = parsed.NumTurns
		outcome.costUSD = parsed.TotalCost
		if parsed.Result != "" {
			outcome.output = parsed.Result
		}
		if parsed.IsError {
			outcome.failure = interfaces.FailureTypeCrash
			outcome.failureText = firstNonEmpty(parsed.Result, parsed.Subtype, "claude reported an error")
		}
	}

	switch {
	case res.TimedOut:
		outcome.failure = interfaces.FailureTypeTimeout
		outcome.failureText = fmt.Sprintf("claude timed out after %ds", m.timeoutSeconds)
	case isQuotaExhaustion(combined):
		outcome.failure = interfaces.FailureTypeQuota
		outcome.failureText = "claude usage limit reached"
	case isNetworkFailure(combined):
		outcome.failure = interfaces.FailureTypeNetwork
		outcome.failureText = "network failure contacting claude"
	case res.ExitCode != 0 && outcome.failure == "":
		outcome.failure = interfaces.FailureTypeCrash
		outcome.failureText = fmt.Sprintf("claude exited with status %d: %s",
			res.ExitCode, strings.TrimSpace(res.Stderr))
	}

	return outcome, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
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
