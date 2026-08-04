# Project: 24-7 AI Lead Engineer

## Overview

An autonomous software engineering platform that uses Claude Code as its execution engine. The system behaves like a senior software engineer: understanding projects, maintaining long-term state, planning work, implementing features, running tests, fixing failures, and performing reviews—all with minimal human intervention.

## Purpose

Build a production-grade, reusable framework capable of:
- Running continuously for months without human intervention
- Automatically recovering from all failure modes
- Never losing work or project state
- Maintaining high code quality and security standards
- Providing complete observability and audit trails

## Vision

This framework should eventually become the industry-standard open-source autonomous AI engineering platform, usable across ANY software repository.

## Constraints

1. **No Data Loss** - Must never lose work or project state
2. **Deterministic** - Same inputs → same outputs
3. **Fault Tolerant** - Recover from crashes, quota exhaustion, network failures
4. **Production Quality** - No TODOs, no placeholders, comprehensive tests
5. **Modular** - Every subsystem independently testable and replaceable
6. **Observable** - Complete logging, metrics, and audit trails

## Key Principles

1. Modularity over monoliths
2. Composition over inheritance
3. Interfaces over implementations
4. Fault tolerance by design
5. State checkpointing at every critical point
6. Claude as execution engine, system as decision maker
7. Git as single source of truth for code
8. Persistent memory for long-term context

## Technology Stack

- **Language**: Go 1.21+
- **Package Manager**: Go modules
- **CLI Framework**: Cobra
- **Configuration**: Viper
- **Logging**: Zap (structured JSON)
- **Metrics**: Prometheus format
- **Version Control**: Git

## Success Metrics

- **Reliability**: 99.9% task completion rate
- **Recovery**: 95%+ automatic recovery success  
- **Quality**: 100% of tasks pass quality gates before commit
- **Performance**: Average task completion in < 1 hour
- **Observability**: 100% of actions logged and traceable
- **Cost**: < 5% wasted API tokens

## Deployment Target

- Single binary (no runtime dependencies)
- Runs on Linux, macOS, Windows
- Minimal resource footprint
- Container-ready

## Related Documentation

- `ARCHITECTURE.md` - Complete system architecture
- `ADR.md` - Architecture Decision Records
- `README.md` - User guide
- `.ai/ROADMAP.md` - High-level goals
- `.ai/BACKLOG.md` - Task queue
- `.ai/CURRENT.md` - Current work
- `.ai/ENGINEERING.md` - Development standards

## Last Updated

2025-08-04 20:30 UTC
