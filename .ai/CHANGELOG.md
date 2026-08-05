---
entries:
    - taskId: PHASE5-003
      title: Quality gates for JavaScript and TypeScript
      date: 2026-08-05T10:16:24.947147436Z
      summary: Node gate set driven by package.json scripts; the implement prompt now states the bar the configured gates actually enforce rather than hardcoding Go.
    - taskId: PHASE4-002
      title: Review engine
      date: 2026-08-05T09:49:10.833894449Z
      summary: Six-perspective self-review between the gates and the commit; an unreadable verdict blocks rather than approves.
    - taskId: PHASE4-001
      title: Quality gate runners
      date: 2026-08-05T09:49:10.833894449Z
      summary: format, secrets, build, vet, test, lint, security. Required gates fail when their tooling is absent.
    - taskId: PHASE3-005
      title: Integration test against the real Claude CLI
      date: 2026-08-05T09:49:10.833894449Z
      summary: Confirmed the documented flags and JSON envelope against a real binary.
    - taskId: PHASE3-004
      title: Task executor with quality gates
      date: 2026-08-05T09:49:10.833894449Z
      summary: Select, implement, verify, repair, review, commit, record. Nothing commits until every required gate passes.
    - taskId: PHASE3-003
      title: Recovery manager
      date: 2026-08-05T09:49:10.833894449Z
      summary: Diagnoses interrupted work read-only; never discards uncommitted changes.
    - taskId: PHASE3-002
      title: Quota manager with checkpoint-and-sleep
      date: 2026-08-05T09:49:10.833894449Z
      summary: Cooldowns persist across restarts, so a new process waits rather than burning attempts.
    - taskId: PHASE3-001
      title: Wire CLI commands to real subsystems
      date: 2026-08-05T09:49:10.833894449Z
      summary: Composition root in internal/app; every command operates on real state.
    - taskId: PHASE2-007
      title: Checkpoint manager with crash-safe atomic writes
      date: 2026-08-05T03:51:38.270551246Z
      summary: Atomic write, checksum verification, corruption-skipping recovery, corrupt-first pruning.
    - taskId: PHASE2-006
      title: Claude session manager driving the real CLI
      date: 2026-08-05T03:51:38.270551246Z
      summary: Replaced simulated execution; classified quota, network, timeout and crash failures.
    - taskId: PHASE2-005
      title: Git manager
      date: 2026-08-05T03:51:38.270551246Z
      summary: Context-bound operations, propagated errors, conflict detection.
    - taskId: PHASE2-004
      title: Project memory
      date: 2026-08-05T03:51:38.270551246Z
      summary: Front-matter documents preserving human-written bodies across state writes.
    - taskId: PHASE2-001
      title: Configuration, logging and metrics
      date: 2026-08-05T03:51:38.270551246Z
      summary: Layered config with validation, structured logging, Prometheus metrics.
updatedAt: 2026-08-05T10:16:24.94899137Z
---

# Changelog

Completed work, newest first. Each entry records the task, the commit that
carried it, and a summary of what changed.
