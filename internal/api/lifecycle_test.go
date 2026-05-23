package api_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/t01buddy/agent-task-center/internal/api"
	"github.com/t01buddy/agent-task-center/internal/db"
)

// openLifecycleDB opens a unique in-memory DB per test.
func openLifecycleDB(t *testing.T, name string) *sql.DB {
	t.Helper()
	conn, err := db.OpenDefault(fmt.Sprintf("file::memory:?mode=memory&cache=shared&_lc=%s", name))
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	conn.Exec("PRAGMA foreign_keys=OFF")
	t.Cleanup(func() { conn.Close() })
	return conn
}

// insertLeasedTaskLC inserts a leased task and a task_attempts row, returns attemptID.
func insertLeasedTaskLC(t *testing.T, conn *sql.DB, taskID, agentID string, fencingToken int64) string {
	t.Helper()
	conn.Exec(`INSERT INTO tasks (id, title, priority, status, assigned_agent_id,
		lease_expires_at, attempt_count, created_at, updated_at)
		VALUES (?, 'test task', 1, 'leased', ?, '2099-01-01T00:00:00Z', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		taskID, agentID)
	attemptID := "attempt-" + taskID
	conn.Exec(`INSERT INTO task_attempts (id, task_id, agent_id, fencing_token, started_at, expires_at)
		VALUES (?, ?, ?, ?, '2026-01-01T00:00:00Z', '2099-01-01T00:00:00Z')`,
		attemptID, taskID, agentID, fencingToken)
	return attemptID
}

func TestHeartbeat_Basic(t *testing.T) {
	conn := openLifecycleDB(t, "hb-basic")
	insertLeasedTaskLC(t, conn, "task-hb", "agent-1", 1)

	h := api.HeartbeatHandler(conn)
	body := `{"agent_id":"agent-1","fencing_token":1,"progress":50,"message":"halfway"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-hb/heartbeat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["lease_expires_at"] == "" {
		t.Error("expected lease_expires_at in response")
	}

	// Verify DB was updated
	var dbLease string
	conn.QueryRow("SELECT lease_expires_at FROM tasks WHERE id='task-hb'").Scan(&dbLease)
	if dbLease == "2099-01-01T00:00:00Z" {
		t.Error("expected lease_expires_at to be updated, but it wasn't")
	}

	// Verify heartbeat event appended
	var eventCount int
	conn.QueryRow("SELECT COUNT(*) FROM task_events WHERE task_id='task-hb' AND event_type='heartbeat'").Scan(&eventCount)
	if eventCount != 1 {
		t.Errorf("expected 1 heartbeat event, got %d", eventCount)
	}
}

func TestHeartbeat_StaleFencingToken(t *testing.T) {
	conn := openLifecycleDB(t, "hb-stale")
	insertLeasedTaskLC(t, conn, "task-hbs", "agent-1", 5)

	h := api.HeartbeatHandler(conn)
	body := `{"agent_id":"agent-1","fencing_token":99}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-hbs/heartbeat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "stale_fencing_token" {
		t.Errorf("expected stale_fencing_token error, got %v", resp["error"])
	}
}

func TestComplete_WithResult(t *testing.T) {
	conn := openLifecycleDB(t, "complete-result")
	insertLeasedTaskLC(t, conn, "task-c", "agent-1", 2)

	h := api.CompleteHandler(conn)
	body := `{"agent_id":"agent-1","fencing_token":2,"result":{"findings":0,"report_url":"http://example.com"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-c/complete", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "completed" {
		t.Errorf("expected status=completed, got %v", resp["status"])
	}

	// Verify attempt row closed
	var resultCode string
	conn.QueryRow("SELECT result_code FROM task_attempts WHERE id='attempt-task-c'").Scan(&resultCode)
	if resultCode != "completed" {
		t.Errorf("expected result_code=completed, got %q", resultCode)
	}

	// Verify completed event
	var eventCount int
	conn.QueryRow("SELECT COUNT(*) FROM task_events WHERE task_id='task-c' AND event_type='completed'").Scan(&eventCount)
	if eventCount != 1 {
		t.Errorf("expected 1 completed event, got %d", eventCount)
	}
}

func TestComplete_StaleFencingToken(t *testing.T) {
	conn := openLifecycleDB(t, "complete-stale")
	insertLeasedTaskLC(t, conn, "task-cs", "agent-1", 3)

	h := api.CompleteHandler(conn)
	body := `{"agent_id":"agent-1","fencing_token":99}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-cs/complete", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
	// Verify task NOT modified
	var status string
	conn.QueryRow("SELECT status FROM tasks WHERE id='task-cs'").Scan(&status)
	if status != "leased" {
		t.Errorf("expected task still leased, got %q", status)
	}
}

func TestFail_WithRetry(t *testing.T) {
	conn := openLifecycleDB(t, "fail-retry")
	// Insert task with attempt_count=1, task_type has max_attempts=3
	conn.Exec(`INSERT INTO tasks (id, title, priority, status, assigned_agent_id,
		lease_expires_at, attempt_count, created_at, updated_at)
		VALUES ('task-fr', 'retry task', 1, 'leased', 'agent-1', '2099-01-01T00:00:00Z', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	conn.Exec(`INSERT INTO task_attempts (id, task_id, agent_id, fencing_token, started_at, expires_at)
		VALUES ('attempt-fr', 'task-fr', 'agent-1', 1, '2026-01-01T00:00:00Z', '2099-01-01T00:00:00Z')`)

	h := api.FailHandler(conn)
	body := `{"agent_id":"agent-1","fencing_token":1,"reason":"timeout","retry_hint":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-fr/fail", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "queued" {
		t.Errorf("expected status=queued for retry, got %v", resp["status"])
	}

	// Verify failed event appended
	var eventCount int
	conn.QueryRow("SELECT COUNT(*) FROM task_events WHERE task_id='task-fr' AND event_type='failed'").Scan(&eventCount)
	if eventCount != 1 {
		t.Errorf("expected 1 failed event, got %d", eventCount)
	}
}

func TestFail_Exhausted(t *testing.T) {
	conn := openLifecycleDB(t, "fail-exhausted")
	// attempt_count=3 = max_attempts (default 3) — exhausted
	conn.Exec(`INSERT INTO tasks (id, title, priority, status, assigned_agent_id,
		lease_expires_at, attempt_count, created_at, updated_at)
		VALUES ('task-fe', 'exhausted task', 1, 'leased', 'agent-1', '2099-01-01T00:00:00Z', 3, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	conn.Exec(`INSERT INTO task_attempts (id, task_id, agent_id, fencing_token, started_at, expires_at)
		VALUES ('attempt-fe', 'task-fe', 'agent-1', 1, '2026-01-01T00:00:00Z', '2099-01-01T00:00:00Z')`)

	h := api.FailHandler(conn)
	body := `{"agent_id":"agent-1","fencing_token":1,"reason":"all attempts exhausted"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-fe/fail", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "failed" {
		t.Errorf("expected status=failed when exhausted, got %v", resp["status"])
	}
}

func TestFail_StaleFencingToken(t *testing.T) {
	conn := openLifecycleDB(t, "fail-stale")
	insertLeasedTaskLC(t, conn, "task-fs", "agent-1", 4)

	h := api.FailHandler(conn)
	body := `{"agent_id":"agent-1","fencing_token":99,"reason":"bad"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-fs/fail", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
	// Task NOT modified
	var status string
	conn.QueryRow("SELECT status FROM tasks WHERE id='task-fs'").Scan(&status)
	if status != "leased" {
		t.Errorf("expected task still leased, got %q", status)
	}
}
