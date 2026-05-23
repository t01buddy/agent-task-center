package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Agent is the response object for agent endpoints.
type Agent struct {
	ID             string   `json:"agent_id"`
	Name           string   `json:"name"`
	Runtime        string   `json:"runtime"`
	RuntimeVersion *string  `json:"runtime_version"`
	Domain         string   `json:"domain"`
	WorkspaceID    *string  `json:"workspace_id"`
	Capabilities   []string `json:"capabilities"`
	LastHeartbeatAt *string `json:"last_heartbeat_at"`
	Status         string   `json:"status"`
	RegisteredAt   string   `json:"registered_at"`
}

// AgentsRegisterHandler handles POST /api/agents/register.
func AgentsRegisterHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		handleRegisterAgent(db, w, r)
	}
}

// AgentsHandler handles GET /api/agents.
func AgentsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		handleListAgents(db, w, r)
	}
}

// AgentHeartbeatHandler handles POST /api/agents/{id}/heartbeat.
func AgentHeartbeatHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		// Extract agent ID: /api/agents/<id>/heartbeat
		path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
		agentID := strings.TrimSuffix(path, "/heartbeat")
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "missing agent id")
			return
		}
		handleAgentHeartbeat(db, w, r, agentID)
	}
}

// AgentsRouterHandler routes /api/agents/{id}/heartbeat.
func AgentsRouterHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/agents/")
		if strings.HasSuffix(path, "/heartbeat") {
			agentID := strings.TrimSuffix(path, "/heartbeat")
			agentID = strings.TrimSpace(agentID)
			if agentID == "" {
				writeError(w, http.StatusBadRequest, "missing agent id")
				return
			}
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
				return
			}
			handleAgentHeartbeat(db, w, r, agentID)
			return
		}
		writeError(w, http.StatusNotFound, "not found")
	}
}

func handleRegisterAgent(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentID        string   `json:"agent_id"`
		Name           string   `json:"name"`
		Runtime        string   `json:"runtime"`
		RuntimeVersion *string  `json:"runtime_version"`
		Domain         string   `json:"domain"`
		WorkspaceID    *string  `json:"workspace_id"`
		Capabilities   []string `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if strings.TrimSpace(req.Runtime) == "" {
		writeError(w, http.StatusBadRequest, "runtime is required")
		return
	}
	if strings.TrimSpace(req.Domain) == "" {
		writeError(w, http.StatusBadRequest, "domain is required")
		return
	}

	var capsJSON *string
	if len(req.Capabilities) > 0 {
		b, _ := json.Marshal(req.Capabilities)
		s := string(b)
		capsJSON = &s
	}

	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.Exec(`
		INSERT INTO agents (id, name, runtime, runtime_version, domain, workspace_id, capabilities, status, registered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'active', ?)
		ON CONFLICT(id) DO UPDATE SET
		  name = excluded.name,
		  runtime = excluded.runtime,
		  runtime_version = excluded.runtime_version,
		  domain = excluded.domain,
		  workspace_id = excluded.workspace_id,
		  capabilities = excluded.capabilities,
		  status = 'active'
	`, req.AgentID, req.Name, req.Runtime, req.RuntimeVersion, req.Domain, req.WorkspaceID, capsJSON, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}

	agent, err := getAgentByID(db, req.AgentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, agent)
}

func handleAgentHeartbeat(db *sql.DB, w http.ResponseWriter, r *http.Request, agentID string) {
	now := time.Now().UTC().Format(time.RFC3339)

	result, err := db.Exec(
		`UPDATE agents SET last_heartbeat_at = ?, status = 'active' WHERE id = ?`,
		now, agentID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	var status string
	db.QueryRow(`SELECT status FROM agents WHERE id = ?`, agentID).Scan(&status) //nolint:errcheck

	writeJSON(w, http.StatusOK, map[string]string{
		"agent_id":          agentID,
		"status":            status,
		"last_heartbeat_at": now,
	})
}

func handleListAgents(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
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
	if v := q.Get("status"); v != "" {
		where = append(where, "status = ?")
		args = append(args, v)
	}

	rows, err := db.Query(
		`SELECT id, name, runtime, runtime_version, domain, workspace_id, capabilities, last_heartbeat_at, status, registered_at
		 FROM agents WHERE `+strings.Join(where, " AND ")+` ORDER BY registered_at ASC`,
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error: "+err.Error())
		return
	}
	defer rows.Close()

	agents := []Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan error: "+err.Error())
			return
		}
		agents = append(agents, a)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

func getAgentByID(db *sql.DB, id string) (Agent, error) {
	row := db.QueryRow(
		`SELECT id, name, runtime, runtime_version, domain, workspace_id, capabilities, last_heartbeat_at, status, registered_at
		 FROM agents WHERE id = ?`, id,
	)
	return scanAgent(row)
}

func scanAgent(s scanner) (Agent, error) {
	var a Agent
	var runtimeVersion, workspaceID, caps, lastHeartbeat sql.NullString
	err := s.Scan(
		&a.ID, &a.Name, &a.Runtime, &runtimeVersion, &a.Domain,
		&workspaceID, &caps, &lastHeartbeat, &a.Status, &a.RegisteredAt,
	)
	if err != nil {
		return Agent{}, err
	}
	if runtimeVersion.Valid {
		a.RuntimeVersion = &runtimeVersion.String
	}
	if workspaceID.Valid {
		a.WorkspaceID = &workspaceID.String
	}
	if caps.Valid {
		json.Unmarshal([]byte(caps.String), &a.Capabilities) //nolint:errcheck
	} else {
		a.Capabilities = []string{}
	}
	if lastHeartbeat.Valid {
		a.LastHeartbeatAt = &lastHeartbeat.String
	}
	return a, nil
}
