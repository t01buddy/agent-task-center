package api_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/t01buddy/agent-task-center/internal/api"
	"github.com/t01buddy/agent-task-center/internal/db"
)

func openTestDB(t *testing.T, name string) *sql.DB {
	t.Helper()
	conn, err := db.OpenDefault("file::memory:?mode=memory&cache=shared&_testname=" + name)
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	// Disable FK constraints so log entries can be inserted without parent tasks/agents.
	if _, err := conn.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("disable FK: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestIngestLogs_Basic(t *testing.T) {
	conn := openTestDB(t, "ingest-basic")
	handler := api.LogsHandler(conn)

	body := `{"logs":[
		{"task_id":"task-1","level":"info","message":"hello"},
		{"task_id":"task-1","level":"error","message":"oops"}
	]}`
	req := httptest.NewRequest(http.MethodPost, "/api/logs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]int
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["ingested"] != 2 {
		t.Errorf("expected ingested=2, got %d", resp["ingested"])
	}
}

func TestIngestLogs_MissingTaskID(t *testing.T) {
	conn := openTestDB(t, "ingest-missing-task")
	handler := api.LogsHandler(conn)

	body := `{"logs":[
		{"task_id":"","level":"info","message":"no task"},
		{"task_id":"task-2","level":"info","message":"valid"}
	]}`
	req := httptest.NewRequest(http.MethodPost, "/api/logs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	var resp map[string]int
	json.NewDecoder(w.Body).Decode(&resp)
	// only the entry with task_id is inserted
	if resp["ingested"] != 1 {
		t.Errorf("expected ingested=1, got %d", resp["ingested"])
	}
}

func TestQueryLogs_Filters(t *testing.T) {
	conn := openTestDB(t, "query-filters")
	handler := api.LogsHandler(conn)

	// Ingest 3 entries
	body := `{"logs":[
		{"task_id":"task-A","agent_id":"agent-1","level":"info","message":"msg1","timestamp":"2026-01-01T10:00:00Z"},
		{"task_id":"task-A","agent_id":"agent-2","level":"warn","message":"msg2","timestamp":"2026-01-01T11:00:00Z"},
		{"task_id":"task-B","agent_id":"agent-1","level":"error","message":"msg3","timestamp":"2026-01-01T12:00:00Z"}
	]}`
	req := httptest.NewRequest(http.MethodPost, "/api/logs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	tests := []struct {
		name        string
		query       string
		wantTotal   int
		wantCount   int
	}{
		{"all", "", 3, 3},
		{"by task", "task_id=task-A", 2, 2},
		{"by agent", "agent_id=agent-1", 2, 2},
		{"by level", "level=error", 1, 1},
		{"by task+level", "task_id=task-A&level=warn", 1, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/logs?"+tc.query, nil)
			rw := httptest.NewRecorder()
			handler.ServeHTTP(rw, r)

			if rw.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rw.Code, rw.Body.String())
			}
			var resp struct {
				Logs  []map[string]any `json:"logs"`
				Total int              `json:"total"`
			}
			if err := json.NewDecoder(rw.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Total != tc.wantTotal {
				t.Errorf("total: want %d, got %d", tc.wantTotal, resp.Total)
			}
			if len(resp.Logs) != tc.wantCount {
				t.Errorf("logs count: want %d, got %d", tc.wantCount, len(resp.Logs))
			}
		})
	}
}

func TestQueryLogs_Pagination(t *testing.T) {
	conn := openTestDB(t, "query-pagination")
	handler := api.LogsHandler(conn)

	// Ingest 5 entries
	var logs []map[string]string
	for i := 0; i < 5; i++ {
		logs = append(logs, map[string]string{
			"task_id": "task-P",
			"level":   "info",
			"message": "msg",
		})
	}
	bodyBytes, _ := json.Marshal(map[string]any{"logs": logs})
	req := httptest.NewRequest(http.MethodPost, "/api/logs", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Page 1: limit=2, offset=0
	r := httptest.NewRequest(http.MethodGet, "/api/logs?task_id=task-P&limit=2&offset=0", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, r)

	var resp struct {
		Logs  []map[string]any `json:"logs"`
		Total int              `json:"total"`
	}
	json.NewDecoder(rw.Body).Decode(&resp)

	if resp.Total != 5 {
		t.Errorf("expected total=5, got %d", resp.Total)
	}
	if len(resp.Logs) != 2 {
		t.Errorf("expected 2 logs on page 1, got %d", len(resp.Logs))
	}

	// Page 2: limit=2, offset=2
	r2 := httptest.NewRequest(http.MethodGet, "/api/logs?task_id=task-P&limit=2&offset=2", nil)
	rw2 := httptest.NewRecorder()
	handler.ServeHTTP(rw2, r2)
	var resp2 struct {
		Logs  []map[string]any `json:"logs"`
		Total int              `json:"total"`
	}
	json.NewDecoder(rw2.Body).Decode(&resp2)
	if len(resp2.Logs) != 2 {
		t.Errorf("expected 2 logs on page 2, got %d", len(resp2.Logs))
	}
}

func TestMethodNotAllowed(t *testing.T) {
	conn := openTestDB(t, "method-not-allowed")
	handler := api.LogsHandler(conn)

	req := httptest.NewRequest(http.MethodDelete, "/api/logs", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}
