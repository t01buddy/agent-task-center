package api_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/t01buddy/agent-task-center/internal/api"
	"github.com/t01buddy/agent-task-center/internal/db"
)

func openTestDB(t *testing.T, name string) *sql.DB {
	t.Helper()
	conn, err := db.OpenDefault("file::memory:?mode=memory&cache=shared&_testname=" + name)
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestMetricsHandler_EmptyDB(t *testing.T) {
	conn := openTestDB(t, "empty")

	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	rec := httptest.NewRecorder()
	api.MetricsHandler(conn).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp api.MetricsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Agents.Active != 0 || resp.Agents.Stale != 0 || resp.Agents.Offline != 0 {
		t.Errorf("expected zero agent counts, got %+v", resp.Agents)
	}
	if resp.Tasks.Queued != 0 || resp.Tasks.Completed != 0 {
		t.Errorf("expected zero task counts, got %+v", resp.Tasks)
	}
	if resp.Rates.RetryRate1h != 0 || resp.Rates.ThroughputPerMin10m != 0 {
		t.Errorf("expected zero rates, got %+v", resp.Rates)
	}
	if len(resp.DurationsByType) != 0 {
		t.Errorf("expected empty durations, got %v", resp.DurationsByType)
	}
}

func TestMetricsHandler_AgentCounts(t *testing.T) {
	conn := openTestDB(t, "agents")

	now := time.Now().UTC()
	activeTS := now.Add(-10 * time.Second).Format(time.RFC3339)
	staleTS := now.Add(-5 * time.Minute).Format(time.RFC3339)
	nowStr := now.Format(time.RFC3339)

	_, err := conn.Exec(`
		INSERT INTO agents(id,name,runtime,domain,last_heartbeat_at,registered_at) VALUES
		  ('a1','A1','go','coding',?,?),
		  ('a2','A2','go','coding',?,?),
		  ('a3','A3','go','coding',NULL,?)`,
		activeTS, nowStr,
		staleTS, nowStr,
		nowStr,
	)
	if err != nil {
		t.Fatalf("insert agents: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	rec := httptest.NewRecorder()
	api.MetricsHandler(conn).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp api.MetricsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Agents.Active != 1 {
		t.Errorf("active: want 1, got %d", resp.Agents.Active)
	}
	if resp.Agents.Stale != 1 {
		t.Errorf("stale: want 1, got %d", resp.Agents.Stale)
	}
	if resp.Agents.Offline != 1 {
		t.Errorf("offline: want 1, got %d", resp.Agents.Offline)
	}
}

func TestMetricsHandler_TaskCounts(t *testing.T) {
	conn := openTestDB(t, "tasks")

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := conn.Exec(`
		INSERT INTO tasks(id,title,status,priority,created_at,updated_at) VALUES
		  ('t1','T1','queued',0,?,?),
		  ('t2','T2','queued',0,?,?),
		  ('t3','T3','completed',0,?,?),
		  ('t4','T4','failed',0,?,?)`,
		now, now, now, now, now, now, now, now,
	)
	if err != nil {
		t.Fatalf("insert tasks: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	rec := httptest.NewRecorder()
	api.MetricsHandler(conn).ServeHTTP(rec, req)

	var resp api.MetricsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Tasks.Queued != 2 {
		t.Errorf("queued: want 2, got %d", resp.Tasks.Queued)
	}
	if resp.Tasks.Completed != 1 {
		t.Errorf("completed: want 1, got %d", resp.Tasks.Completed)
	}
	if resp.Tasks.Failed != 1 {
		t.Errorf("failed: want 1, got %d", resp.Tasks.Failed)
	}
}

func TestMetricsHandler_MethodNotAllowed(t *testing.T) {
	conn := openTestDB(t, "method")

	req := httptest.NewRequest(http.MethodPost, "/api/metrics", nil)
	rec := httptest.NewRecorder()
	api.MetricsHandler(conn).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}
