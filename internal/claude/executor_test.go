package claude

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// These exercise CLIExecutor against real processes rather than a substitute,
// so the os/exec boundary itself is covered.

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell required")
	}
}

func TestCLIExecutorCapturesStdout(t *testing.T) {
	skipOnWindows(t)

	e := NewCLIExecutor()
	res, err := e.Execute(context.Background(), "sh", []string{"-c", "printf hello"}, "")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if res.Stdout != "hello" {
		t.Errorf("Stdout: got %q, want hello", res.Stdout)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode: got %d, want 0", res.ExitCode)
	}
	if res.TimedOut {
		t.Error("TimedOut should be false")
	}
}

func TestCLIExecutorCapturesStderrAndExitCode(t *testing.T) {
	skipOnWindows(t)

	e := NewCLIExecutor()
	res, err := e.Execute(context.Background(), "sh", []string{"-c", "printf oops >&2; exit 3"}, "")
	if err != nil {
		t.Fatalf("a non-zero exit must not be an error: %v", err)
	}

	if res.ExitCode != 3 {
		t.Errorf("ExitCode: got %d, want 3", res.ExitCode)
	}
	if res.Stderr != "oops" {
		t.Errorf("Stderr: got %q, want oops", res.Stderr)
	}
}

func TestCLIExecutorRespectsWorkDir(t *testing.T) {
	skipOnWindows(t)

	dir := t.TempDir()
	e := NewCLIExecutor()
	res, err := e.Execute(context.Background(), "sh", []string{"-c", "pwd"}, dir)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// macOS reports /private/var for /var, so compare on the suffix.
	got := strings.TrimSpace(res.Stdout)
	if !strings.HasSuffix(got, strings.TrimPrefix(dir, "/private")) {
		t.Errorf("workDir not applied: got %q, want suffix of %q", got, dir)
	}
}

func TestCLIExecutorTimeout(t *testing.T) {
	skipOnWindows(t)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	e := NewCLIExecutor()
	res, err := e.Execute(ctx, "sh", []string{"-c", "sleep 5"}, "")
	if err != nil {
		t.Fatalf("timeout must be reported via result, not error: %v", err)
	}

	if !res.TimedOut {
		t.Error("TimedOut should be true")
	}
}

func TestCLIExecutorMissingBinary(t *testing.T) {
	e := NewCLIExecutor()
	_, err := e.Execute(context.Background(), "definitely-not-a-real-binary-xyz", nil, "")
	if err == nil {
		t.Fatal("a missing binary must return an error")
	}
}

func TestCLIExecutorEmptyName(t *testing.T) {
	e := NewCLIExecutor()
	if _, err := e.Execute(context.Background(), "", nil, ""); err == nil {
		t.Fatal("empty command name must be rejected")
	}
}

func TestCLIExecutorRecordsDuration(t *testing.T) {
	skipOnWindows(t)

	e := NewCLIExecutor()
	res, err := e.Execute(context.Background(), "sh", []string{"-c", "sleep 0.05"}, "")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if res.Duration < 40*time.Millisecond {
		t.Errorf("Duration looks unrecorded: %v", res.Duration)
	}
}
