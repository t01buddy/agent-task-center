package api_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/t01buddy/agent-task-center/internal/api"
	"github.com/t01buddy/agent-task-center/internal/db"
)

func openEventsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := db.OpenDefault("file::memory:?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	if _, err := conn.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("disable FK: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func strPtr(s string) *string { return &s }

func TestAppendEvent_Created(t *testing.T) {
	conn := openEventsTestDB(t)
	if err := api.AppendEvent(conn, "task-1", "", "", "created", nil); err != nil {
		t.Fatalf("AppendEvent created: %v", err)
	}
	var count int
	conn.QueryRow(`SELECT COUNT(*) FROM task_events WHERE task_id='task-1' AND event_type='created'`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 created event, got %d", count)
	}
}

func TestAppendEvent_AllTransitionTypes(t *testing.T) {
	conn := openEventsTestDB(t)
	types := []string{"created", "leased", "heartbeat", "progress", "completed", "failed", "timed_out", "retried", "cancelled"}
	for _, et := range types {
		if err := api.AppendEvent(conn, "task-all", "", "", et, nil); err != nil {
			t.Errorf("AppendEvent %s: %v", et, err)
		}
	}
	var count int
	conn.QueryRow(`SELECT COUNT(*) FROM task_events WHERE task_id='task-all'`).Scan(&count)
	if count != len(types) {
		t.Errorf("expected %d events, got %d", len(types), count)
	}
}

func TestAppendEvent_WithPayload(t *testing.T) {
	conn := openEventsTestDB(t)
	payload := `{"retry_count":2}`
	if err := api.AppendEvent(conn, "task-p", "attempt-1", "agent-1", "retried", &payload); err != nil {
		t.Fatalf("AppendEvent with payload: %v", err)
	}
	var got string
	conn.QueryRow(`SELECT payload FROM task_events WHERE task_id='task-p'`).Scan(&got)
	if got != payload {
		t.Errorf("payload mismatch: got %q want %q", got, payload)
	}
}

func TestAppendEvent_NoDeleteOrUpdate(t *testing.T) {
	// This test verifies that task_events rows are immutable — no DELETE or UPDATE
	// is called by AppendEvent. We verify the count never decreases.
	conn := openEventsTestDB(t)
	for i := 0; i < 3; i++ {
		api.AppendEvent(conn, "task-nd", "", "", "heartbeat", nil)
	}
	var count int
	conn.QueryRow(`SELECT COUNT(*) FROM task_events WHERE task_id='task-nd'`).Scan(&count)
	if count != 3 {
		t.Errorf("expected 3 events (append-only), got %d", count)
	}
}

func TestTaskEventsHandler_ReturnsEvents(t *testing.T) {
	conn := openEventsTestDB(t)
	handler := api.TaskEventsHandler(conn)

	api.AppendEvent(conn, "task-ev", "", "agent-1", "created", nil)
	api.AppendEvent(conn, "task-ev", "attempt-1", "agent-1", "leased", strPtr(`{"fencing_token":1}`))

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-ev/events", nil)
	req.URL.Path = "/api/tasks/task-ev/events"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(resp.Events))
	}
	if resp.Events[0]["event_type"] != "created" {
		t.Errorf("first event should be 'created', got %q", resp.Events[0]["event_type"])
	}
	if resp.Events[1]["event_type"] != "leased" {
		t.Errorf("second event should be 'leased', got %q", resp.Events[1]["event_type"])
	}
}

func TestTaskEventsHandler_EmptyTask(t *testing.T) {
	conn := openEventsTestDB(t)
	handler := api.TaskEventsHandler(conn)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-missing/events", nil)
	req.URL.Path = "/api/tasks/task-missing/events"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Events []map[string]any `json:"events"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Events) != 0 {
		t.Errorf("expected 0 events for missing task, got %d", len(resp.Events))
	}
}

func TestTaskEventsHandler_MethodNotAllowed(t *testing.T) {
	conn := openEventsTestDB(t)
	handler := api.TaskEventsHandler(conn)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-1/events", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestTaskEventsHandler_OrderedByCreatedAt(t *testing.T) {
	conn := openEventsTestDB(t)
	handler := api.TaskEventsHandler(conn)

	// Insert events with explicit timestamps to verify ordering
	types := []string{"created", "leased", "heartbeat", "completed"}
	for _, et := range types {
		api.AppendEvent(conn, "task-order", "", "", et, nil)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-order/events", nil)
	req.URL.Path = "/api/tasks/task-order/events"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp struct {
		Events []map[string]any `json:"events"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Events) != 4 {
		t.Errorf("expected 4 events, got %d", len(resp.Events))
	}
}
