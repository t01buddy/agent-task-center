package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// LeaseHandler handles POST /api/tasks/lease.
func LeaseHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		handleLease(db, w, r)
	}
}

func handleLease(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentID     string  `json:"agent_id"`
		WorkspaceID *string `json:"workspace_id"`
		Domain      *string `json:"domain"`
		TaskTypeID  *string `json:"task_type_id"`
		PriorityGte *int    `json:"priority_gte"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	// Build filter clause
	where := []string{"t.status = 'queued'", "(t.retry_after IS NULL OR t.retry_after <= ?)"}
	args := []any{nowStr}

	if req.WorkspaceID != nil {
		where = append(where, "t.workspace_id = ?")
		args = append(args, *req.WorkspaceID)
	}
	if req.Domain != nil {
		where = append(where, "t.domain = ?")
		args = append(args, *req.Domain)
	}
	if req.TaskTypeID != nil {
		where = append(where, "t.task_type_id = ?")
		args = append(args, *req.TaskTypeID)
	}
	if req.PriorityGte != nil {
		where = append(where, "t.priority >= ?")
		args = append(args, *req.PriorityGte)
	}

	whereClause := strings.Join(where, " AND ")

	// Use a transaction + SELECT with ROWID to atomically lease the task.
	tx, err := db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	defer tx.Rollback() //nolint:errcheck

	// Select the best eligible task (highest priority, then oldest)
	var taskID string
	var visibilityTimeoutS int
	var currentAttemptCount int
	err = tx.QueryRow(`
		SELECT t.id, t.attempt_count,
		       COALESCE(tt.default_visibility_timeout_s, 300)
		FROM tasks t
		LEFT JOIN task_types tt ON tt.id = t.task_type_id
		WHERE `+whereClause+`
		ORDER BY t.priority DESC, t.created_at ASC
		LIMIT 1
	`, args...).Scan(&taskID, &currentAttemptCount, &visibilityTimeoutS)

	if err == sql.ErrNoRows {
		tx.Rollback() //nolint:errcheck
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	// Compute new fencing token (max existing + 1)
	var maxToken int
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(fencing_token), 0) FROM task_attempts WHERE task_id = ?`, taskID,
	).Scan(&maxToken); err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	fencingToken := maxToken + 1

	leaseExpiresAt := now.Add(time.Duration(visibilityTimeoutS) * time.Second)
	leaseExpiresAtStr := leaseExpiresAt.Format(time.RFC3339)
	newAttemptCount := currentAttemptCount + 1

	// Update task to leased
	_, err = tx.Exec(`
		UPDATE tasks
		SET status = 'leased',
		    assigned_agent_id = ?,
		    lease_expires_at = ?,
		    attempt_count = ?,
		    updated_at = ?
		WHERE id = ?
	`, req.AgentID, leaseExpiresAtStr, newAttemptCount, nowStr, taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	// Create task_attempts row
	attemptID := newID()
	_, err = tx.Exec(`
		INSERT INTO task_attempts (id, task_id, agent_id, fencing_token, started_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, attemptID, taskID, req.AgentID, fencingToken, nowStr, leaseExpiresAtStr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	// Append leased event
	eventID := newID()
	_, err = tx.Exec(`
		INSERT INTO task_events (id, task_id, attempt_id, agent_id, event_type, created_at)
		VALUES (?, ?, ?, ?, 'leased', ?)
	`, eventID, taskID, attemptID, req.AgentID, nowStr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "commit error: "+err.Error())
		return
	}

	// Fetch updated task for response
	task, err := getTaskByID(db, taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"task":             task,
		"fencing_token":    fencingToken,
		"lease_expires_at": leaseExpiresAtStr,
	})
}

// ValidateFencingToken checks that the given fencing_token matches the current active attempt.
// Returns the current max fencing token and an error if mismatched.
func ValidateFencingToken(db *sql.DB, taskID string, fencingToken int) error {
	var maxToken int
	err := db.QueryRow(
		`SELECT COALESCE(MAX(fencing_token), 0) FROM task_attempts WHERE task_id = ?`, taskID,
	).Scan(&maxToken)
	if err != nil {
		return err
	}
	if fencingToken != maxToken {
		return errStaleFencingToken
	}
	return nil
}

// errStaleFencingToken is returned when the provided fencing token is stale.
var errStaleFencingToken = &staleFencingTokenError{}

type staleFencingTokenError struct{}

func (e *staleFencingTokenError) Error() string { return "stale_fencing_token" }

// IsStaleFencingTokenError reports whether err is a stale fencing token error.
func IsStaleFencingTokenError(err error) bool {
	_, ok := err.(*staleFencingTokenError)
	return ok
}

