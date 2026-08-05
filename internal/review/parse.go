package review

import (
	"encoding/json"
	"fmt"
	"strings"
)

// reviewPayload is the JSON shape the prompt asks for. Every field is optional
// at the parsing layer; what is and is not acceptable to omit is decided in
// ParseReview, where a missing verdict can be distinguished from an explicit
// one.
type reviewPayload struct {
	Approved *bool            `json:"approved"`
	Summary  string           `json:"summary"`
	Findings []findingPayload `json:"findings"`
}

type findingPayload struct {
	Perspective string `json:"perspective"`
	Severity    string `json:"severity"`
	Location    string `json:"location"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
}

// ParseReview reads a reviewer's response into a verdict.
//
// It is strict about one thing only: a response with no readable verdict is an
// error, never an approval. Everything else is handled leniently, because the
// cost of rejecting sound work over a formatting quirk is real and the response
// is generated text, not a wire protocol.
func ParseReview(output string) (*Review, error) {
	raw, ok := extractJSON(output)
	if !ok {
		return nil, fmt.Errorf("%w: no JSON object found in the response", ErrUnparseable)
	}

	var payload reviewPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnparseable, err)
	}

	if payload.Approved == nil {
		// Absent means the reviewer never rendered a verdict. Defaulting either
		// way would invent one.
		return nil, fmt.Errorf("%w: the response has no \"approved\" verdict", ErrUnparseable)
	}

	review := &Review{
		Summary: strings.TrimSpace(payload.Summary),
		Raw:     output,
	}

	for _, f := range payload.Findings {
		desc := strings.TrimSpace(f.Description)
		if desc == "" {
			// A finding with nothing to say cannot be acted on, and counting it
			// would block a commit with an empty explanation.
			continue
		}
		review.Findings = append(review.Findings, Finding{
			Perspective: normalizePerspective(f.Perspective),
			Severity:    normalizeSeverity(f.Severity),
			Location:    strings.TrimSpace(f.Location),
			Description: desc,
			Suggestion:  strings.TrimSpace(f.Suggestion),
		})
	}

	// The verdict is reconciled against the findings rather than taken on
	// trust. A response claiming approval while reporting a critical problem is
	// self-contradictory, and the safe reading of a contradiction is the one
	// that does not commit.
	review.Approved = *payload.Approved && len(review.Blocking()) == 0

	return review, nil
}

// normalizeSeverity maps a reported severity onto the known set.
//
// An unrecognised value becomes major, not minor: the reviewer meant something
// by it, and treating an unknown severity as non-blocking would let any
// misspelling wave a problem through.
func normalizeSeverity(s string) Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical", "blocker", "high":
		return SeverityCritical
	case "minor", "nit", "low", "info", "suggestion":
		return SeverityMinor
	default:
		return SeverityMajor
	}
}

// normalizePerspective maps a reported perspective onto the known set, falling
// back to the general reviewer for anything unrecognised. Unlike severity this
// has no bearing on whether the commit proceeds, so an unknown value is merely
// labelled rather than escalated.
func normalizePerspective(p string) Perspective {
	candidate := Perspective(strings.ToLower(strings.TrimSpace(p)))
	for _, known := range Perspectives {
		if candidate == known {
			return known
		}
	}
	return PerspectiveReviewer
}

// extractJSON pulls the JSON object out of a response that may also contain
// prose, a fenced code block, or both.
//
// Scanning for the outermost balanced braces handles every shape seen in
// practice — a bare object, an object inside ```json fences, an object preceded
// by a sentence of explanation — without needing to guess which one arrived.
// Braces inside string literals are skipped so a description containing "{"
// does not throw off the balance.
func extractJSON(output string) (string, bool) {
	start := strings.IndexByte(output, '{')
	if start < 0 {
		return "", false
	}

	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(output); i++ {
		c := output[i]

		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}

		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return output[start : i+1], true
			}
		}
	}

	return "", false
}
