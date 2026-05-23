// Package api contains HTTP handlers for the agent-task-center REST API.
package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// LogEntry is a single log line from task_logs.
type LogEntry struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	AttemptID string `json:"attempt_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// ingestRequest is the body of POST /api/logs.
type ingestRequest struct {
	Logs []struct {
		TaskID    string `json:"task_id"`
		AttemptID string `json:"attempt_id"`
		AgentID   string `json:"agent_id"`
		Level     string `json:"level"`
		Message   string `json:"message"`
		Timestamp string `json:"timestamp"`
	} `json:"logs"`
}

// newID generates a random hex ID.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// LogsHandler returns an http.Handler for POST /api/logs and GET /api/logs.
func LogsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleIngest(w, r, db)
		case http.MethodGet:
			handleQuery(w, r, db)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func handleIngest(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req ingestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	ingested := 0

	for _, entry := range req.Logs {
		if strings.TrimSpace(entry.TaskID) == "" {
			continue
		}
		level := strings.ToLower(entry.Level)
		if level == "" {
			level = "info"
		}
		ts := entry.Timestamp
		if ts == "" {
			ts = now
		}

		id := newID()
		var attemptID, agentID any
		if entry.AttemptID != "" {
			attemptID = entry.AttemptID
		}
		if entry.AgentID != "" {
			agentID = entry.AgentID
		}

		_, err := db.Exec(
			`INSERT INTO task_logs (id, task_id, attempt_id, agent_id, level, message, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, entry.TaskID, attemptID, agentID, level, entry.Message, ts,
		)
		if err != nil {
			// skip individual bad rows (e.g. FK mismatch on tasks table)
			continue
		}
		ingested++
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int{"ingested": ingested})
}

func handleQuery(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	q := r.URL.Query()

	taskID := strings.TrimSpace(q.Get("task_id"))
	agentID := strings.TrimSpace(q.Get("agent_id"))
	level := strings.ToLower(strings.TrimSpace(q.Get("level")))
	since := strings.TrimSpace(q.Get("since"))
	until := strings.TrimSpace(q.Get("until"))

	limit := 100
	if l, err := strconv.Atoi(q.Get("limit")); err == nil {
		if l > 1000 {
			l = 1000
		}
		if l > 0 {
			limit = l
		}
	}
	offset := 0
	if o, err := strconv.Atoi(q.Get("offset")); err == nil && o > 0 {
		offset = o
	}

	var conditions []string
	var args []any

	if taskID != "" {
		conditions = append(conditions, "task_id = ?")
		args = append(args, taskID)
	}
	if agentID != "" {
		conditions = append(conditions, "agent_id = ?")
		args = append(args, agentID)
	}
	if level != "" {
		conditions = append(conditions, "level = ?")
		args = append(args, level)
	}
	if since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			conditions = append(conditions, "created_at >= ?")
			args = append(args, t.UTC().Format(time.RFC3339))
		}
	}
	if until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			conditions = append(conditions, "created_at <= ?")
			args = append(args, t.UTC().Format(time.RFC3339))
		}
	}

	baseQuery := "SELECT id, task_id, COALESCE(attempt_id,''), COALESCE(agent_id,''), level, message, created_at FROM task_logs"
	countQuery := "SELECT COUNT(*) FROM task_logs"
	if len(conditions) > 0 {
		where := " WHERE " + strings.Join(conditions, " AND ")
		baseQuery += where
		countQuery += where
	}
	baseQuery += " ORDER BY created_at ASC LIMIT ? OFFSET ?"

	var total int
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		http.Error(w, "count error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	queryArgs := append(args, limit, offset)
	rows, err := db.Query(baseQuery, queryArgs...)
	if err != nil {
		http.Error(w, "query error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		if err := rows.Scan(&e.ID, &e.TaskID, &e.AttemptID, &e.AgentID, &e.Level, &e.Message, &e.Timestamp); err != nil {
			http.Error(w, "scan error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "rows error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []LogEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"logs":  entries,
		"total": total,
	})
}
