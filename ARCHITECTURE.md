# Autonomous AI Engineering Platform - System Architecture

## Vision

An autonomous software engineering system that uses Claude Code as its execution engine. The system behaves like a senior software engineer: understanding projects, maintaining long-term state, planning work, implementing features, running tests, fixing failures, and performing reviews—all with minimal human intervention.

## Core Principles

1. **Modularity** - Every subsystem is independently testable and replaceable
2. **Determinism** - Reproducible behavior, no race conditions
3. **Fault Tolerance** - Recovery from crashes, interruptions, quota exhaustion
4. **State Checkpointing** - Never lose progress
5. **Context Persistence** - Project memory survives Claude sessions
6. **Composition** - Clean interfaces, minimal coupling
7. **Observability** - Comprehensive logging, metrics, and introspection

---

## System Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         CLI & Configuration                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐│
│  │   Project        │  │   Planner &      │  │   Task Queue &   ││
│  │   Scanner        │  │   Prioritizer    │  │   Executor       ││
│  └──────────────────┘  └──────────────────┘  └──────────────────┘│
│           │                    │                      │           │
│  ┌──────────────────────────────────────────────────────────────┐│
│  │              Project Memory (Persistent State)               ││
│  │  - PROJECT.md   - ROADMAP.md  - BACKLOG.md  - CURRENT.md    ││
│  │  - DECISIONS.md - CHANGELOG.md - CHECKLISTS/ - PROMPTS/     ││
│  └──────────────────────────────────────────────────────────────┘│
│           ▲                    ▲                      ▲           │
│           │                    │                      │           │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐│
│  │   Git Manager    │  │   Claude Session │  │   Quality Gates  ││
│  │                  │  │   Manager        │  │   (Tests, Lint,  ││
│  │  - Commit        │  │                  │  │    Security,     ││
│  │  - Push          │  │  - Launch Claude │  │    Perf)         ││
│  │  - History       │  │  - Resume        │  │                  ││
│  │  - Conflict      │  │  - Quota Mgmt    │  │  - Review Engine ││
│  │    Resolution    │  │  - Checkpointing │  │  - Security Scan ││
│  └──────────────────┘  └──────────────────┘  └──────────────────┘│
│                                                                   │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐│
│  │   Checkpoint     │  │   Recovery       │  │   Logging &      ││
│  │   Manager        │  │   Manager        │  │   Metrics        ││
│  │                  │  │                  │  │                  ││
│  │  - Save State    │  │  - Crash        │  │  - Structured    ││
│  │  - Restore       │  │    Recovery     │  │    Logs          ││
│  │  - Validation    │  │  - Quota Reset  │  │  - Prometheus    ││
│  │  - Compression   │  │  - Git Repair   │  │    Metrics       ││
│  └──────────────────┘  └──────────────────┘  └──────────────────┘│
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
        ▲                                                ▲
        │                                                │
    ┌───────────────────┐                    ┌───────────────────┐
    │  Configuration    │                    │  Observability    │
    │  & State Files    │                    │  Dashboard API    │
    └───────────────────┘                    └───────────────────┘
