package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/t01buddy/agent-task-center/internal/api"
)

func TestCreateTaskType_HappyPath(t *testing.T) {
	conn := openTestDB(t, "tt-create-happy")
	handler := api.TaskTypesHandler(conn)

	body := `{"name":"code-review","default_visibility_timeout_s":600,"max_attempts":5,"retry_backoff_s":30,"stale_heartbeat_threshold_s":90}`
	req := httptest.NewRequest(http.MethodPost, "/api/task-types", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["name"] != "code-review" {
		t.Errorf("expected name=code-review, got %v", resp["name"])
	}
	if resp["id"] == "" {
		t.Error("expected non-empty id")
	}
	if resp["default_visibility_timeout_s"] != float64(600) {
		t.Errorf("expected default_visibility_timeout_s=600, got %v", resp["default_visibility_timeout_s"])
	}
	if resp["max_attempts"] != float64(5) {
		t.Errorf("expected max_attempts=5, got %v", resp["max_attempts"])
	}
}

func TestCreateTaskType_Defaults(t *testing.T) {
	conn := openTestDB(t, "tt-create-defaults")
	handler := api.TaskTypesHandler(conn)

	body := `{"name":"minimal-type"}`
	req := httptest.NewRequest(http.MethodPost, "/api/task-types", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["default_visibility_timeout_s"] != float64(300) {
		t.Errorf("expected default dvt=300, got %v", resp["default_visibility_timeout_s"])
	}
	if resp["max_attempts"] != float64(3) {
		t.Errorf("expected default max_attempts=3, got %v", resp["max_attempts"])
	}
	if resp["retry_backoff_s"] != float64(60) {
		t.Errorf("expected default retry_backoff_s=60, got %v", resp["retry_backoff_s"])
	}
	if resp["stale_heartbeat_threshold_s"] != float64(60) {
		t.Errorf("expected default stale_heartbeat_threshold_s=60, got %v", resp["stale_heartbeat_threshold_s"])
	}
}

func TestCreateTaskType_DuplicateName(t *testing.T) {
	conn := openTestDB(t, "tt-create-dup")
	handler := api.TaskTypesHandler(conn)

	body := `{"name":"dup-type"}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/task-types", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if i == 0 && w.Code != http.StatusCreated {
			t.Fatalf("first create: expected 201, got %d", w.Code)
		}
		if i == 1 && w.Code != http.StatusConflict {
			t.Fatalf("second create: expected 409, got %d: %s", w.Code, w.Body.String())
		}
	}
}

func TestListTaskTypes(t *testing.T) {
	conn := openTestDB(t, "tt-list")
	handler := api.TaskTypesHandler(conn)

	for _, name := range []string{"type-a", "type-b"} {
		body, _ := json.Marshal(map[string]string{"name": name})
		req := httptest.NewRequest(http.MethodPost, "/api/task-types", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: expected 201, got %d", name, w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/task-types", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		TaskTypes []map[string]any `json:"task_types"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.TaskTypes) != 2 {
		t.Errorf("expected 2 task types, got %d", len(resp.TaskTypes))
	}
}
