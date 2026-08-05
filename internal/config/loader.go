package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/types"
)

// Loader loads configuration from multiple sources.
type Loader struct {
	v *viper.Viper
}

// NewLoader creates a new configuration loader.
func NewLoader() *Loader {
	return &Loader{
		v: viper.New(),
	}
}

// LoadDefault loads the default configuration template.
func (l *Loader) LoadDefault() error {
	l.v.SetDefault("project.name", "24-7-AI-LeadEngineer")
	l.v.SetDefault("project.path", ".")
	l.v.SetDefault("project.type", "go")

	l.v.SetDefault("claude.enabled", true)
	l.v.SetDefault("claude.model", "claude-opus-5")
	l.v.SetDefault("claude.maxRetries", 3)
	l.v.SetDefault("claude.timeoutSeconds", 300)
	l.v.SetDefault("claude.contextWindowSize", 200000)
	l.v.SetDefault("claude.maxTokensPerRequest", 4000)
	// acceptEdits grants file write/edit without exposing Bash — Claude's -p
	// mode denies every tool call by default, and this system's quality gates
	// already run outside Claude, so Bash access is not needed for it to work.
	l.v.SetDefault("claude.permissionMode", "acceptEdits")

	l.v.SetDefault("quota.checkInterval", 30)
	l.v.SetDefault("quota.warningThreshold", 0.8)
	l.v.SetDefault("quota.exhaustionThreshold", 0.95)
	l.v.SetDefault("quota.autoSleepOnExhaustion", true)
	l.v.SetDefault("quota.sleepDuration", 3600)

	l.v.SetDefault("checkpoint.interval", 300)
	l.v.SetDefault("checkpoint.maxSize", "100MB")
	l.v.SetDefault("checkpoint.retention", 7)
	l.v.SetDefault("checkpoint.compression", true)

	l.v.SetDefault("git.committerName", "24-7-AI-LeadEngineer")
	l.v.SetDefault("git.committerEmail", "ai@leadengineer.local")
	l.v.SetDefault("git.autoRetry", true)
	l.v.SetDefault("git.retryAttempts", 4)
	l.v.SetDefault("git.retryBackoffMs", 1000)

	l.v.SetDefault("tasks.maxDuration", 3600)
	l.v.SetDefault("tasks.autoCheckpoint", true)
	l.v.SetDefault("tasks.autoCommit", true)
	l.v.SetDefault("tasks.parallelExecution", false)

	l.v.SetDefault("logging.level", "info")
	l.v.SetDefault("logging.format", "json")
	l.v.SetDefault("logging.output", ".ai/logs/app.log")
	l.v.SetDefault("logging.retention", 30)

	l.v.SetDefault("metrics.enabled", true)
	l.v.SetDefault("metrics.exportInterval", 300)
	l.v.SetDefault("metrics.exportPath", ".ai/metrics/")
	l.v.SetDefault("metrics.prometheusPort", 9090)

	l.v.SetDefault("plugins.enabled", false)
	l.v.SetDefault("plugins.directory", "plugins/")

	l.v.SetDefault("dashboard.enabled", true)
	l.v.SetDefault("dashboard.port", 8080)
	l.v.SetDefault("dashboard.address", "127.0.0.1")

	l.v.SetDefault("quality.requireTests", true)
	l.v.SetDefault("quality.minimumCoverage", 0.80)
	l.v.SetDefault("quality.runLinter", true)
	l.v.SetDefault("quality.runSecurity", true)
	l.v.SetDefault("quality.runPerformance", false)
	// Verification by reading the diff instead of running the project's
	// tooling. Off by default: it is a deliberate trade, not a fallback to
	// slip into silently.
	l.v.SetDefault("quality.inspectionOnly", false)

	l.v.SetDefault("review.enabled", true)
	l.v.SetDefault("review.maxRevisions", 2)

	l.v.SetDefault("recovery.enabled", true)
	l.v.SetDefault("recovery.maxAttempts", 3)
	l.v.SetDefault("recovery.timeoutSeconds", 300)
	l.v.SetDefault("recovery.conflictStrategy", "manual")

	return nil
}

