package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Task is a single task row returned from the API.
type Task struct {
	ID                 string  `json:"id"`
	WorkspaceID        *string `json:"workspace_id"`
	WorkflowName       *string `json:"workflow_name"`
	Step               *string `json:"step"`
	RunID              *string `json:"run_id"`
	Domain             *string `json:"domain"`
	Title              string  `json:"title"`
	Priority           int     `json:"priority"`
	Context            *string `json:"context"`
	ContextHash        *string `json:"context_hash,omitempty"`
	VisibilityTimeoutS int     `json:"visibility_timeout_s"`
	MaxAttempts        int     `json:"max_attempts"`
	RetryBackoffS      int     `json:"retry_backoff_s"`
	HardDeadlineS      *int    `json:"hard_deadline_s,omitempty"`
	Status             string  `json:"status"`
	AssignedWorkerID   *string `json:"assigned_worker_id"`
	LeaseExpiresAt     *string `json:"lease_expires_at"`
	RetryAfter         *string `json:"retry_after,omitempty"`
	AttemptCount       int     `json:"attempt_count"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
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

// TasksRouterHandler routes /api/tasks/{id} sub-paths.
func TasksRouterHandler(db *sql.DB) http.HandlerFunc {
	eventsHandler := TaskEventsHandler(db)
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/tasks/")

		if strings.HasSuffix(path, "/events") {
			eventsHandler(w, r)
			return
		}
		if strings.HasSuffix(path, "/heartbeat") {
			if r.Method == http.MethodPost {
				id := strings.TrimSuffix(path, "/heartbeat")
				handleTaskHeartbeat(db, w, r, id)
			} else {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			}
			return
		}
		if strings.HasSuffix(path, "/complete") {
			if r.Method == http.MethodPost {
				id := strings.TrimSuffix(path, "/complete")
				handleCompleteTask(db, w, r, id)
			} else {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			}
			return
		}
		if strings.HasSuffix(path, "/fail") {
			if r.Method == http.MethodPost {
				id := strings.TrimSuffix(path, "/fail")
				handleFailTask(db, w, r, id)
			} else {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			}
			return
		}

		// Plain /api/tasks/{id}
		id := strings.TrimSpace(path)
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

// TaskByIDHandler handles PATCH /api/tasks/{id} and DELETE /api/tasks/{id}.
// It is also used in tests that target a specific task by ID.
func TaskByIDHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

// LeaseHandler handles POST /api/tasks/lease.
func LeaseHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		handleLeaseTask(db, w, r)
	}
}


func handleCreateTask(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title               string  `json:"title"`
		WorkspaceID         *string `json:"workspace_id"`
		WorkflowName        *string `json:"workflow_name"`
		Step                *string `json:"step"`
		RunID               *string `json:"run_id"`
		Domain              *string `json:"domain"`
		Priority            int     `json:"priority"`
		Context             any     `json:"context"`
		VisibilityTimeoutS  int     `json:"visibility_timeout_s"`
		MaxAttempts         int     `json:"max_attempts"`
		RetryBackoffS       int     `json:"retry_backoff_s"`
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

	visTimeout := 300
	if req.VisibilityTimeoutS > 0 {
		visTimeout = req.VisibilityTimeoutS
	}
	maxAttempts := 3
	if req.MaxAttempts > 0 {
		maxAttempts = req.MaxAttempts
	}
	retryBackoff := 60
	if req.RetryBackoffS > 0 {
		retryBackoff = req.RetryBackoffS
	}

	// Inherit defaults from workflow if specified and not overridden.
	if req.WorkflowName != nil && req.VisibilityTimeoutS == 0 {
		var wvt, wma, wrb int
		err := db.QueryRow(`SELECT default_visibility_timeout_s, default_max_attempts, default_retry_backoff_s
			FROM workflows WHERE name = ?`, *req.WorkflowName).Scan(&wvt, &wma, &wrb)
		if err == nil {
			visTimeout = wvt
			maxAttempts = wma
			retryBackoff = wrb
		}
	}

	id := newID()
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.Exec(
		`INSERT INTO tasks (id, workspace_id, workflow_name, step, run_id, domain, title, priority,
		                    context, visibility_timeout_s, max_attempts, retry_backoff_s,
		                    status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'queued', ?, ?)`,
		id, req.WorkspaceID, req.WorkflowName, req.Step, req.RunID, req.Domain, req.Title,
		req.Priority, contextStr, visTimeout, maxAttempts, retryBackoff, now, now,
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
	AppendEvent(db, id, "", "", "created", nil)
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
	if v := q.Get("workflow_name"); v != "" {
		where = append(where, "workflow_name = ?")
		args = append(args, v)
	}
	if v := q.Get("step"); v != "" {
		where = append(where, "step = ?")
		args = append(args, v)
	}
	if v := q.Get("run_id"); v != "" {
		where = append(where, "run_id = ?")
		args = append(args, v)
	}
	if v := q.Get("assigned_worker_id"); v != "" {
		where = append(where, "assigned_worker_id = ?")
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

	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	if err := db.QueryRow("SELECT COUNT(*) FROM tasks WHERE "+whereClause, countArgs...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	pageArgs := append(args, limit, offset)
	rows, err := db.Query(
		`SELECT id, workspace_id, workflow_name, step, run_id, domain, title, priority,
		        context, context_hash, visibility_timeout_s, max_attempts, retry_backoff_s,
		        hard_deadline_s, status, assigned_worker_id, lease_expires_at, retry_after,
		        attempt_count, created_at, updated_at
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
	var status string
	err := db.QueryRow("SELECT status FROM tasks WHERE id = ?", id).Scan(&status)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found")
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
		Title        *string `json:"title"`
		Priority     *int    `json:"priority"`
		Context      any     `json:"context"`
		Domain       *string `json:"domain"`
		WorkspaceID  *string `json:"workspace_id"`
		WorkflowName *string `json:"workflow_name"`
		Step         *string `json:"step"`
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
	if req.WorkflowName != nil {
		sets = append(sets, "workflow_name = ?")
		args = append(args, *req.WorkflowName)
	}
	if req.Step != nil {
		sets = append(sets, "step = ?")
		args = append(args, *req.Step)
	}

	if len(sets) == 0 {
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
		writeError(w, http.StatusNotFound, "not_found")
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
	AppendEvent(db, id, "", "", "cancelled", nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleLeaseTask atomically claims the next eligible queued task.
func handleLeaseTask(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkerID     string  `json:"worker_id"`
		WorkspaceID  *string `json:"workspace_id"`
		WorkflowName *string `json:"workflow_name"`
		Step         *string `json:"step"`
		Domain       *string `json:"domain"`
		PriorityGte  *int    `json:"priority_gte"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	// Build filter conditions for the lease query.
	conds := []string{
		"status = 'queued'",
		"(retry_after IS NULL OR retry_after <= ?)",
	}
	args := []any{nowStr}

	if req.WorkspaceID != nil {
		conds = append(conds, "workspace_id = ?")
		args = append(args, *req.WorkspaceID)
	}
	if req.WorkflowName != nil {
		conds = append(conds, "workflow_name = ?")
		args = append(args, *req.WorkflowName)
	}
	if req.Step != nil {
		conds = append(conds, "step = ?")
		args = append(args, *req.Step)
	}
	if req.Domain != nil {
		conds = append(conds, "domain = ?")
		args = append(args, *req.Domain)
	}
	if req.PriorityGte != nil {
		conds = append(conds, "priority >= ?")
		args = append(args, *req.PriorityGte)
	}

	where := strings.Join(conds, " AND ")

	tx, err := db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	defer tx.Rollback()

	// Select next task (highest priority, oldest created_at).
	var taskID string
	var visTimeoutS int
	err = tx.QueryRow(
		`SELECT id, visibility_timeout_s FROM tasks WHERE `+where+
			` ORDER BY priority DESC, created_at ASC LIMIT 1`,
		args...,
	).Scan(&taskID, &visTimeoutS)
	if err == sql.ErrNoRows {
		tx.Commit()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	leaseExpires := now.Add(time.Duration(visTimeoutS) * time.Second).Format(time.RFC3339)

	// Get next fencing token.
	var maxToken sql.NullInt64
	tx.QueryRow("SELECT MAX(fencing_token) FROM task_attempts WHERE task_id = ?", taskID).Scan(&maxToken)
	fencingToken := int(maxToken.Int64) + 1

	// Create attempt row.
	attemptID := newID()
	_, err = tx.Exec(
		`INSERT INTO task_attempts (id, task_id, worker_id, fencing_token, started_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		attemptID, taskID, nullStr(req.WorkerID), fencingToken, nowStr, leaseExpires,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	// Update task status.
	_, err = tx.Exec(`
		UPDATE tasks SET
		    status = 'leased',
		    assigned_worker_id = ?,
		    lease_expires_at = ?,
		    attempt_count = attempt_count + 1,
		    updated_at = ?
		WHERE id = ?`,
		nullStr(req.WorkerID), leaseExpires, nowStr, taskID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	AppendEvent(db, taskID, attemptID, req.WorkerID, "leased", nil)

	task, err := getTaskByID(db, taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task":             task,
		"fencing_token":    fencingToken,
		"attempt_id":       attemptID,
		"lease_expires_at": leaseExpires,
	})
}

// handleTaskHeartbeat extends a lease and records progress.
func handleTaskHeartbeat(db *sql.DB, w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		WorkerID     string  `json:"worker_id"`
		FencingToken int     `json:"fencing_token"`
		Progress     *int    `json:"progress"`
		Message      *string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	var visTimeoutS int
	var attemptID string
	var dbToken int
	err := db.QueryRow(`
		SELECT t.visibility_timeout_s, ta.id, ta.fencing_token
		FROM tasks t
		JOIN task_attempts ta ON ta.task_id = t.id
		WHERE t.id = ? AND ta.ended_at IS NULL
		ORDER BY ta.fencing_token DESC LIMIT 1`, id).Scan(&visTimeoutS, &attemptID, &dbToken)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	if dbToken != req.FencingToken {
		writeError(w, http.StatusConflict, "stale_fencing_token")
		return
	}

	leaseExpires := now.Add(time.Duration(visTimeoutS) * time.Second).Format(time.RFC3339)
	_, err = db.Exec("UPDATE tasks SET lease_expires_at = ?, updated_at = ? WHERE id = ?",
		leaseExpires, nowStr, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	eventType := "heartbeat"
	if req.Progress != nil {
		eventType = "progress"
	}
	AppendEvent(db, id, attemptID, req.WorkerID, eventType, nil)

	writeJSON(w, http.StatusOK, map[string]string{"lease_expires_at": leaseExpires})
}

// handleCompleteTask marks a task as completed.
func handleCompleteTask(db *sql.DB, w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		WorkerID     string `json:"worker_id"`
		FencingToken int    `json:"fencing_token"`
		Result       any    `json:"result"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	attemptID, err := validateFencingToken(db, id, req.FencingToken)
	if err == errNotFound {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err == errStaleToken {
		writeError(w, http.StatusConflict, "stale_fencing_token")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`
		UPDATE tasks SET status = 'completed', assigned_worker_id = ?,
		    lease_expires_at = NULL, updated_at = ? WHERE id = ?`, req.WorkerID, now, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	var resultStr *string
	if req.Result != nil {
		b, _ := json.Marshal(req.Result)
		s := string(b)
		resultStr = &s
	}
	db.Exec(`UPDATE task_attempts SET ended_at = ?, result_code = 'completed' WHERE id = ?`, now, attemptID)
	AppendEvent(db, id, attemptID, req.WorkerID, "completed", resultStr)

	task, err := getTaskByID(db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

// handleFailTask reports a task failure.
func handleFailTask(db *sql.DB, w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		WorkerID     string `json:"worker_id"`
		FencingToken int    `json:"fencing_token"`
		Reason       string `json:"reason"`
		RetryHint    *bool  `json:"retry_hint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	attemptID, err := validateFencingToken(db, id, req.FencingToken)
	if err == errNotFound {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err == errStaleToken {
		writeError(w, http.StatusConflict, "stale_fencing_token")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	var attemptCount, maxAttempts, retryBackoff int
	db.QueryRow("SELECT attempt_count, max_attempts, retry_backoff_s FROM tasks WHERE id = ?", id).
		Scan(&attemptCount, &maxAttempts, &retryBackoff)

	wantRetry := req.RetryHint == nil || *req.RetryHint

	if wantRetry && attemptCount < maxAttempts {
		retryAfter := now.Add(time.Duration(retryBackoff) * time.Second).Format(time.RFC3339)
		db.Exec(`UPDATE tasks SET status = 'queued', assigned_worker_id = NULL,
			lease_expires_at = NULL, retry_after = ?, updated_at = ? WHERE id = ?`,
			retryAfter, nowStr, id)
		db.Exec(`UPDATE task_attempts SET ended_at = ?, result_code = 'failed' WHERE id = ?`, nowStr, attemptID)
		reason := req.Reason
		AppendEvent(db, id, attemptID, req.WorkerID, "failed", &reason)
	} else {
		db.Exec(`UPDATE tasks SET status = 'failed', assigned_worker_id = NULL,
			lease_expires_at = NULL, updated_at = ? WHERE id = ?`, nowStr, id)
		db.Exec(`UPDATE task_attempts SET ended_at = ?, result_code = 'failed' WHERE id = ?`, nowStr, attemptID)
		reason := req.Reason
		AppendEvent(db, id, attemptID, req.WorkerID, "failed", &reason)
	}

	task, err := getTaskByID(db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

// --- helpers ---

var errNotFound = fmt.Errorf("not_found")
var errStaleToken = fmt.Errorf("stale_token")

func validateFencingToken(db *sql.DB, taskID string, token int) (string, error) {
	var attemptID string
	var dbToken int
	err := db.QueryRow(`
		SELECT id, fencing_token FROM task_attempts
		WHERE task_id = ? AND ended_at IS NULL
		ORDER BY fencing_token DESC LIMIT 1`, taskID).Scan(&attemptID, &dbToken)
	if err == sql.ErrNoRows {
		return "", errNotFound
	}
	if err != nil {
		return "", err
	}
	if dbToken != token {
		return "", errStaleToken
	}
	return attemptID, nil
}

func getTaskByID(db *sql.DB, id string) (Task, error) {
	row := db.QueryRow(
		`SELECT id, workspace_id, workflow_name, step, run_id, domain, title, priority,
		        context, context_hash, visibility_timeout_s, max_attempts, retry_backoff_s,
		        hard_deadline_s, status, assigned_worker_id, lease_expires_at, retry_after,
		        attempt_count, created_at, updated_at
		 FROM tasks WHERE id = ?`, id,
	)
	return scanTask(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(s scanner) (Task, error) {
	var t Task
	var workspaceID, workflowName, step, runID sql.NullString
	var domain, context, contextHash, assignedWorkerID, leaseExpiresAt, retryAfter sql.NullString
	var hardDeadlineS sql.NullInt64
	err := s.Scan(
		&t.ID, &workspaceID, &workflowName, &step, &runID, &domain,
		&t.Title, &t.Priority, &context, &contextHash,
		&t.VisibilityTimeoutS, &t.MaxAttempts, &t.RetryBackoffS, &hardDeadlineS,
		&t.Status, &assignedWorkerID, &leaseExpiresAt, &retryAfter,
		&t.AttemptCount, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return Task{}, err
	}
	if workspaceID.Valid {
		t.WorkspaceID = &workspaceID.String
	}
	if workflowName.Valid {
		t.WorkflowName = &workflowName.String
	}
	if step.Valid {
		t.Step = &step.String
	}
	if runID.Valid {
		t.RunID = &runID.String
	}
	if domain.Valid {
		t.Domain = &domain.String
	}
	if context.Valid {
		t.Context = &context.String
	}
	if contextHash.Valid {
		t.ContextHash = &contextHash.String
	}
	if hardDeadlineS.Valid {
		v := int(hardDeadlineS.Int64)
		t.HardDeadlineS = &v
	}
	if assignedWorkerID.Valid {
		t.AssignedWorkerID = &assignedWorkerID.String
	}
	if leaseExpiresAt.Valid {
		t.LeaseExpiresAt = &leaseExpiresAt.String
	}
	if retryAfter.Valid {
		t.RetryAfter = &retryAfter.String
	}
	return t, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
