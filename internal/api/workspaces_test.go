package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/t01buddy/agent-task-center/internal/api"
)

func TestCreateWorkspace_HappyPath(t *testing.T) {
	conn := openTestDB(t, "ws-create-happy")
	handler := api.WorkspacesHandler(conn)

	body := `{"name":"my-workspace"}`
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewBufferString(body))
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
	if resp["name"] != "my-workspace" {
		t.Errorf("expected name=my-workspace, got %v", resp["name"])
	}
	if resp["id"] == "" {
		t.Error("expected non-empty id")
	}
	if resp["created_at"] == "" {
		t.Error("expected non-empty created_at")
	}
}

func TestCreateWorkspace_DuplicateName(t *testing.T) {
	conn := openTestDB(t, "ws-create-dup")
	handler := api.WorkspacesHandler(conn)

	body := `{"name":"dup-ws"}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewBufferString(body))
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

func TestCreateWorkspace_EmptyName(t *testing.T) {
	conn := openTestDB(t, "ws-create-empty")
	handler := api.WorkspacesHandler(conn)

	body := `{"name":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListWorkspaces_OrderByCreatedAt(t *testing.T) {
	conn := openTestDB(t, "ws-list-order")
	handler := api.WorkspacesHandler(conn)

	for _, name := range []string{"alpha", "beta", "gamma"} {
		body, _ := json.Marshal(map[string]string{"name": name})
		req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: expected 201, got %d", name, w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Workspaces []map[string]any `json:"workspaces"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Workspaces) != 3 {
		t.Errorf("expected 3 workspaces, got %d", len(resp.Workspaces))
	}
	if resp.Workspaces[0]["name"] != "alpha" {
		t.Errorf("expected first workspace to be alpha, got %v", resp.Workspaces[0]["name"])
	}
}
