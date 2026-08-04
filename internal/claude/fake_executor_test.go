package claude

import (
	"context"
	"errors"
	"sync"
)

// fakeExecutor is a scripted CommandExecutor for tests. Each call consumes the
// next queued response; once exhausted, the final response repeats.
type fakeExecutor struct {
	mu        sync.Mutex
	responses []fakeResponse
	calls     []fakeCall
	callCount int
}

type fakeResponse struct {
	result *ExecResult
	err    error
}

type fakeCall struct {
	name    string
	args    []string
	workDir string
}

func newFakeExecutor(responses ...fakeResponse) *fakeExecutor {
	return &fakeExecutor{responses: responses}
}

func okResponse(stdout string) fakeResponse {
	return fakeResponse{result: &ExecResult{Stdout: stdout, ExitCode: 0}}
}

func exitResponse(code int, stdout, stderr string) fakeResponse {
	return fakeResponse{result: &ExecResult{Stdout: stdout, Stderr: stderr, ExitCode: code}}
}

func timeoutResponse() fakeResponse {
	return fakeResponse{result: &ExecResult{ExitCode: -1, TimedOut: true}}
}

func errResponse(msg string) fakeResponse {
	return fakeResponse{err: errors.New(msg)}
}

func (f *fakeExecutor) Execute(ctx context.Context, name string, args []string, workDir string) (*ExecResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, fakeCall{name: name, args: args, workDir: workDir})

	idx := f.callCount
	f.callCount++

	if len(f.responses) == 0 {
		return &ExecResult{}, nil
	}
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}

	r := f.responses[idx]
	return r.result, r.err
}

func (f *fakeExecutor) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCount
}

func (f *fakeExecutor) lastCall() fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return fakeCall{}
	}
	return f.calls[len(f.calls)-1]
}

// hasArgPair reports whether args contains flag immediately followed by value.
func hasArgPair(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
