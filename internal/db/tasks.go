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
	if f.TaskType != "" {
		conds = append(conds, "tt.name = ?")
		args = append(args, f.TaskType)
	}
	if len(f.Statuses) > 0 {
		ph := strings.Repeat("?,", len(f.Statuses))
		ph = ph[:len(ph)-1]
		conds = append(conds, fmt.Sprintf("t.status IN (%s)", ph))
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	if f.AgentID != "" {
		conds = append(conds, "(t.assigned_agent_id LIKE ? OR a.name LIKE ?)")
		like := "%" + f.AgentID + "%"
		args = append(args, like, like)
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
		LEFT JOIN workspaces w  ON t.workspace_id = w.id
		LEFT JOIN task_types tt ON t.task_type_id  = tt.id
		LEFT JOIN agents a      ON t.assigned_agent_id = a.id
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
		SELECT t.id, t.workspace_id, w.name, t.domain, t.task_type_id, tt.name,
		       t.title, t.priority, t.context, t.status, t.assigned_agent_id,
		       t.lease_expires_at, t.retry_after, t.attempt_count, t.created_at, t.updated_at
		`+baseQ+`
		ORDER BY t.priority DESC, t.created_at ASC
		LIMIT ? OFFSET ?`, append(args, pageSize, offset)...)
	if err != nil {
		return model.TaskPage{}, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []model.Task
	for rows.Next() {
		var tk model.Task
		if err := rows.Scan(
			&tk.ID, &tk.WorkspaceID, &tk.WorkspaceName, &tk.Domain,
			&tk.TaskTypeID, &tk.TaskTypeName,
			&tk.Title, &tk.Priority, &tk.Context, &tk.Status,
			&tk.AssignedAgentID, &tk.LeaseExpiresAt, &tk.RetryAfter,
			&tk.AttemptCount, &tk.CreatedAt, &tk.UpdatedAt,
		); err != nil {
			return model.TaskPage{}, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, tk)
	}

	workspaces, _ := distinctStrings(db, "SELECT DISTINCT name FROM workspaces ORDER BY name")
	domains, _ := distinctStrings(db, "SELECT DISTINCT domain FROM tasks WHERE domain IS NOT NULL ORDER BY domain")
	taskTypes, _ := distinctStrings(db, "SELECT DISTINCT name FROM task_types ORDER BY name")

	return model.TaskPage{
		Tasks:      tasks,
		Filter:     f,
		Page:       page,
		TotalPages: totalPages,
		Total:      total,
		Workspaces: workspaces,
		Domains:    domains,
		TaskTypes:  taskTypes,
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
	var tk model.Task
	err := db.QueryRow(`
		SELECT t.id, t.workspace_id, w.name, t.domain, t.task_type_id, tt.name,
		       t.title, t.priority, t.context, t.status, t.assigned_agent_id,
		       t.lease_expires_at, t.retry_after, t.attempt_count, t.created_at, t.updated_at
		FROM tasks t
		LEFT JOIN workspaces w  ON t.workspace_id = w.id
		LEFT JOIN task_types tt ON t.task_type_id  = tt.id
		WHERE t.id = ?`, id).Scan(
		&tk.ID, &tk.WorkspaceID, &tk.WorkspaceName, &tk.Domain,
		&tk.TaskTypeID, &tk.TaskTypeName,
		&tk.Title, &tk.Priority, &tk.Context, &tk.Status,
		&tk.AssignedAgentID, &tk.LeaseExpiresAt, &tk.RetryAfter,
		&tk.AttemptCount, &tk.CreatedAt, &tk.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return model.TaskDetail{}, fmt.Errorf("not_found")
	}
	if err != nil {
		return model.TaskDetail{}, fmt.Errorf("get task: %w", err)
	}

	evRows, err := db.Query(`
		SELECT id, task_id, attempt_id, agent_id, event_type, payload, created_at
		FROM task_events WHERE task_id = ? ORDER BY created_at ASC`, id)
	if err != nil {
		return model.TaskDetail{}, fmt.Errorf("get events: %w", err)
	}
	defer evRows.Close()
	var events []model.TaskEvent
	for evRows.Next() {
		var ev model.TaskEvent
		if err := evRows.Scan(&ev.ID, &ev.TaskID, &ev.AttemptID, &ev.AgentID,
			&ev.EventType, &ev.Payload, &ev.CreatedAt); err == nil {
			events = append(events, ev)
		}
	}

	logRows, err := db.Query(`
		SELECT id, task_id, attempt_id, agent_id, level, message, created_at
		FROM task_logs WHERE task_id = ? ORDER BY created_at DESC LIMIT 50`, id)
	if err != nil {
		return model.TaskDetail{}, fmt.Errorf("get logs: %w", err)
	}
	defer logRows.Close()
	var logs []model.TaskLog
	for logRows.Next() {
		var lg model.TaskLog
		if err := logRows.Scan(&lg.ID, &lg.TaskID, &lg.AttemptID, &lg.AgentID,
			&lg.Level, &lg.Message, &lg.CreatedAt); err == nil {
			logs = append(logs, lg)
		}
	}

	return model.TaskDetail{Task: tk, Events: events, Logs: logs}, nil
}
