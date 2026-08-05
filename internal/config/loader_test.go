package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoaderLoadDefault(t *testing.T) {
	loader := NewLoader()
	if err := loader.LoadDefault(); err != nil {
		t.Fatalf("LoadDefault failed: %v", err)
	}

	tests := []struct {
		key      string
		expected interface{}
	}{
		{"project.name", "24-7-AI-LeadEngineer"},
		{"project.path", "."},
		{"project.type", "go"},
		{"claude.enabled", true},
		{"claude.model", "claude-opus-5"},
		{"claude.maxRetries", 3},
		{"quota.warningThreshold", 0.8},
		{"logging.level", "info"},
		{"logging.format", "json"},
		{"dashboard.port", 8080},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			val := loader.v.Get(tt.key)
			if val != tt.expected {
				t.Errorf("got %v, want %v", val, tt.expected)
			}
		})
	}
}

func TestLoaderLoadFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test-config.yaml")

	configContent := `
project:
  name: "TestProject"
  path: "/test/path"
  type: "go"
claude:
  enabled: true
  model: "claude-opus-5"
logging:
  level: "debug"
`

	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	loader := NewLoader()
	if err := loader.LoadDefault(); err != nil {
		t.Fatalf("LoadDefault failed: %v", err)
	}

	if err := loader.LoadFile(configFile); err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}

	if got := loader.v.GetString("project.name"); got != "TestProject" {
		t.Errorf("project.name: got %v, want TestProject", got)
	}

	if got := loader.v.GetString("logging.level"); got != "debug" {
		t.Errorf("logging.level: got %v, want debug", got)
	}
}

func TestLoaderLoadFileMissing(t *testing.T) {
	loader := NewLoader()
	if err := loader.LoadFile("/nonexistent/path/config.yaml"); err == nil {
		t.Fatal("Expected error for missing file, got nil")
	}
}

func TestLoaderLoadFileEmpty(t *testing.T) {
	loader := NewLoader()
	if err := loader.LoadFile(""); err == nil {
		t.Fatal("Expected error for empty path, got nil")
	}
}

