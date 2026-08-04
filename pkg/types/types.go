package types

import "time"

// ExecutionContext wraps commonly needed context information.
type ExecutionContext struct {
	ProjectPath    string
	WorkspacePath  string
	ConfigPath     string
	LogPath        string
	CheckpointPath string
}

// TaskStatus represents possible task states.
type TaskStatus string

const (
	TaskStatusNew        TaskStatus = "new"
	TaskStatusPlanned    TaskStatus = "planned"
	TaskStatusInProgress TaskStatus = "in-progress"
	TaskStatusBlocked    TaskStatus = "blocked"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
)

// SystemState represents overall system health.
type SystemState string

const (
	StateRunning    SystemState = "running"
	StateRecovering SystemState = "recovering"
	StateSleeping   SystemState = "sleeping"
	StatePaused     SystemState = "paused"
	StateFailed     SystemState = "failed"
)

// Priority represents task priority levels.
type Priority int

const (
	PriorityCritical Priority = 1
	PriorityHigh     Priority = 3
	PriorityMedium   Priority = 5
	PriorityLow      Priority = 7
	PriorityTrivial  Priority = 10
)

// Stage represents execution pipeline stages.
type Stage string

const (
	StageAnalysis     Stage = "analysis"
	StagePlanning     Stage = "planning"
	StageImplementing Stage = "implementing"
	StageTesting      Stage = "testing"
	StageReviewing    Stage = "reviewing"
	StageCommitting   Stage = "committing"
	StageCompleting   Stage = "completing"
)

// NotificationLevel represents alert severity.
type NotificationLevel string

const (
	NotificationDebug    NotificationLevel = "debug"
	NotificationInfo     NotificationLevel = "info"
	NotificationWarning  NotificationLevel = "warning"
	NotificationError    NotificationLevel = "error"
	NotificationCritical NotificationLevel = "critical"
)

// Notification represents a system notification.
type Notification struct {
	Level     NotificationLevel
	Message   string
	Timestamp time.Time
	Context   map[string]interface{}
}

// Error represents a system error with context.
type Error struct {
	Code      string
	Message   string
	Timestamp time.Time
	Context   map[string]interface{}
	Cause     error
}

// Statistics represents execution statistics.
type Statistics struct {
	TasksCompleted      int
	TasksFailed         int
	AverageTaskDuration time.Duration
	TotalExecutionTime  time.Duration
	QuotaUsed           float64
	SuccessRate         float64
	LastTaskTime        time.Time
	LastCheckpointTime  time.Time
	RecoveryCount       int
}

// HealthCheck represents system health status.
type HealthCheck struct {
	Status     string // "healthy", "degraded", "unhealthy"
	Timestamp  time.Time
	Components map[string]ComponentHealth
	LastError  error
	Uptime     time.Duration
}

// ComponentHealth represents health of a subsystem.
type ComponentHealth struct {
	Name   string
	Status string // "healthy", "degraded", "unhealthy"
	Error  error
}

// ConfigSpec defines configuration structure.
type ConfigSpec struct {
	Project struct {
		Name string
		Path string
		Type string // "go", "node", "python", "rust", etc.
	}

	Claude struct {
		Enabled             bool
		Model               string
		MaxRetries          int
		TimeoutSeconds      int
		ContextWindowSize   int
		MaxTokensPerRequest int
	}

	Quota struct {
		CheckInterval         time.Duration
		WarningThreshold      float64
		ExhaustionThreshold   float64
		AutoSleepOnExhaustion bool
		SleepDuration         time.Duration
	}

	Checkpoint struct {
		Interval    time.Duration
		MaxSize     string
		Retention   int
		Compression bool
	}

	Git struct {
		CommitterName  string
		CommitterEmail string
		AutoRetry      bool
		RetryAttempts  int
		RetryBackoff   time.Duration
	}

	Tasks struct {
		MaxDuration       time.Duration
		AutoCheckpoint    bool
		AutoCommit        bool
		ParallelExecution bool
	}

	Logging struct {
		Level     string
		Format    string // "json", "text"
		Output    string
		Retention int // days
	}

	Metrics struct {
		Enabled        bool
		ExportInterval time.Duration
		ExportPath     string
		PrometheusPort int
	}

	Plugins struct {
		Enabled   bool
		Directory string
	}

	Dashboard struct {
		Enabled bool
		Port    int
		Address string
	}
}

// Event represents a system event.
type Event struct {
	ID        string
	Type      string // "task_started", "task_completed", "quota_low", etc.
	Timestamp time.Time
	Payload   map[string]interface{}
	Source    string
}

// AuditEntry represents an action for audit trail.
type AuditEntry struct {
	ID        string
	Action    string
	Actor     string
	Timestamp time.Time
	Details   map[string]interface{}
	Status    string // "success", "failure"
	Error     error
}