```

---

## Subsystem Responsibilities

### 1. Configuration Manager
**Responsibility**: Load and manage system configuration from multiple sources.

**Inputs**:
- `config.yaml` - Default configuration
- Environment variables
- CLI flags
- Runtime overrides

**Outputs**:
- `Config` struct accessible to all subsystems
- Validation errors

**Key Interfaces**:
- `ConfigLoader` - Load from different sources
- `ConfigValidator` - Validate configuration
- `ConfigProvider` - Provide config to subsystems

**State**: Immutable after initialization

---

### 2. Project Scanner & Analyzer
**Responsibility**: Understand the structure of a software project.

**Inputs**:
- Repository path
- Git information
- Build system (Go, Node, Python, etc.)

**Outputs**:
- Project structure analysis
- Language detection
- Build system detection
- Entry points
- Dependencies

**Key Interfaces**:
- `ProjectAnalyzer` - Analyze project structure
- `LanguageDetector` - Detect programming languages
- `BuildSystemDetector` - Detect build system

**State**: Cached analysis of project, refreshed on demand

---

### 3. Project Memory
**Responsibility**: Persistent, long-term project context that survives Claude sessions.

**Files**:
```
PROJECT.md            - Project overview, purpose, constraints
ROADMAP.md            - High-level goals, milestones
BACKLOG.md            - Prioritized list of tasks/issues
CURRENT.md            - Current focus, in-progress work
ARCHITECTURE.md       - System design, decisions, patterns
ENGINEERING.md        - Development standards, conventions
CHANGELOG.md          - What's been completed
DECISIONS.md          - ADRs, key decisions made
CHECKLISTS/           - Quality gates, task checklists
  - quality-gates.md
  - pre-commit.md
  - testing-checklist.md
PROMPTS/              - Prompt templates
  - analyze-code.md
  - implement-feature.md
  - review-code.md
  - fix-failure.md
