# Engineering Standards

This document defines the development standards and practices for the 24-7 AI Lead Engineer project.

## Code Quality Requirements

### Formatting
- Run `go fmt ./...` before committing
- No exceptions to formatting rules
- CI will reject unformatted code

### Linting
- Run `golangci-lint run ./...` before committing
- Fix all lint issues, no exceptions
- CI will reject code with lint errors
- Configuration in `.golangci.yml`

### Type Safety
- Run `go vet ./...` before committing
- Fix all type issues
- No unsafe code unless absolutely necessary (document why)
- CI will reject code with type errors

### Testing
- Minimum 80% code coverage
- All public functions must have tests
- All interfaces must have tests
- Unit tests for single components
- Integration tests for subsystem interactions
- End-to-end tests for complete workflows
- Run `go test ./... -cover` before committing

### Security
- Run `gosec ./...` before committing
- No hardcoded secrets or credentials
- No SQL injection vulnerabilities
- No command injection vulnerabilities
- No unsafe file operations
- Validate all external inputs
- CI will reject code with security issues

### Performance
- No obvious inefficiencies
- No unnecessary allocations
- No goroutine leaks
- Use sync.Pool for frequently allocated objects
- Profile hot paths under load
- Document performance assumptions

## Code Organization

### File Structure
- **Maximum file size**: 500 lines
- **Maximum function size**: 50 lines
- **Maximum cyclomatic complexity**: 10
- Break large files into focused packages
- One concept per file

### Naming Conventions
- **Packages**: lowercase, concise, descriptive (e.g., `checkpoint`, `claude`)
- **Interfaces**: end with "er" or describe role (e.g., `Logger`, `Planner`, `Runner`)
- **Implementations**: concrete names (e.g., `ZapLogger`, `SimpleCheckpoint`)
- **Functions**: PascalCase for exported, camelCase for unexported
- **Variables**: Clear, concise names (no single letter except loops/defers)
- **Constants**: ALL_CAPS for exported constants

### Imports
- Group imports: stdlib, third-party, internal
- Alphabetical within groups
- Run `goimports` to auto-format

## Documentation

### Comments
- **No TODOs**: If it needs to be done, it needs to be done now
- **No explanations of WHAT**: Code should be self-explanatory via naming
- **Only WHAT's non-obvious**: Hidden constraints, subtle invariants, workarounds
- **No reference to caller**: Don't mention "used by X" or "for feature Y"
- **Maximum 1 line**: Comments should be brief

### Package Documentation
```go
// Package checkpoint manages system state checkpoints.
package checkpoint
```

### Function Documentation
- Document exported functions
- Describe behavior, not implementation
- Include return value descriptions
- Document error cases

```go
// CreateCheckpoint saves the current system state to a checkpoint file.
// Returns error if checkpoint creation fails or disk space is insufficient.
func (m *Manager) CreateCheckpoint(ctx context.Context, state *CheckpointState) (string, error) {
```

### README & Guides
- Keep up-to-date with code changes
- Clear examples for all features
- Troubleshooting section
- Performance characteristics

## Testing Standards

### Unit Tests
- Test the public interface only
- One package per test file
- Naming: `TestFunctionName`
- Use table-driven tests for multiple scenarios
- No external dependencies (mock if needed)

### Integration Tests
- Test subsystems working together
- Real file system operations (in temp dir)
- Real git operations (in temp repo)
- Located in `tests/integration/`

### End-to-End Tests
- Full task execution scenarios
- Multi-step workflows
- Recovery scenarios
- Located in `tests/e2e/`

### Test Data
- Use fixtures for complex data
- Keep fixtures small and focused
- Located in `tests/fixtures/`

### Coverage
- Minimum 80% for each subsystem
- Aim for 90%+ for critical paths
- Branch coverage matters
- Report coverage in metrics

## Git Practices

### Commits
- **Atomic**: One logical change per commit
- **Descriptive**: Clear, imperative commit message
- **Well-tested**: All tests pass before committing
- **Quality gates**: All gates pass before committing
- **No merges**: Rebase, don't merge

### Commit Messages
```
Brief summary (max 70 characters)

Longer explanation if needed. Explain the WHY, not the WHAT.
Reference related issues or decisions.

Fixes #123
Related to ADR-001
```

### Branches
- Branch per feature/fix
- Named: `feature/description` or `fix/description`
- Keep branches up-to-date with main
- Delete after merge

### Pull Requests
- One feature per PR
- Descriptive title and description
- Link related issues
- All CI checks pass
- Self-review before requesting review

## Error Handling

### Panic vs Error
- Never panic in production code
- Use errors for recoverable failures
- Document error cases
- Log errors with context

