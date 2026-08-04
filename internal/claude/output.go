package claude

import (
	"encoding/json"
	"strings"
)

// CLIResponse is the envelope emitted by the Claude Code CLI under
// --output-format json. Fields are optional: the CLI's shape varies across
// versions, so every field is treated as best-effort and absence is not an
// error. Raw output is always retained by the caller as the source of truth.
type CLIResponse struct {
	SessionID  string  `json:"session_id"`
	Result     string  `json:"result"`
	IsError    bool    `json:"is_error"`
	NumTurns   int     `json:"num_turns"`
	DurationMS int64   `json:"duration_ms"`
	TotalCost  float64 `json:"total_cost_usd"`
	Subtype    string  `json:"subtype"`
}

// parseCLIResponse extracts the JSON envelope from CLI stdout. When stdout is
// not JSON — a plain-text build, a crash, a truncated stream — ok is false and
// the caller falls back to treating stdout as opaque text.
func parseCLIResponse(stdout string) (*CLIResponse, bool) {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" || trimmed[0] != '{' {
		return nil, false
	}

	resp := &CLIResponse{}
	if err := json.Unmarshal([]byte(trimmed), resp); err != nil {
		return nil, false
	}

	return resp, true
}

// quotaMarkers are substrings that indicate the account has hit a usage or
// rate limit. Matching is case-insensitive and deliberately broad: a false
// positive costs a pause and retry, a false negative burns the run against a
// limit that will not clear.
var quotaMarkers = []string{
	"usage limit reached",
	"rate limit",
	"rate_limit",
	"quota exceeded",
	"too many requests",
	"429",
}

// networkMarkers indicate a transport-level failure worth retrying.
var networkMarkers = []string{
	"connection refused",
	"connection reset",
	"no such host",
	"network is unreachable",
	"tls handshake",
	"eof",
}

// isQuotaExhaustion reports whether combined CLI output indicates a usage limit.
func isQuotaExhaustion(output string) bool {
	return containsAnyFold(output, quotaMarkers)
}

// isNetworkFailure reports whether combined CLI output indicates a transport error.
func isNetworkFailure(output string) bool {
	return containsAnyFold(output, networkMarkers)
}

func containsAnyFold(haystack string, needles []string) bool {
	lowered := strings.ToLower(haystack)
	for _, n := range needles {
		if strings.Contains(lowered, n) {
			return true
		}
	}
	return false
}
