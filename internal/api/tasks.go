package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Task is a single task row returned from the API.
type Task struct {
	ID              string  `json:"id"`
	WorkspaceID     *string `json:"workspace_id"`
	Domain          *string `json:"domain"`
	TaskTypeID      *string `json:"task_type_id"`
	Title           string  `json:"title"`
	Priority        int     `json:"priority"`
	Context         *string `json:"context"`
	Status          string  `json:"status"`
	AssignedAgentID *string `json:"assigned_agent_id"`
	LeaseExpiresAt  *string `json:"lease_expires_at"`
	AttemptCount    int     `json:"attempt_count"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// TasksRouterHandler routes /api/tasks/{id} and sub-paths.
// Routes: /events → events, /heartbeat → heartbeat, /complete → complete, /fail → fail,
// bare /{id} → PATCH/DELETE by ID handler.
func TasksRouterHandler(db *sql.DB) http.HandlerFunc {
	eventsHandler := TaskEventsHandler(db)
	byIDHandler := TaskByIDHandler(db)
	heartbeatHandler := HeartbeatHandler(db)
	completeHandler := CompleteHandler(db)
	failHandler := FailHandler(db)
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
		switch {
		case strings.HasSuffix(path, "/events"):
			eventsHandler(w, r)
		case strings.HasSuffix(path, "/heartbeat"):
			heartbeatHandler(w, r)
		case strings.HasSuffix(path, "/complete"):
			completeHandler(w, r)
		case strings.HasSuffix(path, "/fail"):
			failHandler(w, r)
		default:
			byIDHandler(w, r)
		}
	}
}

// TasksHandler handles POST /api/tasks and GET /api/tasks.
func TasksHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleCreateTask(db, w, r)
		case http.MethodGet:
			handleListTasks(db, w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		}
	}
}

// TaskByIDHandler handles PATCH /api/tasks/{id} and DELETE /api/tasks/{id}.
func TaskByIDHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract task ID from path: /api/tasks/<id>
		id := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
		id = strings.TrimSpace(id)
		if id == "" || strings.Contains(id, "/") {
			writeError(w, http.StatusBadRequest, "missing task id")
			return
		}
		switch r.Method {
		case http.MethodPatch:
			handleUpdateTask(db, w, r, id)
		case http.MethodDelete:
			handleCancelTask(db, w, r, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		}
	}
}

func handleCreateTask(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string  `json:"title"`
		WorkspaceID *string `json:"workspace_id"`
		Domain      *string `json:"domain"`
		TaskTypeID  *string `json:"task_type_id"`
		Priority    int     `json:"priority"`
		Context     any     `json:"context"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	var contextStr *string
	if req.Context != nil {
		b, _ := json.Marshal(req.Context)
		s := string(b)
		contextStr = &s
	}

	id := newID()
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.Exec(
		`INSERT INTO tasks (id, workspace_id, domain, task_type_id, title, priority, context, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'queued', ?, ?)`,
		id, req.WorkspaceID, req.Domain, req.TaskTypeID, req.Title, req.Priority, contextStr, now, now,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	task, err := getTaskByID(db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func handleListTasks(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := 50
	if l := q.Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			if v > 200 {
				v = 200
			}
			limit = v
		}
	}
	offset := 0
	if o := q.Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	where := []string{"1=1"}
	args := []any{}

	if v := q.Get("workspace_id"); v != "" {
		where = append(where, "workspace_id = ?")
		args = append(args, v)
	}
	if v := q.Get("domain"); v != "" {
		where = append(where, "domain = ?")
		args = append(args, v)
	}
	if v := q.Get("task_type_id"); v != "" {
		where = append(where, "task_type_id = ?")
		args = append(args, v)
	}
	if v := q.Get("assigned_agent_id"); v != "" {
		where = append(where, "assigned_agent_id = ?")
		args = append(args, v)
	}
	if v := q.Get("priority_gte"); v != "" {
		if pv, err := strconv.Atoi(v); err == nil {
			where = append(where, "priority >= ?")
			args = append(args, pv)
		}
	}
	if v := q.Get("status"); v != "" {
		parts := strings.Split(v, ",")
		placeholders := make([]string, len(parts))
		for i, p := range parts {
			placeholders[i] = "?"
			args = append(args, strings.TrimSpace(p))
		}
		where = append(where, "status IN ("+strings.Join(placeholders, ",")+")")
	}

	whereClause := strings.Join(where, " AND ")

	// Count total
	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	if err := db.QueryRow("SELECT COUNT(*) FROM tasks WHERE "+whereClause, countArgs...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	// Fetch page
	pageArgs := append(args, limit, offset)
	rows, err := db.Query(
		`SELECT id, workspace_id, domain, task_type_id, title, priority, context, status,
		        assigned_agent_id, lease_expires_at, attempt_count, created_at, updated_at
		 FROM tasks
		 WHERE `+whereClause+`
		 ORDER BY priority DESC, created_at ASC
		 LIMIT ? OFFSET ?`,
		pageArgs...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	defer rows.Close()

	tasks := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan error: "+err.Error())
			return
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks, "total": total})
}

func handleUpdateTask(db *sql.DB, w http.ResponseWriter, r *http.Request, id string) {
	// Check current status
	var status string
	err := db.QueryRow("SELECT status FROM tasks WHERE id = ?", id).Scan(&status)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	if status != "queued" && status != "blocked" {
		writeError(w, http.StatusConflict, "task can only be updated when queued or blocked")
		return
	}

	var req struct {
		Title       *string `json:"title"`
		Priority    *int    `json:"priority"`
		Context     any     `json:"context"`
		Domain      *string `json:"domain"`
		WorkspaceID *string `json:"workspace_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	sets := []string{}
	args := []any{}
	if req.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *req.Title)
	}
	if req.Priority != nil {
		sets = append(sets, "priority = ?")
		args = append(args, *req.Priority)
	}
	if req.Context != nil {
		b, _ := json.Marshal(req.Context)
		sets = append(sets, "context = ?")
		args = append(args, string(b))
	}
	if req.Domain != nil {
		sets = append(sets, "domain = ?")
		args = append(args, *req.Domain)
	}
	if req.WorkspaceID != nil {
		sets = append(sets, "workspace_id = ?")
		args = append(args, *req.WorkspaceID)
	}

	if len(sets) == 0 {
		// No fields to update — return current task
		task, err := getTaskByID(db, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, task)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	sets = append(sets, "updated_at = ?")
	args = append(args, now)
	args = append(args, id)

	_, err = db.Exec("UPDATE tasks SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	task, err := getTaskByID(db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func handleCancelTask(db *sql.DB, w http.ResponseWriter, r *http.Request, id string) {
	var status string
	err := db.QueryRow("SELECT status FROM tasks WHERE id = ?", id).Scan(&status)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	if status == "leased" {
		writeError(w, http.StatusConflict, "task is currently leased")
		return
	}
	if status != "queued" && status != "blocked" {
		writeError(w, http.StatusConflict, "task can only be cancelled when queued or blocked")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec("UPDATE tasks SET status = 'cancelled', updated_at = ? WHERE id = ?", now, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func getTaskByID(db *sql.DB, id string) (Task, error) {
	row := db.QueryRow(
		`SELECT id, workspace_id, domain, task_type_id, title, priority, context, status,
		        assigned_agent_id, lease_expires_at, attempt_count, created_at, updated_at
		 FROM tasks WHERE id = ?`, id,
	)
	return scanTask(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(s scanner) (Task, error) {
	var t Task
	var workspaceID, domain, taskTypeID, context, assignedAgentID, leaseExpiresAt sql.NullString
	err := s.Scan(
		&t.ID, &workspaceID, &domain, &taskTypeID, &t.Title, &t.Priority,
		&context, &t.Status, &assignedAgentID, &leaseExpiresAt,
		&t.AttemptCount, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return Task{}, err
	}
	if workspaceID.Valid {
		t.WorkspaceID = &workspaceID.String
	}
	if domain.Valid {
		t.Domain = &domain.String
	}
	if taskTypeID.Valid {
		t.TaskTypeID = &taskTypeID.String
	}
	if context.Valid {
		t.Context = &context.String
	}
	if assignedAgentID.Valid {
		t.AssignedAgentID = &assignedAgentID.String
	}
	if leaseExpiresAt.Valid {
		t.LeaseExpiresAt = &leaseExpiresAt.String
	}
	return t, nil
}