func TestLoaderValidateProjectType(t *testing.T) {
	loader := NewLoader()
	loader.LoadDefault()

	tests := []struct {
		value   string
		wantErr bool
	}{
		{"go", false},
		{"node", false},
		{"python", false},
		{"rust", false},
		{"invalid", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			loader.v.Set("project.type", tt.value)
			err := loader.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoaderValidateClaudeConfig(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   interface{}
		wantErr bool
	}{
		{"valid maxRetries", "claude.maxRetries", 3, false},
		{"maxRetries too low", "claude.maxRetries", 0, true},
		{"maxRetries too high", "claude.maxRetries", 11, true},
		{"valid timeout", "claude.timeoutSeconds", 300, false},
		{"timeout too low", "claude.timeoutSeconds", 5, true},
		{"timeout too high", "claude.timeoutSeconds", 5000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := NewLoader()
			loader.LoadDefault()
			loader.v.Set("claude.enabled", true)
			loader.v.Set("claude.model", "claude-opus-5")
			loader.v.Set(tt.key, tt.value)
			err := loader.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoaderValidateQuotaThresholds(t *testing.T) {
	tests := []struct {
		name                string
		warningThreshold    float64
		exhaustionThreshold float64
		wantErr             bool
	}{
		{"valid", 0.8, 0.95, false},
		{"warning >= exhaustion", 0.95, 0.95, true},
		{"warning > exhaustion", 0.9, 0.8, true},
		{"warning negative", -0.1, 0.95, true},
		{"exhaustion > 1", 0.8, 1.1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := NewLoader()
			loader.LoadDefault()
			loader.v.Set("quota.warningThreshold", tt.warningThreshold)
			loader.v.Set("quota.exhaustionThreshold", tt.exhaustionThreshold)
			err := loader.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoaderValidateLoggingConfig(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		{"valid level debug", "logging.level", "debug", false},
		{"valid level info", "logging.level", "info", false},
		{"valid level warn", "logging.level", "warn", false},
		{"valid level error", "logging.level", "error", false},
		{"invalid level", "logging.level", "invalid", true},
		{"valid format json", "logging.format", "json", false},
		{"valid format text", "logging.format", "text", false},
		{"invalid format", "logging.format", "binary", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := NewLoader()
			loader.LoadDefault()
			loader.v.Set(tt.key, tt.value)
			err := loader.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoaderBuildConfig(t *testing.T) {
	loader := NewLoader()
	if err := loader.LoadDefault(); err != nil {
		t.Fatalf("LoadDefault failed: %v", err)
	}

	cfg, err := loader.BuildConfig()
	if err != nil {
		t.Fatalf("BuildConfig failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("BuildConfig returned nil config")
	}
}

func TestConfigGetString(t *testing.T) {
	loader := NewLoader()
	loader.LoadDefault()
	cfg, _ := loader.BuildConfig()

	tests := []struct {
		key      string
		expected string
	}{
		{"project.name", "24-7-AI-LeadEngineer"},
		{"claude.model", "claude-opus-5"},
		{"logging.level", "info"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			val := cfg.GetString(tt.key)
			if val != tt.expected {
				t.Errorf("got %v, want %v", val, tt.expected)
			}
		})
	}
}

func TestConfigGetInt(t *testing.T) {
	loader := NewLoader()
	loader.LoadDefault()
	cfg, _ := loader.BuildConfig()

	tests := []struct {
		key      string
		expected int
	}{
		{"claude.maxRetries", 3},
		{"claude.timeoutSeconds", 300},
		{"dashboard.port", 8080},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			val := cfg.GetInt(tt.key)
			if val != tt.expected {
				t.Errorf("got %v, want %v", val, tt.expected)
			}
		})
	}
}

func TestConfigGetBool(t *testing.T) {
	loader := NewLoader()
	loader.LoadDefault()
	cfg, _ := loader.BuildConfig()

	tests := []struct {
		key      string
		expected bool
	}{
		{"claude.enabled", true},
		{"quota.autoSleepOnExhaustion", true},
		{"checkpoint.compression", true},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			val := cfg.GetBool(tt.key)
			if val != tt.expected {
				t.Errorf("got %v, want %v", val, tt.expected)
			}
		})
	}
}

func TestConfigGetDuration(t *testing.T) {
	loader := NewLoader()
	loader.LoadDefault()
	cfg, _ := loader.BuildConfig()

	tests := []struct {
		key      string
		expected time.Duration
	}{
		{"quota.checkInterval", 30 * time.Second},
		{"checkpoint.interval", 300 * time.Second},
		{"tasks.maxDuration", 3600 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			val := cfg.GetDuration(tt.key)
			if val != tt.expected {
				t.Errorf("got %v, want %v", val, tt.expected)
			}
		})
	}
}

func TestConfigGetFloat64(t *testing.T) {
	loader := NewLoader()
	loader.LoadDefault()
	cfg, _ := loader.BuildConfig()

	tests := []struct {
		key      string
		expected float64
	}{
		{"quota.warningThreshold", 0.8},
		{"quota.exhaustionThreshold", 0.95},
		{"quality.minimumCoverage", 0.80},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			val := cfg.GetFloat64(tt.key)
			if val != tt.expected {
				t.Errorf("got %v, want %v", val, tt.expected)
			}
		})
	}
}

func TestConfigGetSpec(t *testing.T) {
	loader := NewLoader()
	loader.LoadDefault()
	cfg, _ := loader.BuildConfig()

	spec := cfg.GetSpec()
	if spec == nil {
		t.Fatal("GetSpec returned nil")
	}

	if spec.Project.Name != "24-7-AI-LeadEngineer" {
		t.Errorf("Project.Name: got %v, want 24-7-AI-LeadEngineer", spec.Project.Name)
	}

	if spec.Claude.Model != "claude-opus-5" {
		t.Errorf("Claude.Model: got %v, want claude-opus-5", spec.Claude.Model)
	}

	if spec.Quota.WarningThreshold != 0.8 {
		t.Errorf("Quota.WarningThreshold: got %v, want 0.8", spec.Quota.WarningThreshold)
	}
}

func TestLoaderValidatePermissionMode(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"acceptEdits is valid", "acceptEdits", false},
		{"plan is valid", "plan", false},
		{"bypassPermissions is valid", "bypassPermissions", false},
		{"empty omits the flag deliberately", "", false},
		{"unknown mode rejected", "yolo", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := NewLoader()
			loader.LoadDefault()
			loader.v.Set("claude.enabled", true)
			loader.v.Set("claude.model", "claude-opus-5")
			loader.v.Set("claude.permissionMode", tt.value)
			err := loader.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigDefaultPermissionMode(t *testing.T) {
	loader := NewLoader()
	loader.LoadDefault()
	cfg, _ := loader.BuildConfig()

	if got := cfg.GetString("claude.permissionMode"); got != "acceptEdits" {
		t.Errorf("default permissionMode: got %q, want acceptEdits", got)
	}
}
