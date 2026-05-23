package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const defaultVisibilityTimeoutS = 300

// LeaseResponse is returned on a successful POST /api/tasks/lease.
type LeaseResponse struct {
	Task          Task   `json:"task"`
	FencingToken  int64  `json:"fencing_token"`
	LeaseExpiresAt string `json:"lease_expires_at"`
}

// LeaseHandler handles POST /api/tasks/lease.
func LeaseHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}

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

		resp, err := atomicLease(db, req.AgentID, req.WorkspaceID, req.Domain, req.TaskTypeID, req.PriorityGte)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
			return
		}
		if resp == nil {
			// No eligible task
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// atomicLease claims the next eligible task in a single SQLite transaction.
// Returns nil if no task is available.
func atomicLease(db *sql.DB, agentID string, workspaceID, domain, taskTypeID *string, priorityGte *int) (*LeaseResponse, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Build filter WHERE clause for queued tasks
	where := []string{"t.status = 'queued'"}
	args := []any{}

	if workspaceID != nil {
		where = append(where, "t.workspace_id = ?")
		args = append(args, *workspaceID)
	}
	if domain != nil {
		where = append(where, "t.domain = ?")
		args = append(args, *domain)
	}
	if taskTypeID != nil {
		where = append(where, "t.task_type_id = ?")
		args = append(args, *taskTypeID)
	}
	if priorityGte != nil {
		where = append(where, "t.priority >= ?")
		args = append(args, *priorityGte)
	}

	whereClause := strings.Join(where, " AND ")

	// Select the highest-priority queued task (with visibility timeout from task type)
	query := `
		SELECT t.id, COALESCE(tt.default_visibility_timeout_s, ?) as vt
		FROM tasks t
		LEFT JOIN task_types tt ON tt.id = t.task_type_id
		WHERE ` + whereClause + `
		ORDER BY t.priority DESC, t.created_at ASC
		LIMIT 1`

	args = append([]any{defaultVisibilityTimeoutS}, args...)

	var taskID string
	var visibilityTimeoutS int
	err = tx.QueryRow(query, args...).Scan(&taskID, &visibilityTimeoutS)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	leaseExpiresAt := now.Add(time.Duration(visibilityTimeoutS) * time.Second).Format(time.RFC3339)
	nowStr := now.Format(time.RFC3339)

	// Get next fencing token = MAX(fencing_token) + 1 for this task
	var maxToken int64
	tokenErr := tx.QueryRow(
		`SELECT COALESCE(MAX(fencing_token), 0) FROM task_attempts WHERE task_id = ?`, taskID,
	).Scan(&maxToken)
	if tokenErr != nil {
		return nil, tokenErr
	}
	fencingToken := maxToken + 1

	// Create task_attempts row
	attemptID := newID()
	_, err = tx.Exec(
		`INSERT INTO task_attempts (id, task_id, agent_id, fencing_token, started_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		attemptID, taskID, agentID, fencingToken, nowStr, leaseExpiresAt,
	)
	if err != nil {
		return nil, err
	}

	// Update task: status=leased, assigned_agent_id, lease_expires_at, increment attempt_count
	_, err = tx.Exec(
		`UPDATE tasks SET status = 'leased', assigned_agent_id = ?, lease_expires_at = ?,
		  attempt_count = attempt_count + 1, updated_at = ?
		 WHERE id = ?`,
		agentID, leaseExpiresAt, nowStr, taskID,
	)
	if err != nil {
		return nil, err
	}

	// Append leased event
	eventID := newID()
	_, err = tx.Exec(
		`INSERT INTO task_events (id, task_id, attempt_id, agent_id, event_type, created_at)
		 VALUES (?, ?, ?, ?, 'leased', ?)`,
		eventID, taskID, attemptID, agentID, nowStr,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Read back the updated task
	task, err := getTaskByID(db, taskID)
	if err != nil {
		return nil, err
	}

	return &LeaseResponse{
		Task:           task,
		FencingToken:   fencingToken,
		LeaseExpiresAt: leaseExpiresAt,
	}, nil
}

// ValidateFencingToken checks that the given fencing_token matches the latest
// attempt for taskID. Returns (attemptID, nil) on success, or ("", err) on mismatch.
// Returns ("", sql.ErrNoRows) if task not found.
func ValidateFencingToken(db interface {
	QueryRow(string, ...any) *sql.Row
}, taskID string, fencingToken int64) (string, error) {
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
		return "", errStaleFencingToken
	}
	return attemptID, nil
}

// errStaleFencingToken is a sentinel used by ValidateFencingToken.
var errStaleFencingToken = staleFencingTokenError{}

type staleFencingTokenError struct{}

func (staleFencingTokenError) Error() string { return "stale_fencing_token" }

// IsStaleFencingToken reports whether err is a stale fencing token error.
func IsStaleFencingToken(err error) bool {
	_, ok := err.(staleFencingTokenError)
	return ok
}
