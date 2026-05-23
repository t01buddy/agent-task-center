package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// TaskType is the response object for task type endpoints.
type TaskType struct {
	ID                        string `json:"id"`
	Name                      string `json:"name"`
	DefaultVisibilityTimeoutS int    `json:"default_visibility_timeout_s"`
	MaxAttempts               int    `json:"max_attempts"`
	RetryBackoffS             int    `json:"retry_backoff_s"`
	HardDeadlineS             *int   `json:"hard_deadline_s"`
	StaleHeartbeatThresholdS  int    `json:"stale_heartbeat_threshold_s"`
	CreatedAt                 string `json:"created_at"`
}

// TaskTypesHandler handles POST /api/task-types and GET /api/task-types.
func TaskTypesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleCreateTaskType(db, w, r)
		case http.MethodGet:
			handleListTaskTypes(db, w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		}
	}
}

func handleCreateTaskType(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name                      string `json:"name"`
		DefaultVisibilityTimeoutS *int   `json:"default_visibility_timeout_s"`
		MaxAttempts               *int   `json:"max_attempts"`
		RetryBackoffS             *int   `json:"retry_backoff_s"`
		HardDeadlineS             *int   `json:"hard_deadline_s"`
		StaleHeartbeatThresholdS  *int   `json:"stale_heartbeat_threshold_s"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Apply defaults
	dvt := 300
	if req.DefaultVisibilityTimeoutS != nil {
		dvt = *req.DefaultVisibilityTimeoutS
	}
	ma := 3
	if req.MaxAttempts != nil {
		ma = *req.MaxAttempts
	}
	rb := 60
	if req.RetryBackoffS != nil {
		rb = *req.RetryBackoffS
	}
	sht := 60
	if req.StaleHeartbeatThresholdS != nil {
		sht = *req.StaleHeartbeatThresholdS
	}

	id := newID()
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.Exec(
		`INSERT INTO task_types (id, name, default_visibility_timeout_s, max_attempts, retry_backoff_s, hard_deadline_s, stale_heartbeat_threshold_s, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, dvt, ma, rb, req.HardDeadlineS, sht, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, TaskType{
		ID:                        id,
		Name:                      name,
		DefaultVisibilityTimeoutS: dvt,
		MaxAttempts:               ma,
		RetryBackoffS:             rb,
		HardDeadlineS:             req.HardDeadlineS,
		StaleHeartbeatThresholdS:  sht,
		CreatedAt:                 now,
	})
}

func handleListTaskTypes(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(
		`SELECT id, name, default_visibility_timeout_s, max_attempts, retry_backoff_s, hard_deadline_s, stale_heartbeat_threshold_s, created_at
		 FROM task_types ORDER BY created_at ASC`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	defer rows.Close()

	taskTypes := []TaskType{}
	for rows.Next() {
		var tt TaskType
		var hardDeadlineS sql.NullInt64
		if err := rows.Scan(&tt.ID, &tt.Name, &tt.DefaultVisibilityTimeoutS, &tt.MaxAttempts, &tt.RetryBackoffS, &hardDeadlineS, &tt.StaleHeartbeatThresholdS, &tt.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error: "+err.Error())
			return
		}
		if hardDeadlineS.Valid {
			v := int(hardDeadlineS.Int64)
			tt.HardDeadlineS = &v
		}
		taskTypes = append(taskTypes, tt)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"task_types": taskTypes})
}
