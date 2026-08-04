# 24-7 AI Lead Engineer

**An autonomous software engineering platform that uses Claude Code as its execution engine.**

## Vision

Build a production-grade autonomous system that behaves like a senior software engineer: understanding projects, maintaining long-term state, planning work, implementing features, running tests, fixing failures, and performing reviews—all with minimal human intervention.

The system should:
- Run continuously for months without human intervention
- Automatically recover from crashes and quota exhaustion
- Never lose work or project state
- Maintain code quality and security standards
- Provide complete observability and audit trails

## Status

**Phase 1: Architecture & Planning** ✅ (In Progress)
- Architecture design document
- Architecture Decision Records (ADRs)
- Complete folder structure
- Core interface definitions
- Type definitions
- CLI skeleton

**Phase 2: Core Infrastructure** (Next)
- Configuration Manager
- Logging & Metrics
- Claude Session Manager
- Checkpoint Manager
- Project Memory
- Git Manager

**Phase 3: Execution Engine** (Following)
- Task Queue & Prioritizer
- Claude Runner
- Prompt Builder & Library
- Task Executor

**Phase 4: Quality Gates & Intelligence** (Later)
- Review Engine
- Testing Engine
- Security Engine
- Planner & Prioritizer

**Phase 5: Observability & Dashboard** (Future)
- Progress Reporter
- Web UI
- Plugin system

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         CLI & Configuration                      │
├─────────────────────────────────────────────────────────────────┤
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐        │
│  │ Project  │  │ Planner  │  │   Task   │  │  Claude  │        │
│  │ Scanner  │  │          │  │ Executor │  │  Runner  │        │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘        │
│         │              │              │            │            │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │              Project Memory (Persistent State)           │   │
│  │  .ai/PROJECT.md  .ai/BACKLOG.md  .ai/CURRENT.md etc.   │   │
│  └──────────────────────────────────────────────────────────┘   │
│         ▲              ▲              ▲            ▲            │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐        │
│  │   Git    │  │ Claude   │  │ Quality  │  │Checkpoint│        │
│  │ Manager  │  │ Session  │  │  Gates   │  │ Manager  │        │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘        │
│         │              │              │            │            │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Logging • Metrics • Recovery • Quota Management         │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

## Project Structure

```
.
├── cmd/
│   └── leadengineer/        # CLI entry point
├── internal/
│   ├── config/              # Configuration management
│   ├── cli/                 # CLI commands
│   ├── claude/              # Claude session management
│   ├── checkpoint/          # Checkpoint/recovery
│   ├── project/             # Project analysis
│   ├── memory/              # Persistent memory
│   ├── planner/             # Task planning
│   ├── tasks/               # Task execution
│   ├── git/                 # Git operations
│   ├── quality/             # Quality gates
│   ├── recovery/            # Failure recovery
│   ├── logging/             # Structured logging
│   ├── metrics/             # Metrics collection
│   ├── scheduler/           # Task scheduling
│   └── plugins/             # Plugin system
├── pkg/
│   ├── interfaces/          # Interface definitions
│   ├── types/               # Type definitions
│   ├── utils/               # Utility functions
│   └── middleware/          # Middleware (logging, metrics, etc.)
├── configs/
│   ├── config.default.yaml  # Default configuration
│   └── templates/           # Config templates
├── templates/
│   ├── prompts/             # Prompt templates
│   ├── tasks/               # Task templates
│   └── checklists/          # Quality checklists
├── docs/
│   ├── design/              # Design documents
│   ├── api/                 # API documentation
│   ├── operations/          # Operations guides
│   └── guides/              # User guides
├── examples/
│   ├── projects/            # Example projects
│   └── workflows/           # Example workflows
├── tests/
│   ├── integration/         # Integration tests
│   ├── e2e/                 # End-to-end tests
│   └── fixtures/            # Test fixtures
├── scripts/
│   ├── build/               # Build scripts
│   ├── deploy/              # Deployment scripts
│   └── test/                # Test scripts
├── .ai/
│   ├── checkpoints/         # System state checkpoints
│   ├── sessions/            # Claude session metadata
│   ├── logs/                # Application logs
│   ├── metrics/             # Exported metrics
│   ├── prompts/             # Prompt templates
│   └── checklists/          # Quality checklists
├── ARCHITECTURE.md          # System architecture
├── ADR.md                   # Architecture decisions
├── go.mod                   # Go module definition
└── README.md                # This file
```

## Key Concepts

### Project Memory
The system maintains persistent project state in `.ai/` directory:

