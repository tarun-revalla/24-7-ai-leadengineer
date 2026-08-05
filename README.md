# 24-7 AI Lead Engineer

An autonomous software engineering system that uses Claude Code as its execution engine.

It reads a backlog, implements the highest-priority task, verifies the result against quality
gates, reviews it from six perspectives, and commits only if everything passes — then does it
again. State lives in the repository, so the process can be killed at any point and resumed.

## What it actually does

The system decides *what* to work on and *whether the result is acceptable*. Claude decides
*how* to implement it. That split is deliberate: the parts that must be deterministic and
auditable — task selection, gate enforcement, commit policy, recovery — are ordinary Go code
with tests, not prompts.

One task runs through this pipeline:

```
select → implement → verify → repair → review → revise → commit → record
             │          │        │        │        │         │
          Claude      gates    Claude   Claude   Claude    git
```

Three rules shape everything else:

1. **Nothing is committed until every required gate passes.** A gate that could not run is
   never reported as passing.
2. **The working tree must be clean before a task starts.** Otherwise a commit would sweep in
   unrelated work under a message describing something else.
3. **A review that cannot be read is not an approval.** "The reviewer did not answer" and
   "the reviewer approved" must never collapse into the same outcome.

## Install

Requires Go 1.21+ and Git 2.30+. The Claude Code CLI must be on `PATH` for anything that runs
tasks; the inspection commands work without it.

```bash
go build -o leadengineer ./cmd/leadengineer
```

## Use

```bash
# Create .ai/ with the memory documents the system reads and writes.
leadengineer init

# Run backlog tasks, highest priority first.
leadengineer start
leadengineer start --once            # a single task
leadengineer start --max-tasks 5

# What is the system doing, and what state is the repository in?
leadengineer status

# Was work interrupted by a crash or a usage limit? What should happen next?
leadengineer recover

# Saved state.
leadengineer checkpoint list
leadengineer checkpoint show <id>
leadengineer checkpoint verify
leadengineer checkpoint prune --keep 7

# Claude usage cooldown.
leadengineer quota status
leadengineer quota clear

# Work out how this project verifies itself and record it.
leadengineer detect

# Effective configuration, after defaults, file and environment are merged.
leadengineer config show
```

Every command takes `-C <path>` to operate on a repository other than the current directory.

## Project memory

State lives in `.ai/` inside the repository being worked on, as Markdown documents with YAML
front matter. The front matter is the machine-readable state; the body is for a human, and is
preserved across writes. One file serves both readers, so the system's state is reviewable in a
pull request like anything else.

| File | Holds |
| --- | --- |
| `PROJECT.md` | Name, purpose, and constraints that apply to every task |
| `ROADMAP.md` | Goals and milestones |
| `BACKLOG.md` | Tasks with priority and status |
| `CURRENT.md` | The task in flight, its stage and progress |
| `CHANGELOG.md` | Completed work, newest first |
| `TOOLCHAIN.md` | The commands that verify this project |
| `DECISIONS.md` | Architecture decisions |
| `checkpoints/` | Compressed, checksummed recovery snapshots |
| `logs/` | Structured JSON logs |

Writes are atomic — a temporary file and a rename — so a crash mid-write cannot leave a torn
document.

## Quality gates

Gates run against the whole project after Claude implements a task. Failures are handed back
with the tools' own output, and the change is re-verified after every repair.

**Any language works.** Nothing about a particular language is compiled into the binary. A gate
is a name, a command, and whether a missing tool blocks — so Rust, Elixir, Zig, a Makefile
target or a polyglot monorepo are all first-class. Run `leadengineer detect` and it reads the
repository's manifests, build files and CI config and writes the commands to `.ai/TOOLCHAIN.md`:

```yaml
---
language: rust
gates:
    - name: build
      command: [cargo, build, --all-targets]
      required: true
      expectation: compile with cargo build
    - name: test
      command: [cargo, test, --all-features]
      required: true
    - name: lint
      command: [cargo, clippy, --all-targets, --, -D, warnings]
      required: false
---
```

