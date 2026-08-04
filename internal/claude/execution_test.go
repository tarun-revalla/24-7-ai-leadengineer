package claude

import (
	"context"
	"strings"
	"testing"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

const jsonOK = `{"session_id":"cli-abc","result":"done","is_error":false,"num_turns":2,"total_cost_usd":0.03}`

func newTestManager(t *testing.T, fe *fakeExecutor) *Manager {
	t.Helper()
	m, err := New(t.TempDir(), "claude-opus-5", 3, 300, WithExecutor(fe), WithBinary("claude"))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	return m
}

func TestLaunchInvokesCLIWithExpectedArgs(t *testing.T) {
	fe := newFakeExecutor(okResponse(jsonOK))
	m := newTestManager(t, fe)

	if _, err := m.LaunchSession(context.Background(), "build the thing"); err != nil {
		t.Fatalf("LaunchSession failed: %v", err)
	}

	if fe.count() != 1 {
		t.Fatalf("expected 1 CLI invocation, got %d", fe.count())
	}

	call := fe.lastCall()
	if call.name != "claude" {
		t.Errorf("binary: got %q, want claude", call.name)
	}
	if !hasArgPair(call.args, "-p", "build the thing") {
		t.Errorf("prompt not passed via -p: %v", call.args)
	}
	if !hasArgPair(call.args, "--output-format", "json") {
		t.Errorf("json output format not requested: %v", call.args)
	}
	if !hasArgPair(call.args, "--model", "claude-opus-5") {
		t.Errorf("model not passed: %v", call.args)
	}
	if hasArg(call.args, "--resume") {
		t.Errorf("fresh launch must not pass --resume: %v", call.args)
	}
}

func TestLaunchParsesJSONEnvelope(t *testing.T) {
	fe := newFakeExecutor(okResponse(jsonOK))
	m := newTestManager(t, fe)

	res, err := m.LaunchSession(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("LaunchSession failed: %v", err)
	}

	if res.Output != "done" {
		t.Errorf("output: got %q, want done", res.Output)
	}
	if len(res.Errors) != 0 {
		t.Errorf("expected no errors, got %v", res.Errors)
	}

	md, err := m.loadMetadata(res.SessionID)
	if err != nil {
		t.Fatalf("loadMetadata failed: %v", err)
	}
	if md.CLISessionID != "cli-abc" {
		t.Errorf("CLISessionID: got %q, want cli-abc", md.CLISessionID)
	}
	if md.Turns != 2 {
		t.Errorf("Turns: got %d, want 2", md.Turns)
	}
	if md.Status != "completed" {
		t.Errorf("Status: got %q, want completed", md.Status)
	}
}

func TestLaunchHandlesNonJSONOutput(t *testing.T) {
	fe := newFakeExecutor(okResponse("plain text reply"))
	m := newTestManager(t, fe)

	res, err := m.LaunchSession(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("LaunchSession failed: %v", err)
	}

	if res.Output != "plain text reply" {
		t.Errorf("output: got %q, want plain text reply", res.Output)
	}
}

func TestQuotaExhaustionPausesWithoutRetry(t *testing.T) {
	fe := newFakeExecutor(exitResponse(1, "", "Claude usage limit reached. Try again later."))
	m := newTestManager(t, fe)

	res, err := m.LaunchSession(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("LaunchSession returned error: %v", err)
	}

	// A usage limit does not clear by retrying, so it must not consume attempts.
	if fe.count() != 1 {
		t.Errorf("quota failure retried %d times, want 1 invocation", fe.count())
	}

	md, _ := m.loadMetadata(res.SessionID)
	if md.Status != "paused" {
		t.Errorf("Status: got %q, want paused", md.Status)
	}
	if md.QuotaRemaining != 0 {
		t.Errorf("QuotaRemaining: got %v, want 0", md.QuotaRemaining)
	}

	exhausted, err := m.IsQuotaExhausted(context.Background())
	if err != nil {
		t.Fatalf("IsQuotaExhausted failed: %v", err)
	}
	if !exhausted {
		t.Error("IsQuotaExhausted should report true after a usage limit")
	}
}

func TestNetworkFailureRetriesUpToMax(t *testing.T) {
	fe := newFakeExecutor(exitResponse(1, "", "dial tcp: connection refused"))
	m := newTestManager(t, fe)

	res, err := m.LaunchSession(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("LaunchSession returned error: %v", err)
	}

	if fe.count() != 3 {
		t.Errorf("expected 3 attempts (maxRetries=3), got %d", fe.count())
	}

	md, _ := m.loadMetadata(res.SessionID)
	if md.Status != "failed" {
		t.Errorf("Status: got %q, want failed", md.Status)
	}
}

func TestTransientFailureThenSuccess(t *testing.T) {
	fe := newFakeExecutor(
		exitResponse(1, "", "connection reset by peer"),
		okResponse(jsonOK),
	)
	m := newTestManager(t, fe)

	res, err := m.LaunchSession(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("LaunchSession failed: %v", err)
	}

	if fe.count() != 2 {
		t.Errorf("expected 2 attempts, got %d", fe.count())
	}

	md, _ := m.loadMetadata(res.SessionID)
	if md.Status != "completed" {
		t.Errorf("Status: got %q, want completed after recovery", md.Status)
	}
}

func TestTimeoutClassified(t *testing.T) {
	fe := newFakeExecutor(timeoutResponse())
	m := newTestManager(t, fe)

	res, err := m.LaunchSession(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("LaunchSession returned error: %v", err)
	}

	joined := strings.Join(res.Errors, " ")
	if !strings.Contains(joined, "timed out") {
		t.Errorf("expected timeout in errors, got %v", res.Errors)
	}
}

func TestCLIErrorFlagTreatedAsFailure(t *testing.T) {
	fe := newFakeExecutor(okResponse(`{"session_id":"x","result":"boom","is_error":true}`))
	m := newTestManager(t, fe)

	res, err := m.LaunchSession(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("LaunchSession returned error: %v", err)
	}

	if len(res.Errors) == 0 {
		t.Error("is_error:true must produce a recorded error")
	}
}

func TestExecutorErrorSurfaces(t *testing.T) {
	fe := newFakeExecutor(errResponse("exec: \"claude\": executable file not found in $PATH"))
	m := newTestManager(t, fe)

	_, err := m.LaunchSession(context.Background(), "prompt")
	if err == nil {
		t.Fatal("a missing binary must surface as an error, not a silent success")
	}
	if !strings.Contains(err.Error(), "claude invocation failed") {
		t.Errorf("unexpected error text: %v", err)
	}
}

func TestContinueSessionPassesResumeID(t *testing.T) {
	fe := newFakeExecutor(okResponse(jsonOK))
	m := newTestManager(t, fe)

	launched, err := m.LaunchSession(context.Background(), "first")
	if err != nil {
		t.Fatalf("LaunchSession failed: %v", err)
	}

	if _, err := m.ContinueSession(context.Background(), launched.SessionID, "second"); err != nil {
		t.Fatalf("ContinueSession failed: %v", err)
	}

	call := fe.lastCall()
	if !hasArgPair(call.args, "--resume", "cli-abc") {
		t.Errorf("continuation must resume the CLI session id: %v", call.args)
	}
	if !hasArgPair(call.args, "-p", "second") {
		t.Errorf("continuation prompt not passed: %v", call.args)
	}
}

func TestContinueSessionAccumulatesCost(t *testing.T) {
	fe := newFakeExecutor(okResponse(jsonOK))
	m := newTestManager(t, fe)

	launched, _ := m.LaunchSession(context.Background(), "first")
	m.ContinueSession(context.Background(), launched.SessionID, "second")

	md, _ := m.loadMetadata(launched.SessionID)
	if md.CostUSD < 0.059 || md.CostUSD > 0.061 {
		t.Errorf("cost should accumulate across turns, got %v", md.CostUSD)
	}
}

func TestContinueSessionValidation(t *testing.T) {
	fe := newFakeExecutor(okResponse(jsonOK))
	m := newTestManager(t, fe)

	if _, err := m.ContinueSession(context.Background(), "", "prompt"); err == nil {
		t.Error("empty session id must be rejected")
	}
	if _, err := m.ContinueSession(context.Background(), "sess", ""); err == nil {
		t.Error("empty prompt must be rejected")
	}
	if _, err := m.ContinueSession(context.Background(), "missing", "prompt"); err == nil {
		t.Error("unknown session must be rejected")
	}
}

func TestDetectFailureAfterQuota(t *testing.T) {
	fe := newFakeExecutor(exitResponse(1, "", "rate limit exceeded"))
	m := newTestManager(t, fe)

	if _, err := m.LaunchSession(context.Background(), "prompt"); err != nil {
		t.Fatalf("LaunchSession failed: %v", err)
	}

	ft, err := m.DetectFailure(context.Background())
	if err != nil {
		t.Fatalf("DetectFailure failed: %v", err)
	}
	if ft != interfaces.FailureTypeQuota {
		t.Errorf("FailureType: got %q, want %q", ft, interfaces.FailureTypeQuota)
	}
}

func TestModelOmittedWhenUnset(t *testing.T) {
	fe := newFakeExecutor(okResponse(jsonOK))
	m, err := New(t.TempDir(), "", 1, 300, WithExecutor(fe))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if _, err := m.LaunchSession(context.Background(), "prompt"); err != nil {
		t.Fatalf("LaunchSession failed: %v", err)
	}

	if hasArg(fe.lastCall().args, "--model") {
		t.Errorf("--model must be omitted when no model configured: %v", fe.lastCall().args)
	}
}
