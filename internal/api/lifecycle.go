package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// HeartbeatHandler handles POST /api/tasks/{id}/heartbeat.
func HeartbeatHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}

		taskID := extractTaskIDSuffix(r.URL.Path, "/heartbeat")
		if taskID == "" {
			writeError(w, http.StatusBadRequest, "missing task id")
			return
		}

		var req struct {
			AgentID      string  `json:"agent_id"`
			FencingToken int64   `json:"fencing_token"`
			Progress     *int    `json:"progress"`
			Message      *string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}

		// Validate fencing token
		attemptID, err := validateToken(db, taskID, req.FencingToken)
		if err != nil {
			if isStaleFencingToken(err) {
				writeError(w, http.StatusConflict, "stale_fencing_token")
				return
			}
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}

		// Get visibility timeout from task type
		visibilityTimeoutS, err := getVisibilityTimeout(db, taskID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}

		now := time.Now().UTC()
		leaseExpiresAt := now.Add(time.Duration(visibilityTimeoutS) * time.Second).Format(time.RFC3339)
		nowStr := now.Format(time.RFC3339)

		// Extend lease
		_, err = db.Exec(
			`UPDATE tasks SET lease_expires_at = ?, updated_at = ? WHERE id = ?`,
			leaseExpiresAt, nowStr, taskID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}

		// Append heartbeat event with optional progress/message payload
		var payloadStr *string
		if req.Progress != nil || req.Message != nil {
			p := map[string]any{}
			if req.Progress != nil {
				p["progress"] = *req.Progress
			}
			if req.Message != nil {
				p["message"] = *req.Message
			}
			b, _ := json.Marshal(p)
			s := string(b)
			payloadStr = &s
		}
		_ = AppendEvent(db, taskID, attemptID, req.AgentID, "heartbeat", payloadStr)

		writeJSON(w, http.StatusOK, map[string]string{"lease_expires_at": leaseExpiresAt})
	}
}

