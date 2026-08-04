package claude

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// ExecResult captures the outcome of running an external command.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	TimedOut bool
}

// CommandExecutor runs an external command. It exists so the subprocess
// boundary can be substituted in tests without a Claude binary present.
type CommandExecutor interface {
	Execute(ctx context.Context, name string, args []string, workDir string) (*ExecResult, error)
}

// CLIExecutor runs commands as real operating system processes.
type CLIExecutor struct{}

// NewCLIExecutor creates an executor backed by os/exec.
func NewCLIExecutor() *CLIExecutor {
	return &CLIExecutor{}
}

// Execute runs the command and captures its output. A non-zero exit status is
// reported through ExecResult.ExitCode rather than as an error; an error is
// returned only when the process could not be run or observed at all.
func (e *CLIExecutor) Execute(ctx context.Context, name string, args []string, workDir string) (*ExecResult, error) {
	if name == "" {
		return nil, errors.New("command name cannot be empty")
	}

	start := time.Now()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(start),
		TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
	}

	if err == nil {
		return result, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}

	if result.TimedOut {
		result.ExitCode = -1
		return result, nil
	}

	return nil, fmt.Errorf("failed to run %s: %w", name, err)
}
