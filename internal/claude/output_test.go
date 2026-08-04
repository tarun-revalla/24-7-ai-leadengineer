package claude

import "testing"

func TestParseCLIResponse(t *testing.T) {
	tests := []struct {
		name    string
		stdout  string
		wantOK  bool
		wantID  string
		wantErr bool
	}{
		{
			name:   "well formed",
			stdout: `{"session_id":"abc","result":"hi","is_error":false,"num_turns":1}`,
			wantOK: true,
			wantID: "abc",
		},
		{
			name:    "error flag set",
			stdout:  `{"session_id":"abc","result":"bad","is_error":true}`,
			wantOK:  true,
			wantID:  "abc",
			wantErr: true,
		},
		{
			name:   "leading whitespace tolerated",
			stdout: "\n  {\"session_id\":\"abc\"}\n",
			wantOK: true,
			wantID: "abc",
		},
		{
			name:   "unknown fields ignored",
			stdout: `{"session_id":"abc","future_field":123}`,
			wantOK: true,
			wantID: "abc",
		},
		{name: "plain text", stdout: "just a reply", wantOK: false},
		{name: "empty", stdout: "", wantOK: false},
		{name: "truncated json", stdout: `{"session_id":"abc"`, wantOK: false},
		{name: "json array not object", stdout: `[1,2,3]`, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, ok := parseCLIResponse(tt.stdout)
			if ok != tt.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if resp.SessionID != tt.wantID {
				t.Errorf("SessionID: got %q, want %q", resp.SessionID, tt.wantID)
			}
			if resp.IsError != tt.wantErr {
				t.Errorf("IsError: got %v, want %v", resp.IsError, tt.wantErr)
			}
		})
	}
}

func TestIsQuotaExhaustion(t *testing.T) {
	positive := []string{
		"Claude usage limit reached",
		"CLAUDE USAGE LIMIT REACHED",
		"error: rate limit exceeded",
		"rate_limit_error",
		"quota exceeded for this account",
		"HTTP 429 Too Many Requests",
	}
	for _, s := range positive {
		if !isQuotaExhaustion(s) {
			t.Errorf("expected quota detection for %q", s)
		}
	}

	negative := []string{
		"completed successfully",
		"file not found",
		"",
		"the limit of the function as x approaches zero",
	}
	for _, s := range negative {
		if isQuotaExhaustion(s) {
			t.Errorf("false positive quota detection for %q", s)
		}
	}
}

func TestIsNetworkFailure(t *testing.T) {
	positive := []string{
		"dial tcp: connection refused",
		"read: connection reset by peer",
		"no such host",
		"TLS handshake timeout",
	}
	for _, s := range positive {
		if !isNetworkFailure(s) {
			t.Errorf("expected network detection for %q", s)
		}
	}

	if isNetworkFailure("everything is fine") {
		t.Error("false positive network detection")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", "x", "y"); got != "x" {
		t.Errorf("got %q, want x", got)
	}
	if got := firstNonEmpty("", "   "); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := firstNonEmpty(); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
