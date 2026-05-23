package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/t01buddy/agent-task-center/internal/model"
)

const pageSize = 50

// ListTasks returns a paginated, filtered list of tasks.
func ListTasks(db *sql.DB, f model.TaskFilter) (model.TaskPage, error) {
	args := []any{}
	conds := []string{}

	if f.Workspace != "" {
		conds = append(conds, "w.name = ?")
		args = append(args, f.Workspace)
	}
	if f.Domain != "" {
		conds = append(conds, "t.domain = ?")
		args = append(args, f.Domain)
	}
	if f.WorkflowName != "" {
		conds = append(conds, "t.workflow_name = ?")
		args = append(args, f.WorkflowName)
	}
	if f.Step != "" {
		conds = append(conds, "t.step = ?")
		args = append(args, f.Step)
	}
	if f.RunID != "" {
		conds = append(conds, "t.run_id = ?")
		args = append(args, f.RunID)
	}
	if len(f.Statuses) > 0 {
		ph := strings.Repeat("?,", len(f.Statuses))
		ph = ph[:len(ph)-1]
		conds = append(conds, fmt.Sprintf("t.status IN (%s)", ph))
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	if f.WorkerID != "" {
		conds = append(conds, "t.assigned_worker_id LIKE ?")
		args = append(args, "%"+f.WorkerID+"%")
	}
	if f.MinPriority != nil {
		conds = append(conds, "t.priority >= ?")
		args = append(args, *f.MinPriority)
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	baseQ := fmt.Sprintf(`
		FROM tasks t
		LEFT JOIN workspaces w ON t.workspace_id = w.id
		%s`, where)

	var total int
	if err := db.QueryRow("SELECT COUNT(*) "+baseQ, args...).Scan(&total); err != nil {
		return model.TaskPage{}, fmt.Errorf("count tasks: %w", err)
	}

	totalPages := (total + pageSize - 1) / pageSize
	page := f.Page
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize

	rows, err := db.Query(`
		SELECT t.id, t.workspace_id, w.name, t.workflow_name, t.step, t.run_id,
		       t.domain, t.title, t.priority, t.context, t.context_hash,
		       t.visibility_timeout_s, t.max_attempts, t.retry_backoff_s,
		       t.status, t.assigned_worker_id, t.lease_expires_at, t.retry_after,
		       t.attempt_count, t.created_at, t.updated_at
		`+baseQ+`
		ORDER BY t.priority DESC, t.created_at ASC
		LIMIT ? OFFSET ?`, append(args, pageSize, offset)...)
	if err != nil {
		return model.TaskPage{}, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []model.Task
	for rows.Next() {
		tk, err := scanTask(rows)
		if err != nil {
			return model.TaskPage{}, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, tk)
	}

	workspaces, _ := distinctStrings(db, "SELECT DISTINCT name FROM workspaces ORDER BY name")
	domains, _ := distinctStrings(db, "SELECT DISTINCT domain FROM tasks WHERE domain IS NOT NULL ORDER BY domain")
	workflows, _ := distinctStrings(db, "SELECT DISTINCT workflow_name FROM tasks WHERE workflow_name IS NOT NULL ORDER BY workflow_name")
	steps, _ := distinctStrings(db, "SELECT DISTINCT step FROM tasks WHERE step IS NOT NULL ORDER BY step")

	return model.TaskPage{
		Tasks:      tasks,
		Filter:     f,
		Page:       page,
		TotalPages: totalPages,
		Total:      total,
		Workspaces: workspaces,
		Domains:    domains,
		Workflows:  workflows,
		Steps:      steps,
	}, nil
}

func distinctStrings(db *sql.DB, q string) ([]string, error) {
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil {
			out = append(out, s)
		}
	}
	return out, nil
}

// GetTaskDetail fetches a task with its events and last 50 logs.
func GetTaskDetail(db *sql.DB, id string) (model.TaskDetail, error) {
	tk, err := GetTaskByID(db, id)
	if err != nil {
		return model.TaskDetail{}, err
	}

	evRows, err := db.Query(`
		SELECT id, task_id, attempt_id, worker_id, event_type, payload, created_at
		FROM task_events WHERE task_id = ? ORDER BY created_at ASC`, id)
	if err != nil {
		return model.TaskDetail{}, fmt.Errorf("get events: %w", err)
	}
	defer evRows.Close()
	var events []model.TaskEvent
	for evRows.Next() {
		var ev model.TaskEvent
		var attemptID, workerID sql.NullString
		var payload sql.NullString
		if err := evRows.Scan(&ev.ID, &ev.TaskID, &attemptID, &workerID,
			&ev.EventType, &payload, &ev.CreatedAt); err == nil {
			if attemptID.Valid {
				ev.AttemptID = &attemptID.String
			}
			if workerID.Valid {
				ev.WorkerID = &workerID.String
			}
			if payload.Valid {
				ev.Payload = &payload.String
			}
			events = append(events, ev)
		}
	}

	logRows, err := db.Query(`
		SELECT id, task_id, attempt_id, worker_id, level, message, created_at
		FROM task_logs WHERE task_id = ? ORDER BY created_at DESC LIMIT 50`, id)
	if err != nil {
		return model.TaskDetail{}, fmt.Errorf("get logs: %w", err)
	}
	defer logRows.Close()
	var logs []model.TaskLog
	for logRows.Next() {
		var lg model.TaskLog
		var attemptID, workerID sql.NullString
		if err := logRows.Scan(&lg.ID, &lg.TaskID, &attemptID, &workerID,
			&lg.Level, &lg.Message, &lg.CreatedAt); err == nil {
			if attemptID.Valid {
				lg.AttemptID = &attemptID.String
			}
			if workerID.Valid {
				lg.WorkerID = &workerID.String
			}
			logs = append(logs, lg)
		}
	}

	return model.TaskDetail{Task: tk, Events: events, Logs: logs}, nil
}

// GetTaskByID fetches a single task row by ID.
func GetTaskByID(db *sql.DB, id string) (model.Task, error) {
	row := db.QueryRow(`
		SELECT t.id, t.workspace_id, w.name, t.workflow_name, t.step, t.run_id,
		       t.domain, t.title, t.priority, t.context, t.context_hash,
		       t.visibility_timeout_s, t.max_attempts, t.retry_backoff_s,
		       t.status, t.assigned_worker_id, t.lease_expires_at, t.retry_after,
		       t.attempt_count, t.created_at, t.updated_at
		FROM tasks t
		LEFT JOIN workspaces w ON t.workspace_id = w.id
		WHERE t.id = ?`, id)
	tk, err := scanTask(row)
	if err == sql.ErrNoRows {
		return model.Task{}, fmt.Errorf("not_found")
	}
	return tk, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(s scanner) (model.Task, error) {
	var tk model.Task
	var workspaceID, workspaceName, workflowName, step, runID sql.NullString
	var domain, context, contextHash, assignedWorkerID, leaseExpiresAt, retryAfter sql.NullString
	err := s.Scan(
		&tk.ID, &workspaceID, &workspaceName, &workflowName, &step, &runID,
		&domain, &tk.Title, &tk.Priority, &context, &contextHash,
		&tk.VisibilityTimeoutS, &tk.MaxAttempts, &tk.RetryBackoffS,
		&tk.Status, &assignedWorkerID, &leaseExpiresAt, &retryAfter,
		&tk.AttemptCount, &tk.CreatedAt, &tk.UpdatedAt,
	)
	if err != nil {
		return model.Task{}, err
	}
	if workspaceID.Valid {
		tk.WorkspaceID = &workspaceID.String
	}
	if workspaceName.Valid {
		tk.WorkspaceName = &workspaceName.String
	}
	if workflowName.Valid {
		tk.WorkflowName = &workflowName.String
	}
	if step.Valid {
		tk.Step = &step.String
	}
	if runID.Valid {
		tk.RunID = &runID.String
	}
	if domain.Valid {
		tk.Domain = &domain.String
	}
	if context.Valid {
		tk.Context = &context.String
	}
	if contextHash.Valid {
		tk.ContextHash = &contextHash.String
	}
	if assignedWorkerID.Valid {
		tk.AssignedWorkerID = &assignedWorkerID.String
	}
	if leaseExpiresAt.Valid {
		tk.LeaseExpiresAt = &leaseExpiresAt.String
	}
	if retryAfter.Valid {
		tk.RetryAfter = &retryAfter.String
	}
	return tk, nil
}
