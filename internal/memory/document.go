package memory

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// delimiter marks the bounds of a document's YAML front matter.
const delimiter = "---"

// ErrNoFrontMatter reports that a document carries no structured header.
// It is returned rather than an empty value so a hand-edited or malformed
// file is distinguishable from one that genuinely holds no state.
var ErrNoFrontMatter = errors.New("document has no front matter")

// document is a memory file: a YAML header the system reads and writes, and a
// Markdown body written for humans and supplied to Claude as context.
//
// Both live in one file so there is a single place to look for any given
// concept, and so updating machine state cannot silently discard the prose
// explaining it.
type document struct {
	frontMatter string
	body        string
}

// parseDocument splits raw file content into front matter and body.
//
// The expected layout is:
//
//	---
//	key: value
//	---
//	# Heading
//
//	Prose.
func parseDocument(raw string) (*document, error) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	trimmed := strings.TrimLeft(normalized, "\n")

	if !strings.HasPrefix(trimmed, delimiter+"\n") {
		return &document{body: normalized}, ErrNoFrontMatter
	}

	rest := trimmed[len(delimiter)+1:]

	end := strings.Index(rest, "\n"+delimiter)
	if end < 0 {
		// An opening delimiter with no closing one means the file was
		// truncated or hand-edited; treating the remainder as YAML would
		// silently reinterpret prose as state.
		return &document{body: normalized}, fmt.Errorf("%w: unterminated front matter", ErrNoFrontMatter)
	}

	fm := rest[:end]
	body := rest[end+len("\n"+delimiter):]
	body = strings.TrimPrefix(body, "\n")

	return &document{frontMatter: fm, body: body}, nil
}

// decodeInto unmarshals the document's front matter into out.
func (d *document) decodeInto(out any) error {
	if strings.TrimSpace(d.frontMatter) == "" {
		return ErrNoFrontMatter
	}

	if err := yaml.Unmarshal([]byte(d.frontMatter), out); err != nil {
		return fmt.Errorf("failed to parse front matter: %w", err)
	}

	return nil
}

// render serialises meta as front matter above the preserved body.
func renderDocument(meta any, body string) (string, error) {
	encoded, err := yaml.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("failed to encode front matter: %w", err)
	}

	var b strings.Builder
	b.WriteString(delimiter)
	b.WriteString("\n")
	b.Write(encoded)
	if !strings.HasSuffix(string(encoded), "\n") {
		b.WriteString("\n")
	}
	b.WriteString(delimiter)
	b.WriteString("\n")

	if body != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimLeft(body, "\n"))
	}

	return b.String(), nil
}
