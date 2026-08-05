# Quickstart

Getting this running on a project of your own, start to finish.

## 1. Build it once

```bash
git clone https://github.com/tarun-revalla/24-7-ai-leadengineer
cd 24-7-ai-leadengineer
go build -o leadengineer ./cmd/leadengineer
sudo mv leadengineer /usr/local/bin/     # or anywhere on your PATH
```

Requires Go 1.21+, Git 2.30+, and the Claude Code CLI on `PATH` for anything that runs tasks.
The inspection commands (`status`, `recover`, `checkpoint`, `config`) work without it.

## 2. Point it at your project

Your project must be a git repository with a clean working tree.

```bash
cd ~/my-project
leadengineer init
```

That creates `.ai/` with the documents the system reads and writes. Safe to re-run — existing
documents are never overwritten.

## 3. Tell it how to verify your code

```bash
leadengineer detect
```

Claude reads your manifests, build files and CI config and writes `.ai/TOOLCHAIN.md`. Check
what it produced — you know your project better than it does:

```bash
cat .ai/TOOLCHAIN.md
```

```yaml
---
language: python
gates:
    - name: lint
      command: [ruff, check, .]
      required: false
      expectation: pass ruff with no findings
    - name: typecheck
      command: [mypy, src]
      required: false
      expectation: typecheck cleanly under mypy
    - name: test
      command: [pytest, -q]
      required: true
      expectation: pass the test suite
---
```

Edit it freely. Rules:

- `command` is argv, not a shell string. `["pytest", "-q"]`, not `"pytest -q"`.
- It runs from the repository root.
- `required: true` means a missing tool blocks the commit. Use it for build and tests.
- `required: false` records a visible skip instead — for tools a contributor might not have.
- `expectation` is one sentence telling the implementer what this asks of them. It goes into
  their instructions, so it is worth writing.

Prefer the non-interactive form of every command. A test runner in watch mode never returns.

**A secret scan always runs in addition to these.** You cannot switch it off.

## 4. Write your backlog

Edit the YAML front matter of `.ai/BACKLOG.md`:

```yaml
---
tasks:
    - id: API-001
      title: Add email/password signup
      description: |
        POST /api/signup taking {email, password}.
        Hash with argon2id. Reject duplicate emails with 409.
        Reject passwords under 12 characters with 400.
      priority: 1
      status: new
    - id: API-002
      title: Rate-limit the signup endpoint
      description: 10 requests per IP per hour, 429 with Retry-After when exceeded.
      priority: 2
      status: new
---
```

Priority 1 runs first. Statuses: `new`, `in-progress`, `done`, `blocked`. Anything not `done`
or `blocked` counts as open.

**Task descriptions are the highest-leverage thing you write.** "Add signup" produces
something plausible; the description above produces something you can review against a
specification. Include the acceptance criteria you would put in a ticket.

## 5. Run it

```bash
leadengineer start --once        # one task, to see what happens
leadengineer start               # drain the backlog, then exit
leadengineer start --watch       # keep going, picking up tasks as you add them
```

`--watch` is the unattended mode. Ctrl-C stops it cleanly.

```bash
nohup leadengineer start --watch > run.log 2>&1 &
```

## 6. Check on it

```bash
leadengineer status         # what is it doing, what state is the repo in
git log --oneline           # what it has actually done
leadengineer recover        # did something crash, and what should happen
```

Every commit records what verified it:

```
API-001: Add email/password signup

Verified-by: secrets, lint, typecheck, test
Not-checked: format
```

That trailer is how you audit a run you were not watching.

## When your toolchain cannot run

No compiler in the container, dependencies that need credentials, a build too heavy for the
box:

```bash
leadengineer start --watch --inspect-only
```

Nothing is executed. Changes are checked by the review stage reading the diff, plus the secret
scan. Those commits say so:

```
Verified-by: secrets
```

**This is genuinely weaker.** A model reading a diff catches wrong algorithms, missing error
paths, injections. It cannot know the code compiles or that tests pass. Use it when the
alternative is being blocked entirely, and review those commits yourself before shipping.

## Things worth knowing

**The working tree must be clean before each task.** Otherwise a commit would sweep in
whatever you had in progress under a message describing something else. Commit or stash first.

**A failed task is marked `blocked`, not retried.** It stays in the backlog with the reason
recorded on `.ai/CURRENT.md`. Fix the underlying problem, set the status back to `new`, and it
gets picked up again.

**Usage limits are handled.** The cooldown is recorded to disk, so a process that restarts
mid-cooldown still waits it out rather than burning attempts against a limit that has not
reset.

**Nothing is committed unless the gates pass and the review approves.** Failed work is left in
the tree for you to look at, never discarded and never committed.

**`recover` is read-only.** If it finds uncommitted changes it tells you and stops. It will not
guess whether your work is worth keeping.

## Configuration

`config.yaml` in your project root, if you want to change anything:

```yaml
claude:
  model: "claude-opus-5"
  maxRetries: 3           # repair attempts per task when gates fail

review:
  enabled: true
  maxRevisions: 2         # times a rejected change goes back for revision

tasks:
  autoCommit: true        # false leaves passing work uncommitted

quality:
  minimumCoverage: 0.80   # only used by the built-in Go/Node gate sets
  inspectionOnly: false   # same as always passing --inspect-only
```

`leadengineer config show` prints the effective values after defaults, file and environment are
merged. Environment variables use the `LEADENG_` prefix.

## If something looks wrong

| Symptom | Cause |
| --- | --- |
| "no quality gates are defined for project type" | Run `leadengineer detect`, or write `.ai/TOOLCHAIN.md` by hand |
| "working tree has uncommitted changes" | Commit or stash before starting |
| Every task fails the same way | Run the gate commands yourself; `.ai/TOOLCHAIN.md` probably names something that does not work here |
| Watch stopped with "consecutive runs failed" | Something no retry will fix. Check the last error and `leadengineer status` |
| Tasks produce shallow work | Write longer descriptions with acceptance criteria |