```

**Key Interfaces**:
- `Memory` - Read/write memory documents
- `MemorySync` - Sync memory with Claude
- `MemoryValidator` - Ensure consistency

**State**: Files in `.ai/` directory

---

### 4. Claude Session Manager
**Responsibility**: Launch, manage, and maintain Claude Code sessions.

**Responsibilities**:
- Launch Claude Code CLI
- Resume previous sessions
- Detect quota exhaustion
- Detect failures and crashes
- Handle interruptions
- Manage session lifecycle
- Track API usage

**Key Interfaces**:
- `SessionManager` - Launch and manage sessions
- `QuotaDetector` - Detect quota status
- `SessionRecovery` - Recover from failures
- `UsageTracker` - Track API usage

**State**: 
- Session metadata in `.ai/sessions/`
- Current session ID
- Quota state

---

### 5. Checkpoint Manager
**Responsibility**: Save and restore system state for crash recovery.

**Checkpoints on**:
- Every completed task
- Before Claude exits
- Before quota exhaustion
- Every committed change
- Critical state changes

**Checkpoint Contents**:
- Current task state
- Progress tracking
- Memory snapshots
- Git state
- Prompt history
- Performance metrics

**Key Interfaces**:
- `CheckpointWriter` - Write checkpoint
- `CheckpointReader` - Restore checkpoint
- `CheckpointValidator` - Verify checkpoint integrity

**State**: `.ai/checkpoints/` directory

---

### 6. Git Manager
**Responsibility**: Safe, auditable git operations with conflict resolution.

**Responsibilities**:
- Commit with standards
- Push with retry logic
- Pull with conflict detection
- Branch management
- History tracking
- Conflict resolution guidance

**Key Interfaces**:
- `GitCommitter` - Stage and commit
- `GitPusher` - Push with retries
- `GitConflictResolver` - Handle conflicts
- `GitStatus` - Report git state

**State**: Standard `.git/` directory

---

### 7. Planner & Task Prioritizer
**Responsibility**: Determine what to do next.

**Process**:
1. Read BACKLOG.md
2. Analyze project state
3. Consider constraints (quota, time, dependencies)
4. Prioritize tasks
5. Select next task
6. Create detailed implementation plan

**Key Interfaces**:
- `Planner` - Create implementation plans
- `Prioritizer` - Rank tasks by priority
- `Analyzer` - Analyze project state
- `Recommender` - Recommend next task

**State**: Task priority in BACKLOG.md, current plan in CURRENT.md

---

### 8. Task Queue & Executor
**Responsibility**: Execute tasks sequentially with quality gates.

**Task Lifecycle**:
1. Dequeue from BACKLOG
2. Plan implementation
3. Implement
4. Run tests
5. Run lint
6. Run security scan
7. Self-review
8. Commit
9. Update documentation
10. Update CHANGELOG

**Quality Gates** (all must pass):
- Code formatting
- Lint
- Typecheck
- Unit tests
- Integration tests
- Security scan
- Static analysis
- Coverage threshold

**Key Interfaces**:
- `TaskExecutor` - Execute task end-to-end
- `QualityGate` - Verify quality criteria
- `TaskMonitor` - Track task progress

**State**: Current task in CURRENT.md

---

### 9. Claude Runner
**Responsibility**: Execute Claude Code instructions with context.

**Process**:
1. Build prompt from templates + project memory
2. Launch Claude session
3. Execute instructions
4. Capture results
5. Validate output
6. Handle errors
7. Checkpoint progress

**Key Interfaces**:
- `Runner` - Execute Claude instructions
- `PromptBuilder` - Build complete prompts
- `OutputValidator` - Validate Claude output

**State**: Session state, prompt history

---

### 10. Prompt Library
**Responsibility**: Maintain reusable, well-tested prompts.

**Prompts**:
- Analyze project structure
- Implement feature
- Fix bug
- Run tests
- Review code
- Refactor
- Security audit
- Performance audit
- Update documentation

**Key Interfaces**:
- `PromptTemplate` - Define prompt templates
- `PromptRenderer` - Render with variables
- `PromptLibrary` - Access prompts

**State**: `.ai/prompts/` directory

---

### 11. Review Engine
**Responsibility**: Self-review code before committing.

**Review Perspectives**:
1. **Developer** - Correctness, does it work?
2. **Reviewer** - Design, readability, maintainability
3. **Security Engineer** - Vulnerabilities, security patterns
4. **Performance Engineer** - Efficiency, optimization opportunities
5. **QA Engineer** - Edge cases, testing, error handling
6. **Documentation Engineer** - Clarity, completeness

**Key Interfaces**:
- `Reviewer` - Execute review
- `ReviewFinding` - Represent findings
- `ReviewApprover` - Make approval decision

**State**: Review results in CURRENT.md

---

### 12. Testing Engine
**Responsibility**: Run tests and report results.

**Responsibilities**:
- Detect test framework
- Run unit tests
- Run integration tests
- Capture output
- Parse failures
- Report coverage

**Key Interfaces**:
- `TestRunner` - Run tests
- `TestParser` - Parse test output
- `CoverageReporter` - Report coverage

**State**: Test results in metrics

---

### 13. Security Engine
**Responsibility**: Scan for security vulnerabilities.

**Scans**:
- Static analysis (gosec, etc.)
- Dependency vulnerabilities
- Secret detection
- Common patterns

**Key Interfaces**:
- `SecurityScanner` - Run security scans
- `VulnerabilityFinder` - Find issues
- `SecurityReporter` - Report findings

**State**: Security findings in metrics

---

### 14. Quota Manager
**Responsibility**: Detect usage limits and manage quota.

**Detects**:
- API rate limits
- Usage quotas
- Cooldown periods
- Context window limits
- Token exhaustion

**Actions**:
- Checkpoint state
- Sleep/wait
- Resume automatically
- Alert when needed

**Key Interfaces**:
- `QuotaMonitor` - Monitor quota status
- `CooldownManager` - Handle cooldowns
- `AutoResumer` - Resume after quota reset

**State**: Quota tracking in `.ai/quota.json`

---

### 15. Recovery Manager
**Responsibility**: Recover from failures automatically.

**Recovers From**:
- Power failures
- Machine reboots
- Claude crashes
- Git conflicts
- Network failures
- Corrupted checkpoints
- Partial commits
- Quota exhaustion

**Recovery Strategies**:
- Load latest checkpoint
- Validate git state
- Resume task
- Retry operation
- Conflict resolution
- Manual intervention (last resort)

**Key Interfaces**:
- `RecoveryPlanner` - Plan recovery
- `StateValidator` - Validate state
- `AutoRecovery` - Execute recovery

**State**: Recovery state in checkpoints

---

### 16. Logging & Metrics
**Responsibility**: Comprehensive observability.

**Logging**:
- Structured JSON logs
- Multiple levels (debug, info, warn, error)
- Contextual information
- Performance timing

**Metrics**:
- Task duration
- Success/failure rates
- Quota usage
- Claude session count
- Code quality scores
- Test coverage

**Key Interfaces**:
- `Logger` - Log events
- `MetricsCollector` - Collect metrics
- `MetricsExporter` - Export metrics

**State**: Logs in `.ai/logs/`, metrics exported periodically

---

### 17. Progress Reporter
**Responsibility**: Report system status and progress.

**Reports**:
- Current task
- Estimated completion time
- Success rate
- Recent failures
- Quota status
- Repository health

**Key Interfaces**:
- `ProgressReporter` - Generate reports
- `StatusProvider` - Provide current status

**State**: Status in `.ai/status.json`

---

### 18. Scheduler & Timing
**Responsibility**: Schedule recurring tasks and manage timing.

**Scheduled Tasks**:
- Background health checks
- Periodic metrics export
- Documentation updates
- Architecture audits

**Key Interfaces**:
- `Scheduler` - Schedule tasks
- `Timer` - Manage timing

**State**: Schedule in configuration

---

### 19. Plugin Manager
**Responsibility**: Support plugins for extensibility.

**Future Plugins**:
- GitHub integration
- Slack notifications
- Docker execution
- Cloud deployment
- Custom analyzers

**Key Interfaces**:
- `Plugin` - Plugin interface
- `PluginLoader` - Load plugins
- `PluginRegistry` - Manage plugins

**State**: Plugin configuration and metadata

---

### 20. Web Dashboard API
**Responsibility**: Serve observability dashboard.

**Features**:
- Live task progress
- Logs and metrics
- Claude status
- Quota visualization
- Repository health
- Architecture score
- Technical debt tracking

**Key Interfaces**:
- `DashboardServer` - HTTP server
- `EventBroadcaster` - Real-time updates

**State**: Run-time state

---

## Data Flows

### Task Execution Flow

```
┌─────────────────┐
│  Read Backlog   │
└────────┬────────┘
         │
         ▼