Edit it by hand any time — a project's own contributors usually know the answer already. A
declared toolchain beats the built-in sets below, which are just a convenience for the two
most common cases.

Commands are argv lists, not shell strings, so an argument containing spaces or
metacharacters cannot change what executes.

### Built-in sets

Used when no toolchain is declared, chosen from `project.type`.

**Go** (`project.type: go`)

| Gate | Runs | Required |
| --- | --- | --- |
| `format` | `gofmt -l` | yes |
| `secrets` | Credential-format scan over everything a commit could include | yes |
| `build` | `go build ./...` | yes |
| `vet` | `go vet ./...` | yes |
| `test` | `go test ./... -race -cover`, against a coverage threshold | yes |
| `lint` | `golangci-lint run` | no — skipped if not installed |
| `security` | `gosec ./...` | no — skipped if not installed |

**JavaScript / TypeScript** (`project.type: node`, `javascript` or `typescript`)

| Gate | Runs | Required |
| --- | --- | --- |
| `secrets` | Same scan — it reads files and needs no toolchain | yes |
| `install` | `npm ci` / `pnpm install --frozen-lockfile` / `yarn` / `bun`, chosen by lockfile | yes |
| `format` | `format:check`, `format-check` or `prettier:check` script | no — skipped if undefined |
| `typecheck` | `typecheck`, `type-check` or `tsc` script | no — skipped if undefined |
| `build` | `build` script | no — skipped if undefined |
| `test` | `test:coverage`, `coverage` or `test` script | yes |
| `lint` | `lint` script | no — skipped if undefined |

Node has no universal toolchain, so these read `package.json` and run the scripts a project
actually defines rather than assuming commands that may not exist. A script that is not
defined is a labelled skip, not a silent pass — a library with no build step is a normal
project. A script that *is* defined and fails still fails the report: `Required()` only
governs what happens when there is nothing to run.

The package manager is chosen by lockfile, not by what happens to be installed. Running
`npm install` in a pnpm project rewrites the lockfile and can resolve different versions than
the ones the project was tested against.

Coverage is read from istanbul's `coverage/coverage-summary.json`, which jest, vitest, c8 and
nyc all emit under the `json-summary` reporter. Scraping console output instead would break on
a version bump. If no summary is written, tests still have to pass but the floor is reported as
unenforced rather than silently satisfied.

Required gates fail when their tooling is missing, because a check that did not execute proves
nothing. Optional gates record that they were skipped and why, so the gap is visible rather
than silent. An optional gate that *does* run and finds a problem still fails the report.

The secret scan has no external dependency, so it always runs: a credential committed to a
public repository is exactly the failure this system exists to prevent, and that cannot be
contingent on what happens to be installed. It matches structurally distinctive formats (AWS,
GitHub, Slack, Google, Stripe, PEM key blocks) rather than entropy heuristics, which flag
enough ordinary code that the gate would be routinely ignored.

## Self-review

After the gates pass and before anything is committed, the change is reviewed from six
perspectives in a single pass: **developer**, **reviewer**, **security**, **performance**,
**qa**, **documentation**.

One prompt rather than six calls — six calls cost six times the quota for six partial views of
the same diff, where one reviewer holding the whole change can see how a performance decision
created a security problem.

Critical and major findings block the commit and are sent back for revision, bounded by
`review.maxRevisions`. Minor findings are reported without blocking. Each revision is
re-verified by the gates before being reviewed again: a change that satisfies the reviewer but
breaks the build is not an improvement.

Set `review.enabled: false` to run on gates alone.

## Running without the project's tooling

Sometimes the toolchain cannot run where the system runs — no compiler, no dependencies, a
build that needs credentials the container does not have. Rather than being blocked entirely:

```bash
leadengineer start --inspect-only
```