### Error Messages
- Be specific, not generic
- Include context (file, line, values)
- Actionable information
- Lower case

```go
return fmt.Errorf("failed to load checkpoint %s: %w", id, err)
```

### Logging Errors
```go
logger.Error("task execution failed",
    "taskID", taskID,
    "error", err,
)
```

## Concurrency

### Goroutines
- Document goroutine lifecycle
- No goroutine leaks
- Use context for cancellation
- Use sync.WaitGroup for coordination

### Locks
- Use channels where possible
- Minimize lock scope
- Avoid nested locks
- Document lock ordering

### Race Conditions
- Run tests with `-race` flag
- No data races allowed
- Document concurrent access patterns

## Dependencies

### Adding Dependencies
- Justify new dependencies
- Prefer stdlib over third-party
- Check project maturity and maintenance
- Update go.mod and go.sum
- Document why dependency is needed

### Removing Dependencies
- Remove unused dependencies
- Clean up go.mod regularly
- Update documentation if external behavior changes

### Versions
- Prefer stable versions
- Update regularly
- Test after updating
- Document breaking changes

## Performance

### Profiling
- Profile before optimizing
- Profile under realistic load
- Use pprof for CPU and memory
- Document performance improvements

### Benchmarks
- Add benchmarks for critical paths
- Run `go test -bench` regularly
- Track benchmark results over time
- Document performance assumptions

### Optimization Rules
1. Don't optimize prematurely
2. Measure first
3. Optimize by priority (CPU, memory, disk, network)
4. Document optimizations
5. Keep code readable

## Security

### Input Validation
- Validate all external inputs
- Validate at system boundaries (network, filesystem, external APIs)
- Trust internal code
- Document validation rules

### Secrets
- No hardcoded secrets
- Use environment variables for secrets
- Log secret names, not values
- Rotate secrets regularly

### Dependency Security
- Run `go list -json -m all | nancy sleuth` to scan dependencies
- Update dependencies promptly
- Review security advisories

## Deployment

### Binary
- Single binary, no runtime dependencies
- Strip symbols for release builds
- Version string embedded
- Cross-compile for target platforms

### Configuration
- Immutable after startup
- Environment variables for secrets
- Validate configuration on startup
- Report configuration errors clearly

### Upgrades
- No breaking changes to public APIs
- Document upgrade path
- Provide migration tools if needed
- Maintain backward compatibility

## Monitoring & Operations

### Metrics
- Export in Prometheus format
- Counter for events (increment only)
- Gauge for measurements (can go up/down)
- Histogram for distributions
- Summary for quantiles

### Logging
- Structured JSON logs
- Include context (task ID, request ID, session ID)
- Log at appropriate level
- No sensitive information in logs

### Alerting
- Define alert thresholds
- Document alert handling
- Test alert systems
- Automate responses where possible

## Checklist: Before Committing

- [ ] Code compiles without warnings
- [ ] All tests pass (`go test ./... -cover`)
- [ ] Coverage >= 80%
- [ ] Code formatted (`go fmt ./...`)
- [ ] No lint issues (`golangci-lint run`)
- [ ] No type errors (`go vet ./...`)
- [ ] No security issues (`gosec ./...`)
- [ ] No race conditions (`go test -race ./...`)
- [ ] Commit message is descriptive
- [ ] No TODOs or FIXMEs in code
- [ ] Documentation updated
- [ ] No hardcoded secrets
- [ ] No debug logging left in
- [ ] No commented-out code left in

## Code Review Checklist

Reviewer should verify:

- [ ] Follows all standards in this document
- [ ] Tests are comprehensive
- [ ] Error handling is complete
- [ ] No obvious bugs
- [ ] Performance is acceptable
- [ ] Security is good
- [ ] Code is maintainable
- [ ] Documentation is complete
- [ ] Commit messages are clear
- [ ] No unrelated changes

## Continuous Integration

### Automated Checks
- Build on every commit
- Run all tests (with coverage)
- Run linter
- Run security scanner
- Run benchmarks
- Check for race conditions
- Build binaries for all platforms

### Manual Gates
- Code review approval
- Merge only when CI is green

## Decision Making

When faced with a design decision:

1. Document the decision in a comment or commit message
2. If significant, create an ADR in `.ai/DECISIONS.md`
3. Include: context, decision, rationale, alternatives considered, trade-offs
4. Keep decisions close to code

## References

- Go Code Review Comments: https://github.com/golang/go/wiki/CodeReviewComments
- Effective Go: https://golang.org/doc/effective_go
- Go Best Practices: https://talks.golang.org/2013/bestpractices.slide
- 12 Factor App: https://12factor.net/

---

**Last Updated**: 2025-08-04 20:55 UTC
