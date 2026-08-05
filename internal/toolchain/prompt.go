package toolchain

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tarun-revalla/24-7-ai-leadengineer/pkg/interfaces"
)

// BuildDetectPrompt asks for the commands that verify this project.
//
// The instruction that carries the most weight is the one against inventing
// commands. A plausible-looking `npm test` in a repository that has no test
// script produces a gate that fails every task for a reason that has nothing
// to do with the change — worse than having no gate, because it looks like a
// real failure. So the prompt insists on evidence and explicitly permits a
// short list.
func BuildDetectPrompt(evidence string) string {
	var b strings.Builder

	b.WriteString(`Work out how this project verifies itself, and report the commands.

Do not modify any files. Your entire response is the report.

`)
	b.WriteString(evidence)

	b.WriteString(`
## What to report

The commands this project's own contributors would run to check that a change
is sound: build, tests, type check, lint, formatting check — whichever of
these the project actually has.

Base every command on evidence above. A Makefile target, a package.json
script, a CI workflow step, or a documented command in CONTRIBUTING all count.
The conventional command for the language counts too, when the manifest shows
the standard layout and nothing overrides it.

Do not invent commands. A command that does not exist produces a gate that
fails every task for a reason unrelated to the change, which is worse than
having no gate at all — it looks like a real failure. If this project has no
lint step, omit lint. Four solid gates beat seven speculative ones.

Prefer the non-interactive, non-watching form: a test runner that waits for
input never returns, and the task stalls until it times out.

Where a project's CI runs a check, prefer exactly what CI runs. That is the
bar the project already holds itself to.

## Rules for each command

- Give argv as a list, not a shell string. Nothing runs through a shell.
- The command runs from the repository root.
- "required": true means a missing tool blocks the commit. Use it for the
  build and the tests. Use false for anything a contributor might not have
  installed locally, such as an optional linter — those record a visible skip
  instead of blocking.
- "expectation" is one sentence, addressed to whoever implements a change,
  saying what this gate asks of them. It goes into their instructions.

Do not include a secret or credential scan. One always runs, separately.

## Response format

Respond with a single JSON object and nothing else:

` + "```json" + `
{
  "language": "rust",
  "gates": [
    {
      "name": "build",
      "command": ["cargo", "build", "--all-targets"],
      "required": true,
      "expectation": "compile with cargo build --all-targets"
    },
    {
      "name": "test",
      "command": ["cargo", "test", "--all-features"],
      "required": true,
      "expectation": "pass cargo test --all-features"
    },
    {
      "name": "lint",
      "command": ["cargo", "clippy", "--all-targets", "--", "-D", "warnings"],
      "required": false,
      "expectation": "pass clippy with no warnings"
    }
  ]
}
` + "```" + `

Order the gates cheapest first, so an obvious break is reported in seconds
rather than after a full test run.
`)

	return b.String()
}

// ErrUnparseable reports that the detection response could not be read.
//
// Distinct from "the project has no toolchain": a failed detection must not
// silently become an empty gate set, which would let every change through
// unverified.
var ErrUnparseable = fmt.Errorf("the detection response could not be parsed")

type detectPayload struct {
	Language string `json:"language"`
	Gates    []struct {
		Name          string   `json:"name"`
		Command       []string `json:"command"`
		Required      bool     `json:"required"`
		Expectation   string   `json:"expectation"`
		SkipIfMissing string   `json:"skipIfMissing"`
	} `json:"gates"`
}

// ParseToolchain reads a detection response.
func ParseToolchain(output string) (*interfaces.Toolchain, error) {
	raw, ok := extractJSON(output)
	if !ok {
		return nil, fmt.Errorf("%w: no JSON object found", ErrUnparseable)
	}

	var payload detectPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnparseable, err)
	}

	t := &interfaces.Toolchain{Language: strings.TrimSpace(payload.Language)}

	for _, g := range payload.Gates {
		name := strings.TrimSpace(g.Name)
		if name == "" || len(g.Command) == 0 {
			// A gate that cannot be run is not a gate. Dropping it is right;
			// keeping it would fail every task on a malformed declaration.
			continue
		}
		t.Gates = append(t.Gates, interfaces.ToolchainGate{
			Name:          name,
			Command:       g.Command,
			Required:      g.Required,
			Expectation:   strings.TrimSpace(g.Expectation),
			SkipIfMissing: strings.TrimSpace(g.SkipIfMissing),
		})
	}

	if len(t.Gates) == 0 {
		// An empty set would verify nothing while reporting success. A
		// detection that found no way to check this project has to say so.
		return nil, fmt.Errorf("%w: no runnable gates were reported", ErrUnparseable)
	}

	return t, nil
}

// extractJSON pulls the JSON object out of a response that may also carry
// prose or code fences, skipping braces inside string literals so a command
// containing one does not throw off the balance.
func extractJSON(output string) (string, bool) {
	start := strings.IndexByte(output, '{')
	if start < 0 {
		return "", false
	}

	depth, inString, escaped := 0, false, false
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

// Render formats a toolchain for a human reader.
func Render(t *interfaces.Toolchain) string {
	var b strings.Builder

	if t.Language != "" {
		fmt.Fprintf(&b, "Detected: %s\n\n", t.Language)
	}

	fmt.Fprintf(&b, "Gates (%d)\n", len(t.Gates))
	for _, g := range t.Gates {
		requirement := "optional"
		if g.Required {
			requirement = "required"
		}
		fmt.Fprintf(&b, "  %-12s %s  [%s]\n", g.Name, strings.Join(g.Command, " "), requirement)
	}

	b.WriteString("\nA secret scan always runs in addition to these.\n")

	return b.String()
}
