package gates

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// maxSecretScanFileSize bounds how large a file the scanner reads into
// memory. Anything larger is almost certainly a binary or generated
// artifact, not hand-written source carrying a credential.
const maxSecretScanFileSize = 2 << 20 // 2 MiB

// SecretScan checks tracked and untracked-but-not-ignored files for
// well-known credential formats before they can be committed.
//
// Unlike GolangCILint or GoSecurity, this has no external dependency — it
// always runs. gosec and govulncheck depend on tooling that may not be
// installed in every deployment environment; a credential accidentally
// committed to a public repository is exactly the kind of failure this
// system exists to prevent, so catching it cannot be optional.
type SecretScan struct{}

func (SecretScan) Name() string   { return "secrets" }
func (SecretScan) Required() bool { return true }

func (g SecretScan) Run(ctx context.Context, dir string) Result {
	started := time.Now()

	if !toolAvailable("git") {
		return missing(g, "git", started)
	}

	files, err := scannableFiles(ctx, dir)
	if err != nil {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: fmt.Sprintf("failed to list files: %v", err),
		}
	}

	var findings []string
	for _, rel := range files {
		path := filepath.Join(dir, rel)

		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxSecretScanFileSize {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil || looksBinary(data) {
			continue
		}

		findings = append(findings, findSecrets(rel, data)...)
	}

	if len(findings) > 0 {
		return Result{
			Gate: g.Name(), Status: StatusFailed, Duration: time.Since(started),
			Detail: fmt.Sprintf("%d potential secret(s) found", len(findings)),
			Output: strings.Join(findings, "\n"),
		}
	}

	return Result{Gate: g.Name(), Status: StatusPassed, Duration: time.Since(started)}
}

// scannableFiles lists tracked files plus untracked files git would not
// ignore — the same set `git add .` would pick up, so the scan covers
// exactly what a commit could actually include.
func scannableFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := command{"git", []string{"ls-files", "--cached", "--others", "--exclude-standard"}}.run(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(out))
	}
	return nonEmptyLines(out), nil
}

// looksBinary applies the conventional null-byte heuristic: text files do not
// legitimately contain NUL, binaries usually do within their first bytes.
func looksBinary(data []byte) bool {
	probe := data
	if len(probe) > 8000 {
		probe = probe[:8000]
	}
	return bytes.IndexByte(probe, 0) != -1
}

// secretPattern is one credential format worth flagging.
type secretPattern struct {
	name    string
	pattern *regexp.Regexp
}

// secretPatterns covers structurally distinctive credential formats with a
// low false-positive rate — each is a fixed, documented prefix or block
// format that essentially never occurs by chance in ordinary source or
// prose. Deliberately excludes anything that would need entropy heuristics
// (a bare "looks like base64" check on generic strings): those flag so much
// ordinary code and test data that the gate would be routinely ignored,
// which is worse than not having it.
var secretPatterns = []secretPattern{
	{"AWS access key ID", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"GitHub token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[baprs]-[0-9A-Za-z-]{10,}\b`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"Stripe live key", regexp.MustCompile(`\bsk_live_[0-9A-Za-z]{24,}\b`)},
	{"private key block", regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP )?PRIVATE KEY-----`)},
}

// exemptSecrets are known-fake values that legitimately appear in real
// source: documentation examples an author might paste into a comment or
// README. AKIAIOSFODNN7EXAMPLE specifically is AWS's own canonical
// placeholder, used throughout their official docs.
var exemptSecrets = map[string]bool{
	"AKIAIOSFODNN7EXAMPLE": true,
}

// ignoreMarker suppresses a match on the line that carries it — the same
// convention every mainstream secret scanner offers, because it is a real
// need, not a hypothetical one: a test asserting that a credential format is
// detected has to contain a string in that exact format. This package's own
// tests use it for precisely that reason.
const ignoreMarker = "secretscan:ignore"

// findSecrets returns one description per match, formatted for a human to
// act on: file, line, and which pattern fired — never the matched text
// itself, so a real finding is not echoed back into logs or terminal
// scrollback that may be less protected than the source it came from.
func findSecrets(relPath string, data []byte) []string {
	var findings []string

	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.Contains(line, ignoreMarker) {
			continue
		}
		for _, p := range secretPatterns {
			match := p.pattern.FindString(line)
			if match == "" || exemptSecrets[match] {
				continue
			}
			findings = append(findings, fmt.Sprintf("%s:%d: possible %s", relPath, i+1, p.name))
		}
	}

	return findings
}
