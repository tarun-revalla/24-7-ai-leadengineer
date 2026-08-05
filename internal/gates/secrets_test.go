package gates

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic fixtures proving detection works, not real credentials. Each is
// assembled from split literals so no single contiguous match exists in this
// file's own committed text.
//
// This is not just about this package's own findSecrets — GitHub's push
// protection (a wholly separate scanner) rejected an earlier version of this
// file for exactly this reason: nothing in a text file distinguishes "test
// fixture" from "someone pasted a real key" except precisely this kind of
// construction. Go folds the concatenation at compile time, so the assembled
// value is still the complete matching string every test below exercises;
// only the source bytes never spell it out contiguously.
const (
	fakeAWSKey      = "AKIA" + "ABCDEFGHIJKLMNOP"
	fakeAWSTempKey  = "ASIA" + "ABCDEFGHIJKLMNOP"
	fakeGitHubToken = "ghp_" + "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ab"
	fakeSlackToken  = "xoxb-" + "1234567890-abcdefghijklmnop"
	fakeGoogleKey   = "AIza" + "SyABCDEFGHIJKLMNOPQRSTUVWXYZ0123456"
	fakeStripeKey   = "sk_live_" + "ABCDEFGHIJKLMNOPQRSTUVWX"
	fakeRSAKeyBlock = "-----BEGIN RSA " + "PRIVATE KEY-----"
	fakePEMBlock    = "-----BEGIN " + "PRIVATE KEY-----"
)

func TestFindSecretsDetectsKnownFormats(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"AWS access key", `key := "` + fakeAWSKey + `"`, "AWS access key ID"},
		{"AWS temp access key", `key := "` + fakeAWSTempKey + `"`, "AWS access key ID"},
		{"GitHub PAT", `token := "` + fakeGitHubToken + `"`, "GitHub token"},
		{"Slack bot token", `token := "` + fakeSlackToken + `"`, "Slack token"},
		{"Google API key", `key := "` + fakeGoogleKey + `"`, "Google API key"},
		{"Stripe live key", `key := "` + fakeStripeKey + `"`, "Stripe live key"},
		{"RSA private key", fakeRSAKeyBlock, "private key block"},
		{"generic private key", fakePEMBlock, "private key block"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := findSecrets("example.go", []byte(tt.line))
			if len(findings) == 0 {
				t.Fatalf("expected a finding for %q, got none", tt.line)
			}
			if !strings.Contains(findings[0], tt.want) {
				t.Errorf("finding %q does not mention %q", findings[0], tt.want)
			}
		})
	}
}

func TestFindSecretsIgnoresOrdinaryCode(t *testing.T) {
	ordinary := []string{
		"func main() {}",
		`fmt.Println("hello, world")`,
		"const maxRetries = 3",
		`token := os.Getenv("GITHUB_TOKEN")`,
		"AKIA is a prefix used by AWS access keys",
	}

	for _, line := range ordinary {
		if findings := findSecrets("example.go", []byte(line)); len(findings) != 0 {
			t.Errorf("false positive on %q: %v", line, findings)
		}
	}
}

// AWS's own documentation example must not fail every project that quotes it.
func TestFindSecretsExemptsKnownDocsExample(t *testing.T) {
	line := `// Example: ` + "AKIAIOSFODNN7EXAMPLE" + ` is AWS's placeholder access key`
	if findings := findSecrets("README.md", []byte(line)); len(findings) != 0 {
		t.Errorf("the documented AWS example key should be exempt, got %v", findings)
	}
}

func TestFindSecretsReportsFileAndLineNotTheSecret(t *testing.T) {
	data := "line one\nline two\nkey := \"" + fakeAWSKey + "\"\n"
	findings := findSecrets("config.go", []byte(data))

	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	if !strings.HasPrefix(findings[0], "config.go:3:") {
		t.Errorf("finding should cite file:line, got %q", findings[0])
	}
	if strings.Contains(findings[0], fakeAWSKey) {
		t.Errorf("finding must not echo the matched secret back out: %q", findings[0])
	}
}

