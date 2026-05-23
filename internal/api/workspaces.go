package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Workspace is the response object for workspace endpoints.
type Workspace struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// WorkspacesHandler handles POST /api/workspaces and GET /api/workspaces.
func WorkspacesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleCreateWorkspace(db, w, r)
		case http.MethodGet:
			handleListWorkspaces(db, w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		}
	}
}

func handleCreateWorkspace(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
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
	if len(name) > 100 {
		writeError(w, http.StatusBadRequest, "name exceeds 100 characters")
		return
	}

	id := newID()
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.Exec(
		`INSERT INTO workspaces (id, name, created_at) VALUES (?, ?, ?)`,
		id, name, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, Workspace{ID: id, Name: name, CreatedAt: now})
}

func handleListWorkspaces(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, name, created_at FROM workspaces ORDER BY created_at ASC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	defer rows.Close()

	workspaces := []Workspace{}
	for rows.Next() {
		var ws Workspace
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error: "+err.Error())
			return
		}
		workspaces = append(workspaces, ws)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"workspaces": workspaces})
}
