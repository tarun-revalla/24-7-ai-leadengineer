package interfaces

import (
	"context"
	"time"
)

// Logger provides structured logging capabilities.
type Logger interface {
	Debug(msg string, fields ...interface{})
	Info(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	Fatal(msg string, fields ...interface{})
}

// Config provides access to system configuration.
type Config interface {
	GetString(key string) string
	GetInt(key string) int
	GetBool(key string) bool
	GetDuration(key string) time.Duration
	GetStringSlice(key string) []string
}

// ProjectAnalyzer analyzes project structure and characteristics.
type ProjectAnalyzer interface {
	Analyze(ctx context.Context, projectPath string) (*ProjectAnalysis, error)
	DetectLanguage(ctx context.Context, projectPath string) (string, error)
	DetectBuildSystem(ctx context.Context, projectPath string) (string, error)
}

// ProjectAnalysis contains analysis results.
type ProjectAnalysis struct {
	Language     string
	BuildSystem  string
	RootPath     string
	SourceDirs   []string
	TestDirs     []string
	Dependencies []Dependency
	EntryPoints  []string
	AnalyzedAt   time.Time
}

// Dependency represents a project dependency.
type Dependency struct {
	Name    string
	Version string
	Type    string // "direct" or "transitive"
}

// Memory manages persistent project state.
type Memory interface {
	// Read operations
	GetProject(ctx context.Context) (*ProjectMetadata, error)
	GetBacklog(ctx context.Context) (*Backlog, error)
	GetCurrent(ctx context.Context) (*CurrentTask, error)
	GetChangelog(ctx context.Context) (*Changelog, error)
	GetDecisions(ctx context.Context) ([]Decision, error)
	ReadFile(ctx context.Context, name string) (string, error)

	// Write operations
	SaveProject(ctx context.Context, proj *ProjectMetadata) error
	SaveBacklog(ctx context.Context, backlog *Backlog) error
	SaveCurrent(ctx context.Context, current *CurrentTask) error
	SaveChangelog(ctx context.Context, changelog *Changelog) error
	SaveDecision(ctx context.Context, decision Decision) error
	WriteFile(ctx context.Context, name string, content string) error

	// Sync
	SyncWithClaude(ctx context.Context) error
}

// ProjectMetadata contains high-level project information.
type ProjectMetadata struct {
	Name        string
	Description string
	Purpose     string
	Constraints []string
	UpdatedAt   time.Time
}

// Backlog represents the work queue.
type Backlog struct {
	Tasks     []BacklogTask
	UpdatedAt time.Time
}

// BacklogTask represents a single task.
type BacklogTask struct {
	ID          string
	Title       string
	Description string
	Priority    int    // 1=highest, 10=lowest
	Status      string // "new", "in-progress", "blocked", "done"
	Estimate    time.Duration
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CurrentTask represents the task currently being executed.
type CurrentTask struct {
	TaskID     string
	Title      string
	Plan       string
	Progress   float64 // 0.0 to 1.0
	StartedAt  time.Time
	Checkpoint string
	Status     string // "not-started", "planning", "implementing", "testing", "reviewing", "committing"
	Errors     []string
	UpdatedAt  time.Time
}

// Changelog tracks completed work.
type Changelog struct {
	Entries   []ChangelogEntry
	UpdatedAt time.Time
}

// ChangelogEntry represents a completed task.
type ChangelogEntry struct {
	TaskID  string
	Title   string
	Date    time.Time
	Commit  string
	Summary string
}

// Decision represents an architectural decision.
type Decision struct {
	ID        string
	Title     string
	Status    string // "proposed", "accepted", "rejected", "superseded"
	Context   string
	Rationale string
	Date      time.Time
}

// ClaudeSessionManager manages Claude Code sessions.
type ClaudeSessionManager interface {
	// Session lifecycle
	LaunchSession(ctx context.Context, prompt string) (*SessionResult, error)
	ResumeSession(ctx context.Context, sessionID string) (*SessionResult, error)
	EndSession(ctx context.Context, sessionID string) error

	// Status and monitoring
	GetCurrentSessionID() string
	GetSessionStatus(ctx context.Context, sessionID string) (*SessionStatus, error)
	IsQuotaExhausted(ctx context.Context) (bool, error)
	GetQuotaRemaining(ctx context.Context) (float64, error)

	// Recovery
	DetectFailure(ctx context.Context) (FailureType, error)
	RecoverFromFailure(ctx context.Context, failureType FailureType) error
}

// SessionResult contains results from Claude execution.
type SessionResult struct {
	SessionID  string
	Output     string
	Errors     []string
	ExecutedAt time.Time
	Duration   time.Duration
	TokensUsed int
	QuotaUsed  float64
}

// SessionStatus represents current session state.
type SessionStatus struct {
	SessionID      string
	IsActive       bool
	LastActivity   time.Time
	TokensUsed     int
	QuotaRemaining float64
	QuotaResetTime time.Time
}

// FailureType categorizes failures.
type FailureType string

const (
	FailureTypeCrash        FailureType = "crash"
	FailureTypeQuota        FailureType = "quota_exhausted"
	FailureTypeNetwork      FailureType = "network"
	FailureTypeGit          FailureType = "git"
	FailureTypeInterruption FailureType = "interruption"
	FailureTypeTimeout      FailureType = "timeout"
)

// CheckpointManager manages system state checkpoints.
type CheckpointManager interface {
	// Checkpoint operations
	CreateCheckpoint(ctx context.Context, state *CheckpointState) (string, error)
	LoadCheckpoint(ctx context.Context, checkpointID string) (*CheckpointState, error)
	ListCheckpoints(ctx context.Context) ([]CheckpointMetadata, error)
	DeleteCheckpoint(ctx context.Context, checkpointID string) error

	// Recovery
	GetLatestCheckpoint(ctx context.Context) (*CheckpointState, error)
	ValidateCheckpoint(ctx context.Context, checkpointID string) error

	// Cleanup
	PruneOldCheckpoints(ctx context.Context, keepCount int) error
}

// CheckpointState represents a complete system state snapshot.
type CheckpointState struct {
	ID             string
	Timestamp      time.Time
	TaskID         string
	TaskState      string
	Progress       float64
	GitCommit      string
	MemorySnapshot interface{}
	QuotaState     *QuotaSnapshot
	Metrics        map[string]interface{}
}

// CheckpointMetadata contains checkpoint information.
type CheckpointMetadata struct {
	ID        string
	Timestamp time.Time
	TaskID    string
	Size      int64
}

// QuotaSnapshot represents quota state at checkpoint time.
type QuotaSnapshot struct {
	Remaining   float64
	ResetTime   time.Time
	WindowStart time.Time
	WindowEnd   time.Time
}

// GitManager handles git operations.
type GitManager interface {
	// Commit operations
	Stage(ctx context.Context, paths []string) error
	Commit(ctx context.Context, message string) (string, error)
	Push(ctx context.Context, branch string) error
	Pull(ctx context.Context, branch string) error

	// Status and history
	Status(ctx context.Context) (*GitStatus, error)
	GetLastCommit(ctx context.Context) (*GitCommit, error)
	GetCommitHistory(ctx context.Context, limit int) ([]GitCommit, error)

	// Conflict resolution
	HasConflicts(ctx context.Context) (bool, error)
	ResolveConflict(ctx context.Context, strategy ConflictStrategy) error
	GetConflictedFiles(ctx context.Context) ([]string, error)
}

// GitStatus represents current git state.
type GitStatus struct {
	Branch          string
	IsClean         bool
	StagedChanges   []string
	UnstagedChanges []string
	UntrackedFiles  []string
	HasConflicts    bool
	AheadOfOrigin   int
	BehindOrigin    int
}

// GitCommit represents a git commit.
type GitCommit struct {
	Hash      string
	Author    string
	Message   string
	Timestamp time.Time
}

// ConflictStrategy defines how to handle git conflicts.
type ConflictStrategy string

const (
	ConflictStrategyOurs   ConflictStrategy = "ours"
	ConflictStrategyTheirs ConflictStrategy = "theirs"
	ConflictStrategyManual ConflictStrategy = "manual"
)

// Planner creates implementation plans for tasks.
type Planner interface {
	// Planning
	AnalyzeTask(ctx context.Context, task *BacklogTask) (*TaskAnalysis, error)
	CreatePlan(ctx context.Context, task *BacklogTask) (*ImplementationPlan, error)
	EstimateEffort(ctx context.Context, task *BacklogTask) (time.Duration, error)

	// Project understanding
	AnalyzeProjectState(ctx context.Context) (*ProjectState, error)
	IdentifyDependencies(ctx context.Context, task *BacklogTask) ([]string, error)
	GetRisks(ctx context.Context, task *BacklogTask) ([]Risk, error)
}

// TaskAnalysis contains analysis of a task.
type TaskAnalysis struct {
	TaskID        string
	Complexity    string // "low", "medium", "high"
	Dependencies  []string
	Risks         []Risk
	Blockers      []string
	EstimatedTime time.Duration
	AnalyzedAt    time.Time
}

// ImplementationPlan outlines how to implement a task.
type ImplementationPlan struct {
	TaskID       string
	Title        string
	Steps        []PlanStep
	CheckPoints  []string
	RollbackPlan string
	CreatedAt    time.Time
}

// PlanStep represents a single step in implementation.
type PlanStep struct {
	ID          string
	Title       string
	Description string
	Order       int
	Validation  string
	Duration    time.Duration
}

// Risk represents a potential problem.
type Risk struct {
	ID          string
	Description string
	Likelihood  string // "low", "medium", "high"
	Impact      string // "low", "medium", "high"
	Mitigation  string
}

// ProjectState represents current project conditions.
type ProjectState struct {
	Language      string
	BuildSystem   string
	TestCoverage  float64
	LintScore     float64
	SecurityScore float64
	TechnicalDebt []string
	LastAnalysis  time.Time
}

// TaskExecutor executes tasks end-to-end.
type TaskExecutor interface {
	// Task execution
	ExecuteTask(ctx context.Context, task *BacklogTask) (*TaskResult, error)
	ContinueTask(ctx context.Context, taskID string) (*TaskResult, error)

	// Monitoring
	GetProgress(ctx context.Context, taskID string) (*TaskProgress, error)
	CancelTask(ctx context.Context, taskID string) error
}

// TaskResult contains task execution results.
type TaskResult struct {
	TaskID        string
	Success       bool
	Output        string
	Errors        []string
	ArtifactsPath string
	Duration      time.Duration
	StartedAt     time.Time
	CompletedAt   time.Time
	CommitHash    string
}

// TaskProgress represents current execution progress.
type TaskProgress struct {
	TaskID          string
	PercentComplete float64
	CurrentStep     string
	StartedAt       time.Time
	EstimatedEnd    time.Time
	Errors          []string
}

// QualityGate verifies code quality.
type QualityGate interface {
	// Verification
	Verify(ctx context.Context, path string) (*QualityResult, error)
	IsPassing(ctx context.Context, path string) (bool, error)

	// Details
	GetName() string
	GetDescription() string
}

// QualityResult contains gate verification results.
type QualityResult struct {
	GateName   string
	Passed     bool
	Duration   time.Duration
	Findings   []Finding
	Details    map[string]interface{}
	ExecutedAt time.Time
}

// Finding represents a quality finding.
type Finding struct {
	Level      string // "info", "warning", "error"
	Message    string
	Location   string // file:line:column
	Suggestion string
}

// Reviewer performs self-review from multiple perspectives.
type Reviewer interface {
	// Review operations
	ReviewCode(ctx context.Context, diff string) (*CodeReview, error)
	ReviewDesign(ctx context.Context, description string) (*DesignReview, error)
	ReviewSecurity(ctx context.Context, codeChanges string) (*SecurityReview, error)

	// Approval
	IsApproved(ctx context.Context, reviews ...*CodeReview) (bool, error)
}

// CodeReview represents a code review from one perspective.
type CodeReview struct {
	Perspective    string // "developer", "reviewer", "security", "performance", "qa", "documentation"
	Approved       bool
	Findings       []ReviewFinding
	SuggestChanges string
	ExecutedAt     time.Time
}

// ReviewFinding represents a review finding.
type ReviewFinding struct {
	Severity    string // "minor", "major", "critical"
	Category    string
	Description string
	Location    string
	Suggestion  string
}

// DesignReview represents a design review.
type DesignReview struct {
	Approved    bool
	IsClean     bool
	Issues      []string
	Suggestions []string
}

// SecurityReview represents security findings.
type SecurityReview struct {
	Approved             bool
	VulnerabilitiesFound int
	Vulnerabilities      []Vulnerability
	Recommendations      []string
}

// Vulnerability represents a security issue.
type Vulnerability struct {
	ID          string
	Type        string
	Severity    string // "low", "medium", "high", "critical"
	Description string
	Location    string
	Fix         string
}

// RecoveryManager handles system recovery.
type RecoveryManager interface {
	// Detection
	DetectFailure(ctx context.Context) (FailureType, error)
	NeedsRecovery(ctx context.Context) (bool, error)

	// Recovery
	Recover(ctx context.Context) error
	RecoverFromCheckpoint(ctx context.Context, checkpointID string) error
	ValidateRecovery(ctx context.Context) error
}

// MetricsCollector collects and exports metrics.
type MetricsCollector interface {
	// Recording
	RecordTaskDuration(taskID string, duration time.Duration)
	RecordTaskSuccess(taskID string)
	RecordTaskFailure(taskID string, reason string)
	RecordQuotaUsage(quotaUsed float64)

	// Reporting
	GetMetrics(ctx context.Context) (*SystemMetrics, error)
	ExportMetrics(ctx context.Context, path string) error
}

// SystemMetrics contains system-wide metrics.
type SystemMetrics struct {
	TasksCompleted      int
	TasksFailed         int
	SuccessRate         float64
	AverageTaskTime     time.Duration
	QuotaUsedTotal      float64
	SessionsStarted     int
	CrashesDuringRun    int
	AutoRecoveriesCount int
	LastRecoveredAt     time.Time
	LastCheckpointAt    time.Time
}

// Runner executes Claude instructions.
type Runner interface {
	// Execution
	Run(ctx context.Context, prompt string) (*RunResult, error)
	RunWithMemory(ctx context.Context, prompt string, memory *ProjectMemory) (*RunResult, error)

	// Monitoring
	GetCurrentSession() string
	IsRunning() bool
}

// RunResult contains results from Claude execution.
type RunResult struct {
	SessionID  string
	Success    bool
	Output     string
	Errors     []string
	Duration   time.Duration
	TokensUsed int
	ExecutedAt time.Time
}

// ProjectMemory represents project context for Claude.
type ProjectMemory struct {
	ProjectMetadata *ProjectMetadata
	CurrentState    *ProjectState
	RecentChanges   []string
	Backlog         *Backlog
	CurrentTask     *CurrentTask
	Decisions       []Decision
}