// CompleteHandler handles POST /api/tasks/{id}/complete.
func CompleteHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}

		taskID := extractTaskIDSuffix(r.URL.Path, "/complete")
		if taskID == "" {
			writeError(w, http.StatusBadRequest, "missing task id")
			return
		}

		var req struct {
			AgentID      string `json:"agent_id"`
			FencingToken int64  `json:"fencing_token"`
			Result       any    `json:"result"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}

		attemptID, err := validateToken(db, taskID, req.FencingToken)
		if err != nil {
			if isStaleFencingToken(err) {
				writeError(w, http.StatusConflict, "stale_fencing_token")
				return
			}
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}

		now := time.Now().UTC().Format(time.RFC3339)

		// Close attempt row
		_, err = db.Exec(
			`UPDATE task_attempts SET ended_at = ?, result_code = 'completed' WHERE id = ?`,
			now, attemptID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}

		// Set task completed
		_, err = db.Exec(
			`UPDATE tasks SET status = 'completed', lease_expires_at = NULL, updated_at = ? WHERE id = ?`,
			now, taskID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}

		// Append completed event with optional result payload
		var payloadStr *string
		if req.Result != nil {
			b, _ := json.Marshal(req.Result)
			s := string(b)
			payloadStr = &s
		}
		_ = AppendEvent(db, taskID, attemptID, req.AgentID, "completed", payloadStr)

		task, err := getTaskByID(db, taskID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, task)
	}
}

// FailHandler handles POST /api/tasks/{id}/fail.
func FailHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}

		taskID := extractTaskIDSuffix(r.URL.Path, "/fail")
		if taskID == "" {
			writeError(w, http.StatusBadRequest, "missing task id")
			return
		}

		var req struct {
			AgentID      string  `json:"agent_id"`
			FencingToken int64   `json:"fencing_token"`
			Reason       string  `json:"reason"`
			RetryHint    *bool   `json:"retry_hint"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}

		attemptID, err := validateToken(db, taskID, req.FencingToken)
		if err != nil {
			if isStaleFencingToken(err) {
				writeError(w, http.StatusConflict, "stale_fencing_token")
				return
			}
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}

		now := time.Now().UTC().Format(time.RFC3339)

		// Read attempt_count and max_attempts from task + task_type
		var attemptCount, maxAttempts int
		row := db.QueryRow(`
			SELECT t.attempt_count, COALESCE(tt.max_attempts, 3), COALESCE(tt.retry_backoff_s, 60)
			FROM tasks t
			LEFT JOIN task_types tt ON tt.id = t.task_type_id
			WHERE t.id = ?`, taskID)
		var retryBackoffS int
		if err := row.Scan(&attemptCount, &maxAttempts, &retryBackoffS); err != nil {
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}

		// Close attempt row
		_, _ = db.Exec(
			`UPDATE task_attempts SET ended_at = ?, result_code = 'failed' WHERE id = ?`,
			now, attemptID,
		)

		// Determine: retry or fail
		wantRetry := req.RetryHint == nil || *req.RetryHint // default true
		canRetry := wantRetry && attemptCount < maxAttempts

		var newStatus, retryAfter string
		if canRetry {
			newStatus = "queued"
			retryAfterTime := time.Now().UTC().Add(time.Duration(retryBackoffS) * time.Second)
			retryAfter = retryAfterTime.Format(time.RFC3339)
			_, err = db.Exec(
				`UPDATE tasks SET status = 'queued', assigned_agent_id = NULL,
				  lease_expires_at = NULL, retry_after = ?, updated_at = ? WHERE id = ?`,
				retryAfter, now, taskID,
			)
		} else {
			newStatus = "failed"
			_, err = db.Exec(
				`UPDATE tasks SET status = 'failed', lease_expires_at = NULL, updated_at = ? WHERE id = ?`,
				now, taskID,
			)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}

		// Append failed event with reason payload
		payload := map[string]any{"reason": req.Reason, "new_status": newStatus}
		if retryAfter != "" {
			payload["retry_after"] = retryAfter
		}
		b, _ := json.Marshal(payload)
		s := string(b)
		_ = AppendEvent(db, taskID, attemptID, req.AgentID, "failed", &s)

		task, err := getTaskByID(db, taskID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, task)
	}
}

// extractTaskIDSuffix extracts the task ID from a path like /api/tasks/<id>/<suffix>.
func extractTaskIDSuffix(urlPath, suffix string) string {
	path := strings.TrimPrefix(urlPath, "/api/tasks/")
	path = strings.TrimSuffix(path, suffix)
	return strings.TrimSpace(path)
}

// validateToken is a package-local wrapper around ValidateFencingToken for use before
// ValidateFencingToken is available (i.e. when issue #8 hasn't merged yet).
// When issue #8 is merged this calls the exported function.
func validateToken(db *sql.DB, taskID string, fencingToken int64) (string, error) {
	var attemptID string
	var storedToken int64
	err := db.QueryRow(
		`SELECT id, fencing_token FROM task_attempts
		 WHERE task_id = ? ORDER BY fencing_token DESC LIMIT 1`, taskID,
	).Scan(&attemptID, &storedToken)
	if err != nil {
		return "", err
	}
	if storedToken != fencingToken {
		return "", staleFencingToken{}
	}
	return attemptID, nil
}

type staleFencingToken struct{}

func (staleFencingToken) Error() string { return "stale_fencing_token" }

func isStaleFencingToken(err error) bool {
	_, ok := err.(staleFencingToken)
	return ok
}

// getVisibilityTimeout returns the visibility timeout for a task's task_type, or 300s default.
func getVisibilityTimeout(db *sql.DB, taskID string) (int, error) {
	var timeoutS int
	err := db.QueryRow(`
		SELECT COALESCE(tt.default_visibility_timeout_s, 300)
		FROM tasks t
		LEFT JOIN task_types tt ON tt.id = t.task_type_id
		WHERE t.id = ?`, taskID,
	).Scan(&timeoutS)
	if err != nil {
		return 300, err
	}
	return timeoutS, nil
}
