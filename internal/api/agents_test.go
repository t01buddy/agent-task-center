package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/t01buddy/agent-task-center/internal/api"
)

func registerAgent(t *testing.T, handler http.Handler, agentID, name, runtime, domain string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"agent_id": agentID,
		"name":     name,
		"runtime":  runtime,
		"domain":   domain,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/agents/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("register agent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAgentRegister_HappyPath(t *testing.T) {
	conn := openTestDB(t, "agent-register-happy")
	handler := api.AgentsRegisterHandler(conn)

	body := `{"agent_id":"agent-1","name":"Worker A","runtime":"go","domain":"processing"}`
	req := httptest.NewRequest(http.MethodPost, "/api/agents/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["agent_id"] != "agent-1" {
		t.Errorf("expected agent_id=agent-1, got %v", resp["agent_id"])
	}
	if resp["status"] != "active" {
		t.Errorf("expected status=active, got %v", resp["status"])
	}
}

func TestAgentRegister_Idempotent(t *testing.T) {
	conn := openTestDB(t, "agent-register-idempotent")
	handler := api.AgentsRegisterHandler(conn)

	// Register twice — second call should update fields, not duplicate
	for i := 0; i < 2; i++ {
		body := `{"agent_id":"agent-x","name":"Worker X","runtime":"go","domain":"processing"}`
		req := httptest.NewRequest(http.MethodPost, "/api/agents/register", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("call %d: expected 200, got %d: %s", i+1, w.Code, w.Body.String())
		}
	}

	// Verify only 1 row exists
	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM agents WHERE id = 'agent-x'").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 agent row, got %d", count)
	}
}

func TestAgentRegister_MissingFields(t *testing.T) {
	conn := openTestDB(t, "agent-register-missing")
	handler := api.AgentsRegisterHandler(conn)

	cases := []struct {
		body string
		desc string
	}{
		{`{"name":"X","runtime":"go","domain":"d"}`, "missing agent_id"},
		{`{"agent_id":"a","runtime":"go","domain":"d"}`, "missing name"},
		{`{"agent_id":"a","name":"X","domain":"d"}`, "missing runtime"},
		{`{"agent_id":"a","name":"X","runtime":"go"}`, "missing domain"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/agents/register", bytes.NewBufferString(c.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", c.desc, w.Code)
		}
	}
}

func TestAgentHeartbeat_UpdatesTimestamp(t *testing.T) {
	conn := openTestDB(t, "agent-heartbeat")
	registerHandler := api.AgentsRegisterHandler(conn)
	routerHandler := api.AgentsRouterHandler(conn)

	registerAgent(t, registerHandler, "hb-agent", "HB Worker", "go", "infra")

	req := httptest.NewRequest(http.MethodPost, "/api/agents/hb-agent/heartbeat", nil)
	w := httptest.NewRecorder()
	routerHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "active" {
		t.Errorf("expected status=active, got %v", resp["status"])
	}
	if resp["last_heartbeat_at"] == nil || resp["last_heartbeat_at"] == "" {
		t.Error("expected non-empty last_heartbeat_at")
	}
}

func TestAgentHeartbeat_NotFound(t *testing.T) {
	conn := openTestDB(t, "agent-heartbeat-notfound")
	routerHandler := api.AgentsRouterHandler(conn)

	req := httptest.NewRequest(http.MethodPost, "/api/agents/no-such-agent/heartbeat", nil)
	w := httptest.NewRecorder()
	routerHandler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListAgents_Filters(t *testing.T) {
	conn := openTestDB(t, "agent-list-filters")
	regHandler := api.AgentsRegisterHandler(conn)
	listHandler := api.AgentsHandler(conn)

	// Register agents in different domains
	registerAgent(t, regHandler, "a1", "Agent 1", "go", "domain-a")
	registerAgent(t, regHandler, "a2", "Agent 2", "go", "domain-b")
	registerAgent(t, regHandler, "a3", "Agent 3", "go", "domain-a")

	// Filter by domain
	req := httptest.NewRequest(http.MethodGet, "/api/agents?domain=domain-a", nil)
	w := httptest.NewRecorder()
	listHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Agents []map[string]any `json:"agents"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Agents) != 2 {
		t.Errorf("expected 2 agents in domain-a, got %d", len(resp.Agents))
	}
}

func TestStaleDetection_Threshold(t *testing.T) {
	conn := openTestDB(t, "agent-stale")
	regHandler := api.AgentsRegisterHandler(conn)

	registerAgent(t, regHandler, "stale-agent", "Stale Worker", "go", "stale-domain")

	// Manually backdate the heartbeat to simulate staleness (> 60s ago)
	_, err := conn.Exec(
		`UPDATE agents SET last_heartbeat_at = datetime('now', '-120 seconds'), status = 'active' WHERE id = 'stale-agent'`,
	)
	if err != nil {
		t.Fatalf("backdate: %v", err)
	}

	// Simulate the stale detection logic (same as markStaleAgents in queue package)
	threshold := "datetime('now', '-60 seconds')"
	_, err = conn.Exec(`UPDATE agents SET status = 'stale' WHERE status = 'active' AND (last_heartbeat_at IS NULL OR last_heartbeat_at < ` + threshold + `)`)
	if err != nil {
		t.Fatalf("mark stale: %v", err)
	}

	var status string
	if err := conn.QueryRow("SELECT status FROM agents WHERE id = 'stale-agent'").Scan(&status); err != nil {
		t.Fatalf("query: %v", err)
	}
	if status != "stale" {
		t.Errorf("expected status=stale, got %s", status)
	}
}