// LoadFile loads configuration from a YAML file.
func (l *Loader) LoadFile(path string) error {
	if path == "" {
		return errors.New("config file path cannot be empty")
	}

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s", path)
		}
		return fmt.Errorf("error accessing config file: %w", err)
	}

	l.v.SetConfigFile(path)
	if err := l.v.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	return nil
}

// LoadEnv loads configuration from environment variables.
func (l *Loader) LoadEnv(prefix string) error {
	if prefix == "" {
		prefix = "LEADENG"
	}

	l.v.SetEnvPrefix(prefix)
	l.v.AutomaticEnv()

	return nil
}

// Set overrides one setting for this process only.
//
// Nothing is written back to any file. This exists for command-line flags,
// which change a single run rather than the project's configuration.
func (l *Loader) Set(key string, value any) {
	l.v.Set(key, value)
}

// BuildConfig builds the final configuration from all loaded sources.
func (l *Loader) BuildConfig() (*Config, error) {
	if err := l.validate(); err != nil {
		return nil, err
	}

	cfg := &Config{
		v: l.v,
	}

	return cfg, nil
}

// validate checks that the configuration is valid.
func (l *Loader) validate() error {
	// Validate project configuration
	projectType := l.v.GetString("project.type")
	validTypes := map[string]bool{
		"go": true, "node": true, "javascript": true, "typescript": true,
		"python": true, "rust": true,
	}
	if !validTypes[projectType] {
		return fmt.Errorf("invalid project type: %s", projectType)
	}

	// Validate Claude configuration
	if l.v.GetBool("claude.enabled") {
		model := l.v.GetString("claude.model")
		if model == "" {
			return errors.New("claude model cannot be empty when enabled")
		}

		maxRetries := l.v.GetInt("claude.maxRetries")
		if maxRetries < 1 || maxRetries > 10 {
			return errors.New("claude maxRetries must be between 1 and 10")
		}

		// Empty is a deliberate, valid choice: it omits --permission-mode for a
		// caller that wants Claude's own default (every tool call denied under
		// -p), such as a read-only or conversational session.
		if mode := l.v.GetString("claude.permissionMode"); mode != "" {
			validModes := map[string]bool{
				"acceptEdits": true, "auto": true, "bypassPermissions": true,
				"manual": true, "dontAsk": true, "plan": true,
			}
			if !validModes[mode] {
				return fmt.Errorf("invalid claude permissionMode: %s", mode)
			}
		}

		timeout := l.v.GetInt("claude.timeoutSeconds")
		if timeout < 10 || timeout > 3600 {
			return errors.New("claude timeoutSeconds must be between 10 and 3600")
		}
	}

	// Validate quota configuration
	warningThreshold := l.v.GetFloat64("quota.warningThreshold")
	if warningThreshold < 0 || warningThreshold > 1 {
		return errors.New("quota warningThreshold must be between 0 and 1")
	}

	exhaustionThreshold := l.v.GetFloat64("quota.exhaustionThreshold")
	if exhaustionThreshold < 0 || exhaustionThreshold > 1 {
		return errors.New("quota exhaustionThreshold must be between 0 and 1")
	}

	if exhaustionThreshold <= warningThreshold {
		return errors.New("quota exhaustionThreshold must be greater than warningThreshold")
	}

	// Validate logging configuration
	level := l.v.GetString("logging.level")
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[level] {
		return fmt.Errorf("invalid logging level: %s", level)
	}

	format := l.v.GetString("logging.format")
	validFormats := map[string]bool{"json": true, "text": true}
	if !validFormats[format] {
		return fmt.Errorf("invalid logging format: %s", format)
	}

	// Validate quality configuration
	minCoverage := l.v.GetFloat64("quality.minimumCoverage")
	if minCoverage < 0 || minCoverage > 1 {
		return errors.New("quality minimumCoverage must be between 0 and 1")
	}

	// Validate review configuration. Zero revisions is valid — it means the
	// first rejection is final — so only a negative value is an error.
	if revisions := l.v.GetInt("review.maxRevisions"); revisions < 0 || revisions > 10 {
		return errors.New("review maxRevisions must be between 0 and 10")
	}

	return nil
}

// Config provides access to configuration values.
type Config struct {
	v *viper.Viper
}

