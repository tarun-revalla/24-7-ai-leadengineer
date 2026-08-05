// Package app is the composition root: it constructs each subsystem from
// configuration and wires them together. Subsystems depend on interfaces and
// never construct one another, so this is the single place that knows the
// concrete graph.
package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/checkpoint"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/claude"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/config"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/git"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/logging"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/memory"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/metrics"
	"github.com/tarun-revalla/24-7-ai-leadengineer/internal/quota"
)

// ErrQuotaExhausted reports that work stopped because a usage limit was hit.
// The state has been checkpointed and a resume time recorded; the caller
// should stop rather than retry.
var ErrQuotaExhausted = errors.New("claude usage limit reached; cooldown recorded")

// StateDir is the directory holding all persistent system state.
const StateDir = ".ai"

// App holds the wired subsystems for one project.
type App struct {
	ProjectPath string
	Config      *config.Config
	Logger      *logging.Logger
	Memory      *memory.Manager
	Checkpoints *checkpoint.Store
	Git         *git.Manager
	Claude      *claude.Manager
	Metrics     *metrics.Collector
	Quota       *quota.Manager
}

// Options controls construction.
type Options struct {
	// ProjectPath is the repository root. Defaults to the working directory.
	ProjectPath string
	// ConfigFile is an optional YAML file layered over the defaults.
	ConfigFile string
	// LogToStderr sends logs to the terminal instead of the configured file,
	// which is what interactive commands want.
	LogToStderr bool
}

// New builds the subsystem graph.
func New(opts Options) (*App, error) {
	projectPath := opts.ProjectPath
	if projectPath == "" {
		projectPath = "."
	}

	abs, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve project path: %w", err)
	}
	projectPath = abs

	cfg, err := loadConfig(projectPath, opts.ConfigFile)
	if err != nil {
		return nil, err
	}

	logger, err := buildLogger(cfg, projectPath, opts.LogToStderr)
	if err != nil {
		return nil, err
	}

	mem, err := memory.New(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open project memory: %w", err)
	}

	checkpoints, err := checkpoint.NewStore(
		filepath.Join(projectPath, StateDir, "checkpoints"),
		checkpoint.WithCompression(cfg.GetBool("checkpoint.compression")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open checkpoint store: %w", err)
	}

	sessions, err := claude.New(
		projectPath,
		cfg.GetString("claude.model"),
		cfg.GetInt("claude.maxRetries"),
		cfg.GetInt("claude.timeoutSeconds"),
		claude.WithPermissionMode(cfg.GetString("claude.permissionMode")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open session store: %w", err)
	}

	quotas, err := quota.New(
		filepath.Join(projectPath, StateDir),
		quota.WithCooldown(cfg.GetDuration("quota.sleepDuration")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open quota state: %w", err)
	}

	return &App{
		ProjectPath: projectPath,
		Config:      cfg,
		Logger:      logger,
		Memory:      mem,
		Checkpoints: checkpoints,
		Git: git.New(
			projectPath,
			cfg.GetString("git.committerName"),
			cfg.GetString("git.committerEmail"),
			cfg.GetBool("git.autoRetry"),
			cfg.GetInt("git.retryAttempts"),
		),
		Claude:  sessions,
		Metrics: metrics.New(),
		Quota:   quotas,
	}, nil
}

// Close flushes anything buffered. Safe to call on a partially built App.
func (a *App) Close() error {
	if a == nil || a.Logger == nil {
		return nil
	}
	// Sync on a terminal sink reports an error on some platforms even though
	// nothing was lost, so a failure here is not worth surfacing.
	_ = a.Logger.Sync()
	return nil
}

// StatePath returns a path inside the project's state directory.
func (a *App) StatePath(parts ...string) string {
	return filepath.Join(append([]string{a.ProjectPath, StateDir}, parts...)...)
}

// loadConfig layers an optional file and the environment over the defaults.
func loadConfig(projectPath, configFile string) (*config.Config, error) {
	loader := config.NewLoader()

	if err := loader.LoadDefault(); err != nil {
		return nil, fmt.Errorf("failed to load default configuration: %w", err)
	}

	// An explicitly named file must exist; the conventional one is optional so
	// the tool works in a project that has never been configured.
	if configFile != "" {
		if err := loader.LoadFile(configFile); err != nil {
			return nil, err
		}
	} else {
		conventional := filepath.Join(projectPath, "config.yaml")
		if _, err := os.Stat(conventional); err == nil {
			if err := loader.LoadFile(conventional); err != nil {
				return nil, err
			}
		}
	}

	if err := loader.LoadEnv(""); err != nil {
		return nil, fmt.Errorf("failed to read environment configuration: %w", err)
	}

	cfg, err := loader.BuildConfig()
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

func buildLogger(cfg *config.Config, projectPath string, toStderr bool) (*logging.Logger, error) {
	level := cfg.GetString("logging.level")
	format := cfg.GetString("logging.format")

	if toStderr {
		return logging.New(level, format, "")
	}

	output := cfg.GetString("logging.output")
	if output == "" || output == "stdout" {
		return logging.New(level, format, output)
	}

	if !filepath.IsAbs(output) {
		output = filepath.Join(projectPath, output)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	return logging.New(level, format, output)
}