┌─────────────────────────┐
│  Analyze Project State  │
└────────┬────────────────┘
         │
         ▼
┌─────────────────────────┐
│  Prioritize Tasks       │
└────────┬────────────────┘
         │
         ▼
┌─────────────────────────┐
│  Select Next Task       │
└────────┬────────────────┘
         │
         ▼
┌─────────────────────────┐
│  Create Implementation  │
│  Plan                   │
└────────┬────────────────┘
         │
         ▼
┌─────────────────────────┐
│  Launch Claude +        │
│  Execute Task           │
└────────┬────────────────┘
         │
         ▼
┌─────────────────────────┐
│  Run Quality Gates      │
│  (tests, lint, etc.)    │
└────────┬────────────────┘
         │
    ┌────┴─────────────┐
    │                  │
    ▼                  ▼
  PASS              FAIL
    │                  │
    │          ┌───────┴────────┐
    │          │                │
    │          ▼                ▼
    │    Collect Failures  Quota Exceeded?
    │          │                │
    │          ▼                ▼
    │    Launch Claude     Checkpoint &
    │    (fix failures)     Sleep
    │          │
    │          └──────┬────────┘
    │                 │
    │          ┌──────▼────────┐
    │          │                │
    │          ▼                ▼
    │    PASS                Resume
    │
    ▼
┌─────────────────────────┐
│  Self-Review Code       │
│  (6 perspectives)       │
└────────┬────────────────┘
         │
    ┌────┴─────────────┐
    │                  │
    ▼                  ▼
  APPROVE           REQUEST CHANGES
    │                  │
    │          ┌───────┴────────┐
    │          │                │
    │          ▼                ▼
    │    Launch Claude  Checkpoint &
    │    (make changes)   Retry
    │          │
    │          └──────┬────────┘
    │                 │
    │                 ▼
    │            APPROVED
    │
    ▼