No project tooling runs. Changes are checked by the review stage reading the diff, plus the
secret scan, which needs no toolchain. Review is forced on in this mode regardless of
`review.enabled` — with nothing else checking, disabling it too would make every task commit
unconditionally.

**This is a real reduction in assurance, not another route to the same one.** A model reading a
diff can catch a wrong algorithm, a missing error path, an injection, an off-by-one. It cannot
know the code compiles, that the tests pass, or that nothing else in the repository broke. Only
running the tooling establishes those.

So commits record what was actually checked:

```
WEB-014: Add rate limiting to the signup endpoint

Verified-by: secrets
```

against a normally verified one:

```
WEB-014: Add rate limiting to the signup endpoint

Verified-by: secrets, install, typecheck, build, test
Not-checked: lint
```

The trailer is what lets someone reviewing a week of unattended commits tell them apart from
the log alone. Set `quality.inspectionOnly: true` to make it the default for a project.

## Interruption and recovery

A task left at status `in-progress` is the one state the executor never leaves behind on any
normal exit — success ends `done`, any failure ends `blocked`. Finding one means the process
stopped abnormally.

`leadengineer recover` reports what was interrupted, whether the working tree is clean, and
which checkpoints are intact. It is **read-only**. If uncommitted changes are present, they are
left alone with instructions to review them — the system will not guess whether someone's
uncommitted work is worth keeping.

When Claude reports a usage limit, the run records a cooldown and stops. The cooldown persists,
so a process that restarts in the meantime still waits it out rather than burning attempts
against a limit that has not reset.

## Configuration

Defaults are built in; `config.yaml` in the project overrides them; environment variables
prefixed `LEADENG_` override both.

```yaml
project:
  type: "go"          # go | node | javascript | typescript — gate selection follows this

claude:
  model: "claude-opus-5"
  maxRetries: 3       # repair attempts per task
  permissionMode: "acceptEdits"

quality:
  minimumCoverage: 0.80

review:
  enabled: true
  maxRevisions: 2

tasks:
  autoCommit: true
```

See `configs/config.default.yaml` for every option with its rationale. Note that
`permissionMode` defaults to `acceptEdits`: Claude gets file access but not shell access,
because the quality gates run outside Claude and it never needs a shell to do its job.

## Layout

```
cmd/leadengineer/     CLI entry point
internal/
  app/                composition root — wires every subsystem
  cli/                cobra commands
  claude/             Claude Code CLI session management
  executor/           the task pipeline
  gates/              quality gates
  review/             multi-perspective self-review
  toolchain/          works out how a project verifies itself
  memory/             .ai/ document read/write
  checkpoint/         compressed, checksummed state snapshots
  recovery/           crash and interruption diagnosis
  quota/              usage-limit cooldowns
  git/                repository operations
  config/             layered configuration
  logging/            structured logging
  metrics/            counters, gauges, timings, Prometheus export
  atomicfile/         write-temp-then-rename
pkg/
  interfaces/         cross-subsystem contracts
  types/              shared types
tests/integration/    end-to-end tests
```

Each subsystem is reachable only through a narrow interface, so it can be tested against fakes
rather than the rest of the system.

## Development

```bash
go build ./...
go vet ./...
go test ./... -race -cover
golangci-lint run
gofmt -l .
```

CI runs all of these on every push. The race detector is not optional: this system is built
around long-lived concurrent work, and a race that only appears under load is exactly what a
run without `-race` passes straight through.

Test coverage is 80%+ across the subsystems that carry logic. Tests assert behaviour and its
failure paths, not that the code ran.

## Not built

Deliberately out of scope, and listed here rather than implied by silence: the web
dashboard, the plugin system, multi-project support, multi-agent orchestration, and hosted
integrations (GitHub, GitLab, Slack, Docker, Kubernetes). The `dashboard` and `plugins`
sections in the default config are placeholders for these; nothing reads them yet.

Metrics are collected and exportable in Prometheus format, but nothing serves them over HTTP
yet — `metrics.prometheusPort` is likewise unused.
