package logging

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewLogger(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	logger, err := New("info", "json", logFile)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if logger == nil {
		t.Fatal("New returned nil logger")
	}

	defer logger.Sync()
}

func TestLoggerDebug(t *testing.T) {
	logger, _ := New("debug", "json", "")
	defer logger.Sync()

	logger.Debug("test message", "key", "value")
}

func TestLoggerInfo(t *testing.T) {
	logger, _ := New("info", "json", "")
	defer logger.Sync()

	logger.Info("test message", "taskID", "task-123")
}

func TestLoggerWarn(t *testing.T) {
	logger, _ := New("info", "json", "")
	defer logger.Sync()

	logger.Warn("test warning", "count", 42)
}

func TestLoggerError(t *testing.T) {
	logger, _ := New("info", "json", "")
	defer logger.Sync()

	logger.Error("test error", "code", "ERR_001")
}

func TestLoggerWithFields(t *testing.T) {
	logger, _ := New("info", "json", "")
	defer logger.Sync()

	contextLogger := logger.WithFields("taskID", "task-123", "attempt", 1)
	contextLogger.Info("message with context")
}

func TestLoggerTextFormat(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	logger, err := New("info", "text", logFile)
	if err != nil {
		t.Fatalf("New with text format failed: %v", err)
	}

	defer logger.Sync()

	logger.Info("test message")

	if _, err := os.Stat(logFile); err != nil {
		t.Fatalf("Log file not created: %v", err)
	}
}

func TestLoggerStdout(t *testing.T) {
	logger, err := New("info", "json", "stdout")
	if err != nil {
		t.Fatalf("New with stdout failed: %v", err)
	}

	defer logger.Sync()
	logger.Info("test to stdout")
}

func TestLoggerFileCreation(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "app.log")

	logger, err := New("info", "json", logFile)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	logger.Info("test message")
	logger.Sync()

	if _, err := os.Stat(logFile); err != nil {
		t.Fatalf("Log file not found: %v", err)
	}
}

func TestNoOpLogger(t *testing.T) {
	logger := &NoOpLogger{}

	logger.Debug("debug")
	logger.Info("info")
	logger.Warn("warn")
	logger.Error("error")

	_ = logger.Sync()

	contextLogger := logger.WithFields("key", "value")
	contextLogger.Info("with fields")
}

func TestLoggerOddFields(t *testing.T) {
	logger, _ := New("info", "json", "")
	defer logger.Sync()

	logger.Info("message with odd fields", "key")
}

func TestStringToLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"debug", "debug"},
		{"info", "info"},
		{"warn", "warn"},
		{"error", "error"},
		{"unknown", "info"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			level := stringToLevel(tt.input)
			if level.String() != tt.expected {
				t.Errorf("stringToLevel(%s) = %s, want %s", tt.input, level.String(), tt.expected)
			}
		})
	}
}
