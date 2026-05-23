package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Workflow is a named workflow with a natural-language definition.
type Workflow struct {
	Name                     string `json:"name"`
	Definition               string `json:"definition"`
	DefaultVisibilityTimeout int    `json:"default_visibility_timeout_s"`
	DefaultMaxAttempts       int    `json:"default_max_attempts"`
	DefaultRetryBackoff      int    `json:"default_retry_backoff_s"`
	CreatedAt                string `json:"created_at"`
	UpdatedAt                string `json:"updated_at"`
}

// WorkflowsHandler returns a handler for:
//
//	POST   /api/workflows          — create
//	GET    /api/workflows          — list
//	GET    /api/workflows/{name}   — get
//	PUT    /api/workflows/{name}   — update
//	DELETE /api/workflows/{name}   — delete
func WorkflowsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/api/workflows")
		name = strings.TrimPrefix(name, "/")

		if name == "" {
			switch r.Method {
			case http.MethodPost:
				createWorkflow(w, r, db)
			case http.MethodGet:
				listWorkflows(w, r, db)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			}
			return
		}

		switch r.Method {
		case http.MethodGet:
			getWorkflow(w, r, db, name)
		case http.MethodPut:
			updateWorkflow(w, r, db, name)
		case http.MethodDelete:
			deleteWorkflow(w, r, db, name)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		}
	}
}

type workflowRequest struct {
	Name                     string `json:"name"`
	Definition               string `json:"definition"`
	DefaultVisibilityTimeout *int   `json:"default_visibility_timeout_s"`
	DefaultMaxAttempts       *int   `json:"default_max_attempts"`
	DefaultRetryBackoff      *int   `json:"default_retry_backoff_s"`
}

func createWorkflow(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req workflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	if strings.TrimSpace(req.Definition) == "" {
		writeError(w, http.StatusBadRequest, "definition required")
		return
	}

	vt := 300
	if req.DefaultVisibilityTimeout != nil {
		vt = *req.DefaultVisibilityTimeout
	}
	ma := 3
	if req.DefaultMaxAttempts != nil {
		ma = *req.DefaultMaxAttempts
	}
	rb := 60
	if req.DefaultRetryBackoff != nil {
		rb = *req.DefaultRetryBackoff
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO workflows (name, definition, default_visibility_timeout_s, default_max_attempts, default_retry_backoff_s, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.Definition, vt, ma, rb, now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "conflict")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	wf := Workflow{
		Name:                     req.Name,
		Definition:               req.Definition,
		DefaultVisibilityTimeout: vt,
		DefaultMaxAttempts:       ma,
		DefaultRetryBackoff:      rb,
		CreatedAt:                now,
		UpdatedAt:                now,
	}
	writeJSON(w, http.StatusCreated, map[string]any{"workflow": wf})
}

func listWorkflows(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.Query(
		`SELECT name, definition, default_visibility_timeout_s, default_max_attempts, default_retry_backoff_s, created_at, updated_at
		 FROM workflows ORDER BY name ASC`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	defer rows.Close()

	var workflows []Workflow
	for rows.Next() {
		var wf Workflow
		if err := rows.Scan(&wf.Name, &wf.Definition, &wf.DefaultVisibilityTimeout, &wf.DefaultMaxAttempts, &wf.DefaultRetryBackoff, &wf.CreatedAt, &wf.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		workflows = append(workflows, wf)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if workflows == nil {
		workflows = []Workflow{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"workflows": workflows})
}

func getWorkflow(w http.ResponseWriter, r *http.Request, db *sql.DB, name string) {
	var wf Workflow
	err := db.QueryRow(
		`SELECT name, definition, default_visibility_timeout_s, default_max_attempts, default_retry_backoff_s, created_at, updated_at
		 FROM workflows WHERE name = ?`, name,
	).Scan(&wf.Name, &wf.Definition, &wf.DefaultVisibilityTimeout, &wf.DefaultMaxAttempts, &wf.DefaultRetryBackoff, &wf.CreatedAt, &wf.UpdatedAt)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflow": wf})
}

func updateWorkflow(w http.ResponseWriter, r *http.Request, db *sql.DB, name string) {
	var req workflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	var current Workflow
	err := db.QueryRow(
		`SELECT name, definition, default_visibility_timeout_s, default_max_attempts, default_retry_backoff_s, created_at, updated_at
		 FROM workflows WHERE name = ?`, name,
	).Scan(&current.Name, &current.Definition, &current.DefaultVisibilityTimeout, &current.DefaultMaxAttempts, &current.DefaultRetryBackoff, &current.CreatedAt, &current.UpdatedAt)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	if req.Definition != "" {
		current.Definition = req.Definition
	}
	if req.DefaultVisibilityTimeout != nil {
		current.DefaultVisibilityTimeout = *req.DefaultVisibilityTimeout
	}
	if req.DefaultMaxAttempts != nil {
		current.DefaultMaxAttempts = *req.DefaultMaxAttempts
	}
	if req.DefaultRetryBackoff != nil {
		current.DefaultRetryBackoff = *req.DefaultRetryBackoff
	}
	current.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	_, err = db.Exec(
		`UPDATE workflows SET definition=?, default_visibility_timeout_s=?, default_max_attempts=?, default_retry_backoff_s=?, updated_at=?
		 WHERE name=?`,
		current.Definition, current.DefaultVisibilityTimeout, current.DefaultMaxAttempts, current.DefaultRetryBackoff, current.UpdatedAt, name,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"workflow": current})
}

func deleteWorkflow(w http.ResponseWriter, r *http.Request, db *sql.DB, name string) {
	res, err := db.Exec(`DELETE FROM workflows WHERE name = ?`, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