┌─────────────────────────┐
│  Commit to Git          │
└────────┬────────────────┘
         │
         ▼
┌─────────────────────────┐
│  Update Documentation   │
│  & Changelog            │
└────────┬────────────────┘
         │
         ▼
┌─────────────────────────┐
│  Create Checkpoint      │
└────────┬────────────────┘
         │
         ▼
┌─────────────────────────┐
│  Task Complete          │
└─────────────────────────┘
```

### Recovery Flow

```
┌──────────────────────┐
│  Detect Failure      │
│  (crash, quota, etc) │
└─────────┬────────────┘
          │
          ▼
┌──────────────────────┐
│  Load Latest         │
│  Checkpoint          │
└─────────┬────────────┘
          │
          ▼
┌──────────────────────┐
│  Validate Git State  │
└─────────┬────────────┘
          │
    ┌─────┴─────────────┐
    │                   │
    ▼                   ▼
 VALID              CONFLICT
    │                   │
    │          ┌────────┴──────┐
    │          │                │
    │          ▼                ▼
    │    Attempt            Manual
    │    Auto-Resolve       Intervention
    │          │
    │          └────┬───────┘
    │               │
    ▼               ▼
┌──────────────────────┐
│  Resume Task         │
│  from Checkpoint     │
└──────────────────────┘
```

---

## State Management

### Persistent State (Must Survive All Failures)
- `.ai/PROJECT.md` - Project metadata
- `.ai/ROADMAP.md` - High-level goals
- `.ai/BACKLOG.md` - Tasks queue
- `.ai/CURRENT.md` - Current work
- `.ai/CHANGELOG.md` - Completed work
- `.ai/DECISIONS.md` - Architecture decisions
- `.ai/checkpoints/` - Checkpoint files
- `.ai/sessions/` - Session metadata
- `.ai/quota.json` - Quota tracking
- Git repository

### Runtime State (Can be Reconstructed)
- Current task in memory
- Claude session ID
- Performance metrics
- Logs

### Checkpoint Format (JSON)
```json
{
  "timestamp": "2025-08-04T20:30:00Z",
  "checkpointId": "ckpt-12345",
  "taskId": "task-67890",
  "taskState": "in-progress",
  "taskDescription": "...",
  "progress": 0.65,
  "gitState": {
    "branch": "main",
    "commitHash": "abc123",
    "hasUncommitted": false,
    "hasUnpushed": false
  },
  "claudeState": {
    "sessionId": "sess-xyz",
    "quotaRemaining": 0.8,
    "lastActivity": "2025-08-04T20:25:00Z"
  },
  "memorySnapshot": {
    "backlog": [...],
    "currentTask": {...},
    "decisions": [...]
  },
  "metrics": {
    "taskStartTime": "2025-08-04T20:00:00Z",
    "estimatedCompletion": "2025-08-04T21:00:00Z",
    "successRate": 0.95
  }
}
```

---

## Quality Gates & Standards

### Code Quality Requirements
- **Formatting**: gofmt
- **Lint**: golangci-lint
- **Typecheck**: go vet
- **Tests**: 80%+ coverage minimum
- **Security**: gosec, no hardcoded secrets
- **Performance**: No regressions

### Task Completion Criteria
1. ✅ Code passes all quality gates
2. ✅ All tests pass
3. ✅ Self-review approved
4. ✅ Documentation updated
5. ✅ CHANGELOG updated
6. ✅ Commit pushed
7. ✅ Checkpoint created

### Review Perspectives
1. **Developer** - Does it work? Correctness?
2. **Reviewer** - Is it maintainable? Good design?
3. **Security** - Any vulnerabilities?
4. **Performance** - Any inefficiencies?
5. **QA** - Edge cases covered? Error handling?
6. **Documentation** - Clear and complete?

---

## Configuration Structure

```yaml
# config.yaml
project:
  name: "24-7-AI-LeadEngineer"
  path: "."
  type: "go"