// GetString returns a string configuration value.
func (c *Config) GetString(key string) string {
	return c.v.GetString(key)
}

// GetInt returns an int configuration value.
func (c *Config) GetInt(key string) int {
	return c.v.GetInt(key)
}

// GetBool returns a bool configuration value.
func (c *Config) GetBool(key string) bool {
	return c.v.GetBool(key)
}

// GetFloat64 returns a float64 configuration value.
func (c *Config) GetFloat64(key string) float64 {
	return c.v.GetFloat64(key)
}

// GetDuration returns a duration configuration value (seconds to duration).
func (c *Config) GetDuration(key string) time.Duration {
	seconds := c.v.GetInt(key)
	return time.Duration(seconds) * time.Second
}

// GetStringSlice returns a string slice configuration value.
func (c *Config) GetStringSlice(key string) []string {
	return c.v.GetStringSlice(key)
}

// GetSpec returns the complete configuration as a struct.
func (c *Config) GetSpec() *types.ConfigSpec {
	spec := &types.ConfigSpec{}

	spec.Project.Name = c.GetString("project.name")
	spec.Project.Path = c.GetString("project.path")
	spec.Project.Type = c.GetString("project.type")

	spec.Claude.Enabled = c.GetBool("claude.enabled")
	spec.Claude.Model = c.GetString("claude.model")
	spec.Claude.MaxRetries = c.GetInt("claude.maxRetries")
	spec.Claude.TimeoutSeconds = c.GetInt("claude.timeoutSeconds")
	spec.Claude.ContextWindowSize = c.GetInt("claude.contextWindowSize")
	spec.Claude.MaxTokensPerRequest = c.GetInt("claude.maxTokensPerRequest")

	spec.Quota.CheckInterval = c.GetDuration("quota.checkInterval")
	spec.Quota.WarningThreshold = c.GetFloat64("quota.warningThreshold")
	spec.Quota.ExhaustionThreshold = c.GetFloat64("quota.exhaustionThreshold")
	spec.Quota.AutoSleepOnExhaustion = c.GetBool("quota.autoSleepOnExhaustion")
	spec.Quota.SleepDuration = c.GetDuration("quota.sleepDuration")

	spec.Checkpoint.Interval = c.GetDuration("checkpoint.interval")
	spec.Checkpoint.MaxSize = c.GetString("checkpoint.maxSize")
	spec.Checkpoint.Retention = c.GetInt("checkpoint.retention")
	spec.Checkpoint.Compression = c.GetBool("checkpoint.compression")

	spec.Git.CommitterName = c.GetString("git.committerName")
	spec.Git.CommitterEmail = c.GetString("git.committerEmail")
	spec.Git.AutoRetry = c.GetBool("git.autoRetry")
	spec.Git.RetryAttempts = c.GetInt("git.retryAttempts")
	spec.Git.RetryBackoff = c.GetDuration("git.retryBackoffMs") / 1000

	spec.Tasks.MaxDuration = c.GetDuration("tasks.maxDuration")
	spec.Tasks.AutoCheckpoint = c.GetBool("tasks.autoCheckpoint")
	spec.Tasks.AutoCommit = c.GetBool("tasks.autoCommit")
	spec.Tasks.ParallelExecution = c.GetBool("tasks.parallelExecution")

	spec.Logging.Level = c.GetString("logging.level")
	spec.Logging.Format = c.GetString("logging.format")
	spec.Logging.Output = c.GetString("logging.output")
	spec.Logging.Retention = c.GetInt("logging.retention")

	spec.Metrics.Enabled = c.GetBool("metrics.enabled")
	spec.Metrics.ExportInterval = c.GetDuration("metrics.exportInterval")
	spec.Metrics.ExportPath = c.GetString("metrics.exportPath")
	spec.Metrics.PrometheusPort = c.GetInt("metrics.prometheusPort")

	spec.Plugins.Enabled = c.GetBool("plugins.enabled")
	spec.Plugins.Directory = c.GetString("plugins.directory")

	spec.Dashboard.Enabled = c.GetBool("dashboard.enabled")
	spec.Dashboard.Port = c.GetInt("dashboard.port")
	spec.Dashboard.Address = c.GetString("dashboard.address")

	return spec
}
