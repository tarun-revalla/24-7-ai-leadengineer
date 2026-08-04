# Architecture Decision Records

## ADR-001: Go as Primary Language

**Status**: Accepted  
**Date**: 2025-08-04  
**Context**: Need to build a production-grade autonomous engineering system that runs continuously with minimal overhead.

### Decision
Use Go as the primary language for the core platform.

### Rationale
1. **Concurrency**: Go's goroutines and channels provide excellent concurrency primitives for multi-subsystem coordination
2. **Performance**: Compiled binaries with minimal memory footprint and fast startup
3. **Portability**: Single binary deployment across Linux, macOS, Windows
4. **Simplicity**: Clean syntax, fast compilation, minimal dependencies
5. **Reliability**: Strong type system prevents entire classes of errors
6. **CLI Ecosystem**: Excellent libraries for CLI tools, configuration, logging
7. **Cloud Native**: De facto standard for infrastructure tools

### Alternatives Considered
- Python: Too slow for continuous operation, high memory overhead
- Node.js: Too slow, large runtime overhead
- Rust: Overkill complexity for this domain, slower development

### Trade-offs
- Less rich standard library than Python (mitigated by excellent third-party packages)
- Smaller ML/AI ecosystem (not relevant—we're a wrapper around Claude)
- Steeper learning curve than Python (acceptable for production engineers)

---

## ADR-002: Modular Architecture with Clean Interfaces

**Status**: Accepted  
**Date**: 2025-08-04  
**Context**: System must be maintainable, testable, and support plugins. Monolithic design would violate these requirements.

### Decision
Organize code into independent subsystems with well-defined interfaces. Each subsystem owns its state and responsibilities. Communication via dependency injection and interface-based contracts.

### Rationale
1. **Testability**: Each subsystem can be tested in isolation with mocks
2. **Maintainability**: Changes to one subsystem don't require changes to others
3. **Extensibility**: New subsystems can be added without modifying existing code
4. **Parallelization**: Teams can work on different subsystems independently
5. **Complexity Management**: Each subsystem has bounded complexity
6. **Debuggability**: Easier to trace issues when responsibilities are clear

### Alternatives Considered
- Monolithic design: Simpler initially, impossible to maintain at scale
- Service-oriented: Too much operational complexity for single-binary deployment
- Plugin-first: Too much coupling overhead for core functionality

### Trade-offs
- Slightly more code structure overhead (interfaces, factories, dependency injection)
- Moderate learning curve for understanding subsystem boundaries
- Initial development slightly slower (mitigated by long-term maintainability gains)

---

## ADR-003: Persistent Project Memory

**Status**: Accepted  
**Date**: 2025-08-04  
**Context**: Claude sessions are temporary. System must maintain context across session boundaries and survive indefinitely without losing work.

### Decision
Maintain persistent project state in `.ai/` directory with Markdown files as the primary storage format. These files serve as both long-term memory and Claude context.

### Rationale
1. **Human-Readable**: Project memory is inspectable and editable by humans
2. **Version Control**: Memory files can be committed and tracked in git
3. **Language-Agnostic**: Can be read/written by any tool in the pipeline
4. **Lightweight**: Minimal dependencies, no database required
5. **Portable**: Travels with the repository
6. **Claude Integration**: Can be directly passed as context to Claude

### Memory Files
- `PROJECT.md` - Project overview and constraints
- `ROADMAP.md` - High-level goals and milestones
- `BACKLOG.md` - Prioritized list of tasks
- `CURRENT.md` - Current work and progress
- `CHANGELOG.md` - What's been completed
- `DECISIONS.md` - Architecture decisions (ADRs)
- `ENGINEERING.md` - Development standards
- `CHECKLISTS/` - Quality gates and task checklists
- `PROMPTS/` - Reusable prompt templates

### Alternatives Considered
- JSON-based storage: Less readable, harder to use as Claude context
- Database: Too much operational complexity
- Only git history: Loses ability to have mutable current state

### Trade-offs
- Slightly more overhead to parse Markdown vs. structured formats
- Requires discipline to keep memory consistent
- Manual merge conflicts possible (mitigated by clear templates)

---

## ADR-004: Checkpoint-Based Recovery

**Status**: Accepted  
**Date**: 2025-08-04  
**Context**: System must recover from crashes, quota exhaustion, and interruptions without losing work or duplicating completed work.

### Decision
Create JSON checkpoints at critical points in task execution. Checkpoints include task state, git state, memory snapshots, and metrics. On startup, detect incomplete tasks and resume from checkpoint.

### Rationale
1. **No Data Loss**: System can always resume from last checkpoint
2. **Deterministic**: Can replay exact state when recovering
3. **Efficient**: Don't redo work that was checkpointed
4. **Debuggable**: Checkpoints provide audit trail of system state
5. **Flexible**: Can checkpoint at any point without disrupting flow

### Checkpoint Triggers
- Every task completion
- Before Claude session ends
- Before quota exhaustion (detected)
- Before system shutdown
- Before critical operations

### Alternatives Considered
- Transaction-based: Too complex for distributed recovery scenario
- No checkpoints: Would lose work on crashes
- Only git commits: Insufficient for partial task recovery

### Trade-offs
- Disk space overhead (mitigated by compression and retention policies)
- Slight latency overhead (async, configurable interval)
- Requires validation on recovery (mitigated by redundant checks)

---

## ADR-005: Quality Gates as Hard Requirements

**Status**: Accepted  
**Date**: 2025-08-04  
**Context**: System must maintain code quality and security standards automatically without human review bottleneck.

### Decision
Every completed task must pass quality gates before being committed. Quality gates are:
- Code formatting (gofmt)
- Lint (golangci-lint)
- Typecheck (go vet)
- Unit tests (80%+ coverage)
- Security scan (gosec)
- Self-review (6 perspectives)

If quality gates fail, ask Claude to fix and retry. Never commit failing code.

### Rationale
1. **Consistency**: Every commit meets quality standards
2. **Prevention**: Catch errors early, not in production
3. **Automation**: Don't require human review for standard issues
4. **Accountability**: Clear pass/fail criteria
5. **Learning**: Claude learns what quality means from failures

### Quality Perspectives
1. Developer - Does it work?
2. Reviewer - Is it maintainable?
3. Security - Are there vulnerabilities?
4. Performance - Any inefficiencies?
5. QA - Edge cases covered?
6. Documentation - Clear and complete?

### Alternatives Considered
- No gates: Would produce unmaintainable code
- Human review only: Too slow, bottleneck
- Soft gates: Standards would drift over time

### Trade-offs
- Slower task completion (necessary for quality)
- More Claude API usage (offset by fewer bugs and rework)
- Strict standards might slow development initially (correct for long-term)

---

## ADR-006: Quota Management and Automatic Resumption

**Status**: Accepted  
**Date**: 2025-08-04  
**Context**: Claude API has usage quotas. System must detect exhaustion, pause gracefully, and resume automatically without human intervention.

### Decision
Monitor Claude quota continuously. When quota approaches limit (configurable threshold), create checkpoint and sleep. Resume automatically after quota reset.

### Rationale
1. **Continuity**: Don't require human intervention when quota resets
2. **Cost Control**: Prevent over-usage by detecting limits early
3. **Reliability**: Graceful pause/resume instead of crashes
4. **Automation**: Enable true 24/7 operation without human monitoring

### Quota Detection Strategy
- Monitor API responses for rate limit headers
- Track cumulative API usage
- Estimate time until quota reset
- Checkpoint state before exhaustion
- Sleep until reset time + buffer

### Alternatives Considered
- Fail fast: Would require human intervention
- Ignore quotas: Would hit hard limits and crash
- Manual quota management: Defeats automation goal

### Trade-offs
- Adds complexity to session management
- Requires accurate quota estimation
- May sleep conservatively (acceptable—system can wait)

---

## ADR-007: Git as Single Source of Truth for Code

**Status**: Accepted  
**Date**: 2025-08-04  
**Context**: Must maintain accurate version history and support conflict resolution. Git is the industry standard.

### Decision
Use git as the single source of truth for all code changes. Require clean commits before task completion. Implement automatic conflict resolution when possible.

### Rationale
1. **Auditability**: Complete history of changes and rationale
2. **Safety**: Can revert problematic changes
3. **Collaboration**: Compatible with human developers
4. **Industry Standard**: Well-understood, widely available
5. **Distributed**: Each clone has full history

### Commit Standards
- Clear, descriptive commit messages (format TBD)
- Atomic commits (one logical change per commit)
- No merge commits in history
- All commits must pass CI/quality gates

### Alternatives Considered
- Subversion: Outdated, centralized model
- Other VCS: Less widely adopted, less tooling

### Trade-offs
- Requires Claude to understand git workflow
- Conflict resolution can be complex (mitigated by frequent commits)
- History can become messy if not carefully maintained

---

## ADR-008: Claude as Execution Engine, Not Decision Maker

**Status**: Accepted  
**Date**: 2025-08-04  
**Context**: Must maintain clear separation between system decision-making and Claude execution.

### Decision
System determines WHAT to do (via planning and prioritization). Claude determines HOW to do it (implementation details). System validates all results before committing.

### Rationale
1. **Accountability**: System is responsible for overall direction
2. **Reproducibility**: Same task produces same result
3. **Auditability**: Clear decision trail
4. **Safety**: No surprises from Claude
5. **Control**: System retains ultimate control over project direction

### Decision-Making Points (System)
- Which task to execute next
- When to checkpoint
- When to sleep/resume
- How to prioritize work
- What constitutes done

### Execution Points (Claude)
- Implementation details
- Code structure
- Bug fixes
- Refactoring approaches
- Optimization strategies

### Alternatives Considered
- Full autonomy for Claude: Too risky, loss of control
- No Claude input: Defeats purpose of using AI
- Hybrid decision: Confusing, unclear responsibility

### Trade-offs
- Requires careful prompt design (ensures Claude understands boundaries)
- More checkpoints needed (necessary for safety)
- Slightly longer task completion (acceptable trade-off)

---

## ADR-009: Immutable Configuration at Runtime

**Status**: Accepted  
**Date**: 2025-08-04  
**Context**: Must ensure predictable behavior and prevent race conditions from configuration changes.

### Decision
Load configuration once at startup. No runtime modifications to configuration. Override mechanisms only via environment variables or CLI flags (require restart to take effect).

### Rationale
1. **Predictability**: Configuration doesn't change mid-operation
2. **Concurrency Safety**: No need for locks on config
3. **Auditability**: Clear what configuration was used for each run
4. **Simplicity**: No complexity around config updates

### Configuration Sources (Priority Order)
1. CLI flags (highest priority)
2. Environment variables
3. `config.yaml` in project
4. Default configuration (lowest priority)

### Alternatives Considered
- Hot-reload configuration: Adds complexity, can cause races
- No configuration: Inflexible, can't adapt to different projects

### Trade-offs
- Requires restart for configuration changes (acceptable—rare)
- Less dynamic (acceptable for a production system)

---

## ADR-010: Structured Logging and Metrics

**Status**: Accepted  
**Date**: 2025-08-04  
**Context**: Must provide observability for debugging, monitoring, and auditing.

### Decision
Use structured JSON logging for all events. Export metrics in Prometheus format. Create comprehensive audit trail of all actions.

### Rationale
1. **Queryability**: JSON logs can be parsed and searched
2. **Metrics Integration**: Standard format for monitoring tools
3. **Auditability**: Complete trace of system behavior
4. **Debugging**: Rich context for each log entry
5. **Compliance**: Full audit trail for production systems

### Logging Levels
- DEBUG: Detailed execution flow, values (disabled in production)
- INFO: Significant events, task boundaries
- WARN: Recoverable errors, degraded behavior
- ERROR: Unrecoverable errors, failures

### Key Metrics
- Task duration
- Success/failure rates
- Quota usage
- Claude session count
- Code quality scores
- Test coverage

### Alternatives Considered
- Text logging: Less queryable, harder to parse
- No logging: Impossible to debug production issues
- Verbose logging: Performance impact

### Trade-offs
- Disk space for logs (mitigated by retention policy)
- Slight performance overhead (negligible)
- More infrastructure complexity (standard logging stack)

