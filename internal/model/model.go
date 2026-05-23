// Package model defines core data structures for agent-task-center.
package model

// Task represents a unit of work in the queue.
type Task struct {
	ID                 string
	WorkspaceID        *string
	WorkspaceName      *string
	WorkflowName       *string
	Step               *string
	RunID              *string
	Domain             *string
	Title              string
	Priority           int
	Context            *string
	ContextHash        *string
	VisibilityTimeoutS int
	MaxAttempts        int
	RetryBackoffS      int
	Status             string
	AssignedWorkerID   *string
	LeaseExpiresAt     *string
	RetryAfter         *string
	AttemptCount       int
	CreatedAt          string
	UpdatedAt          string
}

// TaskEvent is an event in a task's lifecycle.
type TaskEvent struct {
	ID        string
	TaskID    string
	AttemptID *string
	WorkerID  *string
	EventType string
	Payload   *string
	CreatedAt string
}

// TaskLog is a log line emitted during task execution.
type TaskLog struct {
	ID        string
	TaskID    string
	AttemptID *string
	WorkerID  *string
	Level     string
	Message   string
	CreatedAt string
}

// TaskDetail bundles a task with its events and recent logs.
type TaskDetail struct {
	Task   Task
	Events []TaskEvent
	Logs   []TaskLog
}

// TaskFilter holds query parameters for listing tasks.
type TaskFilter struct {
	Workspace    string
	Domain       string
	WorkflowName string
	Step         string
	RunID        string
	Statuses     []string
	WorkerID     string
	MinPriority  *int
	Page         int // 1-based
}

// TaskPage is a paginated list of tasks.
type TaskPage struct {
	Tasks     []Task
	Filter    TaskFilter
	Page      int
	TotalPages int
	Total     int
	Workspaces []string
	Domains    []string
	Workflows  []string
	Steps      []string
}
