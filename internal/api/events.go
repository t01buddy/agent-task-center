package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// TaskEvent is one row from task_events.
type TaskEvent struct {
	ID        string  `json:"id"`
	TaskID    string  `json:"task_id"`
	AttemptID string  `json:"attempt_id,omitempty"`
	WorkerID  string  `json:"worker_id,omitempty"`
	EventType string  `json:"event_type"`
	Payload   *string `json:"payload,omitempty"`
	CreatedAt string  `json:"created_at"`
}

// AppendEvent writes an immutable event row for a task state transition.
// It is safe to call inside a transaction (pass tx as db) or outside.
// payload may be nil.
func AppendEvent(db interface {
	Exec(string, ...any) (sql.Result, error)
}, taskID, attemptID, workerID, eventType string, payload *string) error {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	now := time.Now().UTC().Format(time.RFC3339)

	var aID, wID any
	if attemptID != "" {
		aID = attemptID
	}
	if workerID != "" {
		wID = workerID
	}
	var p any
	if payload != nil {
		p = *payload
	}

	_, err := db.Exec(
		`INSERT INTO task_events (id, task_id, attempt_id, worker_id, event_type, payload, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, taskID, aID, wID, eventType, p, now,
	)
	return err
}

// TaskEventsHandler returns an http.Handler for GET /api/tasks/{id}/events.
// URL pattern: /api/tasks/{id}/events  — id is the path segment after /api/tasks/
func TaskEventsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract task ID from path: /api/tasks/<id>/events
		path := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
		path = strings.TrimSuffix(path, "/events")
		taskID := strings.TrimSpace(path)
		if taskID == "" {
			http.Error(w, "missing task id", http.StatusBadRequest)
			return
		}

		rows, err := db.Query(
			`SELECT id, task_id, COALESCE(attempt_id,''), COALESCE(worker_id,''), event_type,
			        payload, created_at
			 FROM task_events
			 WHERE task_id = ?
			 ORDER BY created_at ASC`,
			taskID,
		)
		if err != nil {
			http.Error(w, "query error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var events []TaskEvent
		for rows.Next() {
			var e TaskEvent
			var payload sql.NullString
			if err := rows.Scan(&e.ID, &e.TaskID, &e.AttemptID, &e.WorkerID, &e.EventType, &payload, &e.CreatedAt); err != nil {
				http.Error(w, "scan error: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if payload.Valid {
				s := payload.String
				e.Payload = &s
			}
			events = append(events, e)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "rows error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if events == nil {
			events = []TaskEvent{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"events": events})
	}
}
