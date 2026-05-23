package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/t01buddy/agent-task-center/internal/api"
)

func TestCreateTask_Basic(t *testing.T) {
	conn := openTestDB(t, "create-basic")
	h := api.TasksHandler(conn)

	body := `{"title":"my task","priority":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var task map[string]any
	if err := json.NewDecoder(w.Body).Decode(&task); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if task["title"] != "my task" {
		t.Errorf("title mismatch: %v", task["title"])
	}
	if task["status"] != "queued" {
		t.Errorf("expected status=queued, got %v", task["status"])
	}
	if task["id"] == nil || task["id"] == "" {
		t.Errorf("expected non-empty id")
	}
}

func TestCreateTask_MissingTitle(t *testing.T) {
	conn := openTestDB(t, "create-missing-title")
	h := api.TasksHandler(conn)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewBufferString(`{"priority":1}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestListTasks_Empty(t *testing.T) {
	conn := openTestDB(t, "list-empty")
	h := api.TasksHandler(conn)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Tasks []any `json:"tasks"`
		Total int   `json:"total"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 0 {
		t.Errorf("expected total=0, got %d", resp.Total)
	}
}

func TestListTasks_Filters(t *testing.T) {
	conn := openTestDB(t, "list-filters")
	ch := api.TasksHandler(conn)

	// Create two tasks with different domains
	ch.ServeHTTP(httptest.NewRecorder(), mustPost("/api/tasks", `{"title":"t1","priority":1}`))
	body2 := `{"title":"t2","domain":"review","priority":2}`
	ch.ServeHTTP(httptest.NewRecorder(), mustPost("/api/tasks", body2))

	// Filter by domain
	req := httptest.NewRequest(http.MethodGet, "/api/tasks?domain=review", nil)
	w := httptest.NewRecorder()
	ch.ServeHTTP(w, req)

	var resp struct {
		Tasks []map[string]any `json:"tasks"`
		Total int              `json:"total"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 1 {
		t.Errorf("expected total=1 filtered by domain, got %d", resp.Total)
	}
}

func TestListTasks_StatusFilter(t *testing.T) {
	conn := openTestDB(t, "list-status-filter")
	h := api.TasksHandler(conn)

	// Create task
	w0 := httptest.NewRecorder()
	h.ServeHTTP(w0, mustPost("/api/tasks", `{"title":"t1"}`))

	// Filter by status=queued
	req := httptest.NewRequest(http.MethodGet, "/api/tasks?status=queued", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp struct {
		Tasks []map[string]any `json:"tasks"`
		Total int              `json:"total"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 1 {
		t.Errorf("expected total=1, got %d", resp.Total)
	}

	// Filter by status=completed (none)
	req2 := httptest.NewRequest(http.MethodGet, "/api/tasks?status=completed", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	var resp2 struct {
		Total int `json:"total"`
	}
	json.NewDecoder(w2.Body).Decode(&resp2)
	if resp2.Total != 0 {
		t.Errorf("expected total=0, got %d", resp2.Total)
	}
}

func TestListTasks_Pagination(t *testing.T) {
	conn := openTestDB(t, "list-pagination")
	h := api.TasksHandler(conn)

	// Create 5 tasks
	for i := 0; i < 5; i++ {
		h.ServeHTTP(httptest.NewRecorder(), mustPost("/api/tasks", `{"title":"t"}`))
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks?limit=2&offset=0", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp struct {
		Tasks []map[string]any `json:"tasks"`
		Total int              `json:"total"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 5 {
		t.Errorf("expected total=5, got %d", resp.Total)
	}
	if len(resp.Tasks) != 2 {
		t.Errorf("expected 2 tasks on page, got %d", len(resp.Tasks))
	}
}

func TestUpdateTask_Queued(t *testing.T) {
	conn := openTestDB(t, "update-queued")
	ch := api.TasksHandler(conn)
	bh := api.TaskByIDHandler(conn)

	// Create task
	w0 := httptest.NewRecorder()
	ch.ServeHTTP(w0, mustPost("/api/tasks", `{"title":"original","priority":1}`))
	var created map[string]any
	json.NewDecoder(w0.Body).Decode(&created)
	id := created["id"].(string)

	// Patch it
	patchBody := `{"title":"updated","priority":10}`
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/"+id, bytes.NewBufferString(patchBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	bh.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated map[string]any
	json.NewDecoder(w.Body).Decode(&updated)
	if updated["title"] != "updated" {
		t.Errorf("title not updated: %v", updated["title"])
	}
	if updated["priority"].(float64) != 10 {
		t.Errorf("priority not updated: %v", updated["priority"])
	}
}

func TestUpdateTask_WrongStatus(t *testing.T) {
	conn := openTestDB(t, "update-wrong-status")
	bh := api.TaskByIDHandler(conn)

	// Insert a leased task directly
	_, _ = conn.Exec(
		`INSERT INTO tasks (id, title, priority, status, created_at, updated_at)
		 VALUES ('task-leased', 'leased task', 0, 'leased', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	)

	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/task-leased", bytes.NewBufferString(`{"priority":5}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	bh.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func TestCancelTask_Queued(t *testing.T) {
	conn := openTestDB(t, "cancel-queued")
	ch := api.TasksHandler(conn)
	bh := api.TaskByIDHandler(conn)

	// Create task
	w0 := httptest.NewRecorder()
	ch.ServeHTTP(w0, mustPost("/api/tasks", `{"title":"to cancel"}`))
	var created map[string]any
	json.NewDecoder(w0.Body).Decode(&created)
	id := created["id"].(string)

	// Delete (cancel)
	req := httptest.NewRequest(http.MethodDelete, "/api/tasks/"+id, nil)
	w := httptest.NewRecorder()
	bh.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCancelTask_Leased(t *testing.T) {
	conn := openTestDB(t, "cancel-leased")
	bh := api.TaskByIDHandler(conn)

	_, _ = conn.Exec(
		`INSERT INTO tasks (id, title, priority, status, created_at, updated_at)
		 VALUES ('task-L', 'leased', 0, 'leased', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	)

	req := httptest.NewRequest(http.MethodDelete, "/api/tasks/task-L", nil)
	w := httptest.NewRecorder()
	bh.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func mustPost(path, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}