// The suppression mechanism itself needs its own test, independent of the
// fixtures above that happen to use it: a marked line must be skipped, and an
// unmarked line on either side of it must still be checked.
func TestFindSecretsRespectsIgnoreMarker(t *testing.T) {
	data := "key := \"" + fakeAWSKey + "\" // secretscan:ignore\n" +
		"key2 := \"" + fakeAWSTempKey + "\"\n"

	findings := findSecrets("example.go", []byte(data))
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want exactly 1 (the unmarked line): %v", len(findings), findings)
	}
	if !strings.HasPrefix(findings[0], "example.go:2:") {
		t.Errorf("the surviving finding should be the second, unmarked line: %q", findings[0])
	}
}

func TestLooksBinaryDetectsNullByte(t *testing.T) {
	if !looksBinary([]byte{0x00, 0x01, 0x02}) {
		t.Error("data containing a null byte should be treated as binary")
	}
	if looksBinary([]byte("perfectly ordinary text")) {
		t.Error("ordinary text should not be treated as binary")
	}
}

func secretFixture(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s failed: %v", name, err)
		}
	}

	return dir
}

func TestSecretScanPassesOnCleanProject(t *testing.T) {
	dir := secretFixture(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})

	res := SecretScan{}.Run(context.Background(), dir)
	if res.Status != StatusPassed {
		t.Fatalf("got %s (%s), want passed\n%s", res.Status, res.Detail, res.Output)
	}
}

func TestSecretScanFailsOnLeakedCredential(t *testing.T) {
	dir := secretFixture(t, map[string]string{
		"config.go": "package main\n\nconst key = \"" + fakeAWSKey + "\"\n",
	})

	res := SecretScan{}.Run(context.Background(), dir)
	if res.Status != StatusFailed {
		t.Fatalf("got %s, want failed", res.Status)
	}
	if !strings.Contains(res.Output, "config.go") {
		t.Errorf("output should name the offending file: %q", res.Output)
	}
}

// Untracked-but-ignored files (build output, vendored deps) are not something
// a commit could include, so scanning them would just be noise.
func TestSecretScanIgnoresGitignoredFiles(t *testing.T) {
	dir := secretFixture(t, map[string]string{
		".gitignore": "ignored.go\n",
		"main.go":    "package main\n\nfunc main() {}\n",
		"ignored.go": "package main\n\nconst key = \"" + fakeAWSKey + "\"\n",
	})

	res := SecretScan{}.Run(context.Background(), dir)
	if res.Status != StatusPassed {
		t.Fatalf("got %s (%s), want passed — ignored files must not be scanned\n%s",
			res.Status, res.Detail, res.Output)
	}
}

func TestSecretScanCoversUntrackedFiles(t *testing.T) {
	// Not yet `git add`-ed, but not ignored either — still part of what a
	// commit could pick up.
	dir := secretFixture(t, map[string]string{
		"new.go": "package main\n\nconst key = \"" + fakeAWSKey + "\"\n",
	})

	res := SecretScan{}.Run(context.Background(), dir)
	if res.Status != StatusFailed {
		t.Fatalf("got %s, want failed — untracked files must still be scanned", res.Status)
	}
}

func TestSecretScanSkipsOversizedFiles(t *testing.T) {
	huge := strings.Repeat("x", maxSecretScanFileSize+1)
	dir := secretFixture(t, map[string]string{
		"huge.txt": huge + "\n" + fakeAWSKey + "\n",
	})

	res := SecretScan{}.Run(context.Background(), dir)
	if res.Status != StatusPassed {
		t.Fatalf("oversized files should be skipped, not scanned: got %s\n%s", res.Status, res.Output)
	}
}

func TestGoSecurityRequiredIsFalse(t *testing.T) {
	if (GoSecurity{}).Required() {
		t.Error("gosec is a third-party tool a project may not have installed; it must be optional")
	}
}

func TestGoSecuritySkipsWhenNotInstalled(t *testing.T) {
	// This environment does not have gosec installed; verify the gate
	// degrades to a labelled skip rather than a false pass.
	dir := secretFixture(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})

	res := GoSecurity{}.Run(context.Background(), dir)
	if res.Status == StatusSkipped && !strings.Contains(res.Detail, "not installed") {
		t.Errorf("a skip must say why: %q", res.Detail)
	}
}