claude:
  enabled: true
  model: "claude-opus-5"  # Can be configured
  maxRetries: 3
  timeoutSeconds: 300
  contextWindowSize: 200000

quota:
  checkInterval: 30s
  warningThreshold: 0.8
  exhaustionThreshold: 0.95
  autoSleepOnExhaustion: true
  sleepDuration: 3600s

checkpoint:
  interval: 300s  # Every 5 minutes
  maxSize: "100MB"
  retention: 7  # Keep 7 most recent
  compression: true

git:
  committerName: "24-7-AI-LeadEngineer"
  committerEmail: "ai@leadengineer.local"
  autoRetry: true
  retryAttempts: 4

tasks:
  maxDuration: 3600s  # 1 hour per task
  autoCheckpoint: true
  autoCommit: true

logging:
  level: "info"
  format: "json"
  output: ".ai/logs/app.log"
  retention: 30  # days

metrics:
  enabled: true
  exportInterval: 300s
  exportPath: ".ai/metrics/"

plugins:
  enabled: false
  directory: "plugins/"

dashboard:
  enabled: true
  port: 8080
  address: "127.0.0.1"
```

---

## Error Handling Strategy

### Recoverable Errors
- Network failures → Retry with backoff
- Git conflicts → Attempt auto-resolve
- Quota exhaustion → Checkpoint and sleep
- Test failures → Ask Claude to fix
- Lint failures → Ask Claude to fix

### Non-Recoverable Errors
- Corrupted repository → Manual intervention
- Missing critical files → Manual intervention
- Invalid configuration → Exit and report

### Escalation Path
1. Automatic recovery (if available)
2. Logging and checkpoint
3. Notification to user
4. Manual intervention required

---

## Testing Strategy

### Unit Tests
- Each subsystem has isolated unit tests
- Mock external dependencies
- 80%+ coverage minimum

### Integration Tests
- Subsystems working together
- Real project scenarios
- Real git operations (in temporary repo)

### End-to-End Tests
- Full task execution
- Checkpoint and recovery
- Multi-session scenarios

### Scenario Tests
- Recovery from crashes
- Quota exhaustion and resume
- Git conflict resolution
- Long-running tasks

---

## Deployment & Operations

### System Requirements
- Go 1.21+
- Git 2.30+
- 500MB disk minimum (for checkpoints and logs)

### Installation
```bash
go build -o 24-7-ai-leadengineer ./cmd/main
./24-7-ai-leadengineer init <project-path>
./24-7-ai-leadengineer start
```

### Monitoring
- Metrics exported to `.ai/metrics/`
- Logs in `.ai/logs/`
- Status available at `http://localhost:8080/status`
- Events logged for audit trail

### Recovery
- Automatic on startup
- Checkpoints validated
- Git state verified
- Tasks resumed from checkpoint

---

## Evolution & Extensibility

### Phase 2 Enhancements
- Multi-agent orchestration
- Plugin system
- Custom prompt library
- Performance profiling

### Phase 3 Enhancements
- Web dashboard
- Real-time monitoring
- Advanced analytics
- Integration plugins (GitHub, Slack, etc.)

### Phase 4+
- Open-source framework
- Community plugins
- Multi-project support
- Enterprise features

---

## Success Metrics

- **Reliability**: 99.9% task completion rate
- **Recovery**: 95%+ automatic recovery success
- **Quality**: 100% of tasks pass quality gates before commit
- **Performance**: Average task completion in < 1 hour
- **Observability**: 100% of actions logged and traceable
- **Cost**: Minimal wasted API tokens (< 5%)

