// Package model defines core data structures for agent-task-center.
package model

// Task represents a unit of work in the queue.
type Task struct {
	ID              string
	WorkspaceID     *string
	WorkspaceName   *string
	Domain          *string
	TaskTypeID      *string
	TaskTypeName    *string
	Title           string
	Priority        int
	Context         *string
	Status          string
	AssignedAgentID *string
	LeaseExpiresAt  *string
	RetryAfter      *string
	AttemptCount    int
	CreatedAt       string
	UpdatedAt       string
}

// TaskEvent is an event in a task's lifecycle.
type TaskEvent struct {
	ID        string
	TaskID    string
	AttemptID *string
	AgentID   *string
	EventType string
	Payload   *string
	CreatedAt string
}

// TaskLog is a log line emitted during task execution.
type TaskLog struct {
	ID        string
	TaskID    string
	AttemptID *string
	AgentID   *string
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
	Workspace   string
	Domain      string
	TaskType    string
	Statuses    []string
	AgentID     string
	MinPriority *int
	Page        int // 1-based
}

// TaskPage is a paginated list of tasks.
type TaskPage struct {
	Tasks      []Task
	Filter     TaskFilter
	Page       int
	TotalPages int
	Total      int
	Workspaces []string
	Domains    []string
	TaskTypes  []string
}
