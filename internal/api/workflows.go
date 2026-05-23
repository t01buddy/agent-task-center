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
		// Trim leading prefix; path is e.g. "" or "/bug-fix"
		name := strings.TrimPrefix(r.URL.Path, "/api/workflows")
		name = strings.TrimPrefix(name, "/")

		if name == "" {
			switch r.Method {
			case http.MethodPost:
				createWorkflow(w, r, db)
			case http.MethodGet:
				listWorkflows(w, r, db)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Definition) == "" {
		http.Error(w, `{"error":"definition required"}`, http.StatusBadRequest)
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
			http.Error(w, `{"error":"workflow already exists"}`, http.StatusConflict)
			return
		}
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"workflow": wf})
}

func listWorkflows(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	rows, err := db.Query(
		`SELECT name, definition, default_visibility_timeout_s, default_max_attempts, default_retry_backoff_s, created_at, updated_at
		 FROM workflows ORDER BY name ASC`,
	)
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var workflows []Workflow
	for rows.Next() {
		var wf Workflow
		if err := rows.Scan(&wf.Name, &wf.Definition, &wf.DefaultVisibilityTimeout, &wf.DefaultMaxAttempts, &wf.DefaultRetryBackoff, &wf.CreatedAt, &wf.UpdatedAt); err != nil {
			http.Error(w, "scan error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		workflows = append(workflows, wf)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "rows error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if workflows == nil {
		workflows = []Workflow{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"workflows": workflows})
}

func getWorkflow(w http.ResponseWriter, r *http.Request, db *sql.DB, name string) {
	var wf Workflow
	err := db.QueryRow(
		`SELECT name, definition, default_visibility_timeout_s, default_max_attempts, default_retry_backoff_s, created_at, updated_at
		 FROM workflows WHERE name = ?`, name,
	).Scan(&wf.Name, &wf.Definition, &wf.DefaultVisibilityTimeout, &wf.DefaultMaxAttempts, &wf.DefaultRetryBackoff, &wf.CreatedAt, &wf.UpdatedAt)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"workflow": wf})
}

func updateWorkflow(w http.ResponseWriter, r *http.Request, db *sql.DB, name string) {
	var req workflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	var current Workflow
	err := db.QueryRow(
		`SELECT name, definition, default_visibility_timeout_s, default_max_attempts, default_retry_backoff_s, created_at, updated_at
		 FROM workflows WHERE name = ?`, name,
	).Scan(&current.Name, &current.Definition, &current.DefaultVisibilityTimeout, &current.DefaultMaxAttempts, &current.DefaultRetryBackoff, &current.CreatedAt, &current.UpdatedAt)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"workflow": current})
}

func deleteWorkflow(w http.ResponseWriter, r *http.Request, db *sql.DB, name string) {
	res, err := db.Exec(`DELETE FROM workflows WHERE name = ?`, name)
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
