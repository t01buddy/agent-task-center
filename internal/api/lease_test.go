package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/t01buddy/agent-task-center/internal/api"
)

func mustPost(path, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func leaseBody(agentID string) *bytes.Buffer {
	b, _ := json.Marshal(map[string]any{"agent_id": agentID})
	return bytes.NewBuffer(b)
}

func TestLease_NoTask(t *testing.T) {
	conn := openTestDB(t, "lease-no-task")
	h := api.LeaseHandler(conn)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/lease", leaseBody("agent-1"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLease_Basic(t *testing.T) {
	conn := openTestDB(t, "lease-basic")

	// Create a queued task directly in DB
	_, err := conn.Exec(
		`INSERT INTO tasks (id, title, priority, status, created_at, updated_at)
		 VALUES ('task-1', 'my task', 5, 'queued', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	h := api.LeaseHandler(conn)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/lease", leaseBody("agent-1"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Task struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"task"`
		FencingToken   int64  `json:"fencing_token"`
		LeaseExpiresAt string `json:"lease_expires_at"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Task.ID != "task-1" {
		t.Errorf("expected task-1, got %s", resp.Task.ID)
	}
	if resp.Task.Status != "leased" {
		t.Errorf("expected status=leased, got %s", resp.Task.Status)
	}
	if resp.FencingToken != 1 {
		t.Errorf("expected fencing_token=1, got %d", resp.FencingToken)
	}
	if resp.LeaseExpiresAt == "" {
		t.Error("expected lease_expires_at to be set")
	}

	// Second lease request should get 204 (task now leased)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/api/tasks/lease", leaseBody("agent-2")))
	if w2.Code != http.StatusNoContent {
		t.Errorf("expected 204 on second lease, got %d", w2.Code)
	}
}

func TestLease_PriorityOrder(t *testing.T) {
	conn := openTestDB(t, "lease-priority")

	// Insert two tasks with different priorities
	conn.Exec(`INSERT INTO tasks (id, title, priority, status, created_at, updated_at)
		VALUES ('low', 'low priority', 1, 'queued', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	conn.Exec(`INSERT INTO tasks (id, title, priority, status, created_at, updated_at)
		VALUES ('high', 'high priority', 10, 'queued', '2026-01-01T00:00:01Z', '2026-01-01T00:00:01Z')`)

	h := api.LeaseHandler(conn)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/lease", leaseBody("agent-1"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Task.ID != "high" {
		t.Errorf("expected high-priority task first, got %s", resp.Task.ID)
	}
}

func TestLease_FencingTokenIncrement(t *testing.T) {
	conn := openTestDB(t, "lease-fencing-increment")

	conn.Exec(`INSERT INTO tasks (id, title, priority, status, created_at, updated_at)
		VALUES ('task-ft', 'fencing test', 1, 'queued', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)

	h := api.LeaseHandler(conn)

	// First lease: token=1
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, mustPost("/api/tasks/lease", `{"agent_id":"a1"}`))
	var r1 struct{ FencingToken int64 `json:"fencing_token"` }
	json.NewDecoder(w1.Body).Decode(&r1)
	if r1.FencingToken != 1 {
		t.Errorf("expected token=1, got %d", r1.FencingToken)
	}

	// Reset task to queued manually for second lease
	conn.Exec(`UPDATE tasks SET status='queued', assigned_agent_id=NULL, lease_expires_at=NULL WHERE id='task-ft'`)

	// Second lease: token=2
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, mustPost("/api/tasks/lease", `{"agent_id":"a2"}`))
	var r2 struct{ FencingToken int64 `json:"fencing_token"` }
	json.NewDecoder(w2.Body).Decode(&r2)
	if r2.FencingToken != 2 {
		t.Errorf("expected token=2, got %d", r2.FencingToken)
	}
}

func TestLease_WithFilters(t *testing.T) {
	conn := openTestDB(t, "lease-filters")

	conn.Exec(`INSERT INTO tasks (id, title, priority, status, domain, created_at, updated_at)
		VALUES ('t-coding', 'coding task', 1, 'queued', 'coding', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	conn.Exec(`INSERT INTO tasks (id, title, priority, status, domain, created_at, updated_at)
		VALUES ('t-review', 'review task', 1, 'queued', 'review', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)

	h := api.LeaseHandler(conn)

	// Request for domain=coding should only get coding task
	body := `{"agent_id":"a1","domain":"coding"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/lease", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Task.ID != "t-coding" {
		t.Errorf("expected t-coding, got %s", resp.Task.ID)
	}
}

func TestLease_ValidateFencingToken_Stale(t *testing.T) {
	conn := openTestDB(t, "lease-stale-token")

	conn.Exec(`INSERT INTO tasks (id, title, priority, status, created_at, updated_at)
		VALUES ('task-stale', 'stale test', 1, 'queued', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)

	h := api.LeaseHandler(conn)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, mustPost("/api/tasks/lease", `{"agent_id":"a1"}`))

	// Use token 99 (wrong) — should fail stale check
	_, err := api.ValidateFencingToken(conn, "task-stale", 99)
	if err == nil {
		t.Error("expected stale fencing token error")
	}
	if !api.IsStaleFencingToken(err) {
		t.Errorf("expected IsStaleFencingToken=true, got err=%v", err)
	}

	// Use correct token 1 — should succeed
	_, err = api.ValidateFencingToken(conn, "task-stale", 1)
	if err != nil {
		t.Errorf("expected valid token to succeed, got %v", err)
	}
}

func TestLease_Concurrent_OneWins(t *testing.T) {
	conn := openTestDB(t, "lease-concurrent")

	// Insert exactly 1 queued task
	conn.Exec(`INSERT INTO tasks (id, title, priority, status, created_at, updated_at)
		VALUES ('task-race', 'race condition test', 1, 'queued', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)

	h := api.LeaseHandler(conn)

	const workers = 50
	results := make([]int, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body := leaseBody("agent")
			req := httptest.NewRequest(http.MethodPost, "/api/tasks/lease", body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			results[idx] = w.Code
		}(i)
	}
	wg.Wait()

	successes := 0
	noContents := 0
	for _, code := range results {
		switch code {
		case http.StatusOK:
			successes++
		case http.StatusNoContent:
			noContents++
		default:
			t.Errorf("unexpected status: %d", code)
		}
	}

	if successes != 1 {
		t.Errorf("expected exactly 1 success, got %d", successes)
	}
	if noContents != workers-1 {
		t.Errorf("expected %d no-content, got %d", workers-1, noContents)
	}
}

func TestLease_MissingAgentID(t *testing.T) {
	conn := openTestDB(t, "lease-missing-agent")
	h := api.LeaseHandler(conn)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/lease", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
