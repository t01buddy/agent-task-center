package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/t01buddy/agent-task-center/internal/api"
)

// createTask creates a queued task via INSERT and returns its ID.
func createTestTask(t *testing.T, conn interface {
	Exec(string, ...any) (interface{ LastInsertId() (int64, error) }, error)
}, title string) string {
	t.Helper()
	return "" // not used in this test — we use the handler
}

func TestLease_BasicLease(t *testing.T) {
	conn := openTestDB(t, "lease-basic")
	taskHandler := api.TasksHandler(conn)
	leaseHandler := api.LeaseHandler(conn)

	// Create a queued task
	body := `{"title":"test task","priority":10}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	taskHandler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create task: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created map[string]any
	json.NewDecoder(w.Body).Decode(&created)
	taskID := created["id"].(string)

	// Lease the task
	leaseBody := `{"agent_id":"agent-1"}`
	lr := httptest.NewRequest(http.MethodPost, "/api/tasks/lease", bytes.NewBufferString(leaseBody))
	lr.Header.Set("Content-Type", "application/json")
	lw := httptest.NewRecorder()
	leaseHandler.ServeHTTP(lw, lr)

	if lw.Code != http.StatusOK {
		t.Fatalf("lease: expected 200, got %d: %s", lw.Code, lw.Body.String())
	}

	var leaseResp map[string]any
	json.NewDecoder(lw.Body).Decode(&leaseResp)

	if leaseResp["fencing_token"] == nil {
		t.Error("expected fencing_token in response")
	}
	if leaseResp["lease_expires_at"] == nil {
		t.Error("expected lease_expires_at in response")
	}
	task := leaseResp["task"].(map[string]any)
	if task["id"] != taskID {
		t.Errorf("expected task id %s, got %v", taskID, task["id"])
	}
	if task["status"] != "leased" {
		t.Errorf("expected status=leased, got %v", task["status"])
	}
	if leaseResp["fencing_token"].(float64) != 1 {
		t.Errorf("expected fencing_token=1, got %v", leaseResp["fencing_token"])
	}
}

func TestLease_NoEligibleTask(t *testing.T) {
	conn := openTestDB(t, "lease-no-task")
	leaseHandler := api.LeaseHandler(conn)

	lr := httptest.NewRequest(http.MethodPost, "/api/tasks/lease", bytes.NewBufferString(`{"agent_id":"agent-1"}`))
	lr.Header.Set("Content-Type", "application/json")
	lw := httptest.NewRecorder()
	leaseHandler.ServeHTTP(lw, lr)

	if lw.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", lw.Code, lw.Body.String())
	}
}

func TestLease_FencingTokenMonotonicallyIncreasing(t *testing.T) {
	conn := openTestDB(t, "lease-fencing-tokens")

	// Manually insert a task and simulate two sequential leases.
	// Insert task
	taskHandler := api.TasksHandler(conn)
	body := `{"title":"fencing-test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	taskHandler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d", w.Code)
	}
	var created map[string]any
	json.NewDecoder(w.Body).Decode(&created)
	taskID := created["id"].(string)

	leaseHandler := api.LeaseHandler(conn)

	// First lease
	lr1 := httptest.NewRequest(http.MethodPost, "/api/tasks/lease", bytes.NewBufferString(`{"agent_id":"agent-1"}`))
	lw1 := httptest.NewRecorder()
	leaseHandler.ServeHTTP(lw1, lr1)
	if lw1.Code != http.StatusOK {
		t.Fatalf("lease 1: %d", lw1.Code)
	}
	var r1 map[string]any
	json.NewDecoder(lw1.Body).Decode(&r1)
	token1 := int(r1["fencing_token"].(float64))

	// Reset task to queued for second lease (simulate timeout recovery)
	conn.Exec("UPDATE tasks SET status='queued', assigned_agent_id=NULL, lease_expires_at=NULL, retry_after=NULL WHERE id=?", taskID)

	// Second lease
	lr2 := httptest.NewRequest(http.MethodPost, "/api/tasks/lease", bytes.NewBufferString(`{"agent_id":"agent-2"}`))
	lw2 := httptest.NewRecorder()
	leaseHandler.ServeHTTP(lw2, lr2)
	if lw2.Code != http.StatusOK {
		t.Fatalf("lease 2: %d", lw2.Code)
	}
	var r2 map[string]any
	json.NewDecoder(lw2.Body).Decode(&r2)
	token2 := int(r2["fencing_token"].(float64))

	if token2 <= token1 {
		t.Errorf("expected token2 > token1, got %d <= %d", token2, token1)
	}
}

func TestLease_StaleFencingToken(t *testing.T) {
	conn := openTestDB(t, "lease-stale-token")

	// Validate stale token returns error
	err := api.ValidateFencingToken(conn, "nonexistent-task", 99)
	if err == nil {
		t.Fatal("expected error for stale token")
	}
	if !api.IsStaleFencingTokenError(err) {
		t.Errorf("expected stale fencing token error, got: %v", err)
	}
}

func TestLease_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	conn := openTestDB(t, "lease-stress")

	// Create 1 queued task
	taskHandler := api.TasksHandler(conn)
	body := `{"title":"stress-task","priority":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	taskHandler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d", w.Code)
	}

	leaseHandler := api.LeaseHandler(conn)

	var successes int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			leaseBody := fmt.Sprintf(`{"agent_id":"agent-%d"}`, idx)
			lr := httptest.NewRequest(http.MethodPost, "/api/tasks/lease", bytes.NewBufferString(leaseBody))
			lw := httptest.NewRecorder()
			leaseHandler.ServeHTTP(lw, lr)
			if lw.Code == http.StatusOK {
				atomic.AddInt64(&successes, 1)
			}
		}(i)
	}
	wg.Wait()

	// Allow brief settle for any in-flight transactions
	time.Sleep(10 * time.Millisecond)

	if successes != 1 {
		t.Errorf("expected exactly 1 successful lease, got %d", successes)
	}
}