- **PROJECT.md** - Project overview, purpose, constraints
- **ROADMAP.md** - High-level goals and milestones  
- **BACKLOG.md** - Prioritized list of tasks
- **CURRENT.md** - Current work in progress
- **CHANGELOG.md** - Completed work history
- **DECISIONS.md** - Architecture decisions (ADRs)
- **ENGINEERING.md** - Development standards
- **CHECKLISTS/** - Quality gates and task checklists
- **PROMPTS/** - Prompt templates for Claude

### Checkpointing
The system creates JSON checkpoints at critical points:
- Every task completion
- Before Claude exits
- Before quota exhaustion
- Every committed change

Checkpoints include:
- Task state
- Git state  
- Memory snapshots
- Quota state
- Performance metrics

On startup, detect incomplete tasks and resume from checkpoint.

### Quality Gates
Every completed task must pass:
- Code formatting (gofmt)
- Lint (golangci-lint)
- Typecheck (go vet)
- Unit tests (80%+ coverage)
- Security scan (gosec)
- Self-review (6 perspectives)

If quality gates fail, Claude automatically fixes issues and retries.

### Quota Management
System monitors Claude quota continuously:
- Detects approaching limits
- Creates checkpoint before exhaustion
- Sleeps gracefully when quota exhausted
- Resumes automatically after quota reset

### Recovery
System automatically recovers from:
- Power failures
- Machine reboots
- Claude crashes
- Network failures
- Git conflicts
- Corrupted checkpoints
- Partial commits

## Installation

### Requirements
- Go 1.21+
- Git 2.30+
- 500MB disk (for checkpoints and logs)

### Build
```bash
go build -o 24-7-ai-leadengineer ./cmd/leadengineer
```

### Initialize Project
```bash
./24-7-ai-leadengineer init <project-path>
```

### Start System
```bash
./24-7-ai-leadengineer start
```

## Configuration

Create `config.yaml` in your project to override defaults:

```yaml
project:
  name: "My Project"
  type: "go"

claude:
  enabled: true
  model: "claude-opus-5"
  maxRetries: 3

quota:
  checkInterval: 30s
  autoSleepOnExhaustion: true
  sleepDuration: 3600s

checkpoint:
  interval: 300s
  retention: 7

logging:
  level: "info"
  format: "json"
```

See `configs/config.default.yaml` for all available options.

## Usage

### Show Status
```bash
./24-7-ai-leadengineer status
```

### View Logs
```bash
./24-7-ai-leadengineer logs tail
./24-7-ai-leadengineer logs search "error"
```

### Manage Checkpoints
```bash
./24-7-ai-leadengineer checkpoint list
./24-7-ai-leadengineer checkpoint restore <checkpoint-id>
```

### Recover from Failures
```bash
./24-7-ai-leadengineer recover
```

### Configure System
```bash
./24-7-ai-leadengineer config show
./24-7-ai-leadengineer config set key value
```

## Architecture Decisions

Key architectural decisions are documented in `ADR.md`:

- **ADR-001**: Go as primary language
- **ADR-002**: Modular architecture with clean interfaces
- **ADR-003**: Persistent project memory
- **ADR-004**: Checkpoint-based recovery
- **ADR-005**: Quality gates as hard requirements
- **ADR-006**: Quota management and automatic resumption
- **ADR-007**: Git as single source of truth
- **ADR-008**: Claude as execution engine, not decision maker
- **ADR-009**: Immutable configuration at runtime
- **ADR-010**: Structured logging and metrics

## Development

### Project Structure Principles

1. **Modularity** - Each subsystem is independently testable
2. **Determinism** - Reproducible behavior, no race conditions  
3. **Fault Tolerance** - Recovery from all failure modes
4. **State Checkpointing** - Never lose progress
5. **Context Persistence** - Project memory survives sessions
6. **Composition** - Clean interfaces, minimal coupling

### Adding New Subsystems

1. Define interfaces in `pkg/interfaces/`
2. Create subsystem in `internal/<subsystem>/`
3. Write comprehensive tests in `internal/<subsystem>/tests/`
4. Document in `docs/`
5. Add configuration in `configs/`
6. Create examples in `examples/`

### Testing Requirements

- 80%+ code coverage minimum
- Unit tests for all public interfaces
- Integration tests for subsystem interactions
- End-to-end tests for complete workflows
- Recovery scenario tests

### Code Quality

```bash
# Format code
go fmt ./...

# Run linter
golangci-lint run

# Check types
go vet ./...

# Run tests
go test ./... -cover

# Run security scan
gosec ./...
```

## Observability

### Logging
- Structured JSON logs in `.ai/logs/`
- Multiple levels: debug, info, warn, error
- Contextual information for debugging
- Full audit trail of actions

### Metrics
- Prometheus format metrics in `.ai/metrics/`
- Task duration, success/failure rates
- Quota usage, Claude session count
- Code quality scores, test coverage
- Available at `http://localhost:9090/metrics`

### Dashboard
- Real-time progress tracking
- Live logs and metrics
- Claude status and quota
- Repository health
- Architecture score and technical debt
- Available at `http://localhost:8080/`

## Performance Goals

- **Reliability**: 99.9% task completion rate
- **Recovery**: 95%+ automatic recovery success
- **Quality**: 100% of tasks pass quality gates
- **Performance**: Average task in < 1 hour
- **Observability**: 100% of actions logged
- **Cost**: < 5% wasted API tokens

## Contributing

This project follows strict engineering standards:

- No god objects, no giant files
- Comprehensive tests before code
- Production-quality code only
- No TODOs or placeholder code
- Minimal comments (only non-obvious "why")
- Excellent naming and clean APIs

## License

To be determined.

## Roadmap

### Phase 2: Core Infrastructure
- [ ] Configuration Manager
- [ ] Logging & Metrics
- [ ] Claude Session Manager  
- [ ] Checkpoint Manager
- [ ] Project Memory
- [ ] Git Manager

### Phase 3: Execution Engine
- [ ] Task Queue & Prioritizer
- [ ] Claude Runner
- [ ] Prompt Builder & Library
- [ ] Task Executor

### Phase 4: Quality & Intelligence
- [ ] Review Engine (6 perspectives)
- [ ] Testing Engine
- [ ] Security Engine
- [ ] Planner & Prioritizer

### Phase 5: Observability
- [ ] Progress Reporter
- [ ] Web Dashboard
- [ ] Plugin System

### Future
- [ ] Multi-project support
- [ ] Multi-agent orchestration
- [ ] GitHub/GitLab integration
- [ ] Slack notifications
- [ ] Docker/Kubernetes support
- [ ] Cloud deployment
- [ ] Open-source framework release

## Contact

For questions or discussion, open an issue or PR.

---

**Status**: This is an active development project. Documentation and interfaces may change as the system evolves.
