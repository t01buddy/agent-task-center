package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/t01buddy/agent-task-center/internal/ai"
)

// ClassifyRequest is the body of POST /api/classify.
type ClassifyRequest struct {
	Title        string          `json:"title"`
	Context      json.RawMessage `json:"context"`
	RunID        string          `json:"run_id"`
	WorkflowName string          `json:"workflow_name"`
}

// ClassifyResponse is returned by POST /api/classify.
type ClassifyResponse struct {
	Task         *Task  `json:"task"`
	Classified   bool   `json:"classified"`
	ReusedCache  bool   `json:"reused_cache"`
	WorkflowName string `json:"workflow_name,omitempty"`
	Step         string `json:"step,omitempty"`
	Reasoning    string `json:"reasoning,omitempty"`
}

// ClassifyHandler returns an http.Handler for POST /api/classify.
func ClassifyHandler(db *sql.DB, provider ai.Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ClassifyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		req.Title = strings.TrimSpace(req.Title)
		if req.Title == "" {
			http.Error(w, `{"error":"title required"}`, http.StatusBadRequest)
			return
		}

		ctxJSON := "{}"
		if len(req.Context) > 0 && string(req.Context) != "null" {
			ctxJSON = string(req.Context)
		}

		contextHash := hashContext(req.Title, ctxJSON)

		// Look up existing task by title (most recent first)
		existing, err := findTaskByTitle(db, req.Title)
		if err != nil && err != sql.ErrNoRows {
			http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Cache hit: same title + same context → return existing task unchanged
		if existing != nil && existing.ContextHash != nil && *existing.ContextHash == contextHash {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(ClassifyResponse{
				Task:        existing,
				Classified:  false,
				ReusedCache: true,
			})
			return
		}

		// Determine workflow_name
		workflowName := req.WorkflowName
		if workflowName == "" && existing != nil && existing.WorkflowName != nil {
			workflowName = *existing.WorkflowName
		}

		// Load workflow definitions
		var workflowDefs []ai.WorkflowDef
		if workflowName != "" {
			// Load single known workflow
			def, err := loadWorkflowDef(db, workflowName)
			if err != nil {
				http.Error(w, "workflow not found: "+workflowName, http.StatusUnprocessableEntity)
				return
			}
			workflowDefs = []ai.WorkflowDef{{Name: workflowName, Definition: def}}
		} else {
			// Load all workflows for detection
			workflowDefs, err = loadAllWorkflowDefs(db)
			if err != nil {
				http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if len(workflowDefs) == 0 {
				http.Error(w, `{"error":"no workflows defined; create one via POST /api/workflows"}`, http.StatusUnprocessableEntity)
				return
			}
		}

		// Build current task state for the LLM
		var currentStep, currentStatus string
		if existing != nil {
			if existing.Step != nil {
				currentStep = *existing.Step
			}
			currentStatus = existing.Status
		}

		// Call LLM
		result, err := ai.Classify(provider, ai.ClassifyInput{
			Title:         req.Title,
			ContextJSON:   ctxJSON,
			Workflows:     workflowDefs,
			CurrentStep:   currentStep,
			CurrentStatus: currentStatus,
		})
		if err != nil {
			http.Error(w, "classify error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Inherit operational config from workflow for new tasks
		var visibilityTimeoutS, maxAttempts, retryBackoffS int
		if existing == nil {
			var vt, ma, rb sql.NullInt64
			_ = db.QueryRow(
				`SELECT default_visibility_timeout_s, default_max_attempts, default_retry_backoff_s FROM workflows WHERE name = ?`,
				result.WorkflowName,
			).Scan(&vt, &ma, &rb)
			visibilityTimeoutS = intOrDefault(vt, 300)
			maxAttempts = intOrDefault(ma, 3)
			retryBackoffS = intOrDefault(rb, 60)
		}

		now := time.Now().UTC().Format(time.RFC3339)

		var task *Task
		if existing != nil {
			// Update existing task: re-queue with new step/context
			_, err = db.Exec(
				`UPDATE tasks SET workflow_name=?, step=?, context=?, context_hash=?, domain=?, priority=?, status='queued', updated_at=? WHERE id=?`,
				result.WorkflowName, result.Step, ctxJSON, contextHash,
				nullStr(result.Domain), result.Priority, now, existing.ID,
			)
			if err != nil {
				http.Error(w, "db update error: "+err.Error(), http.StatusInternalServerError)
				return
			}
			_ = AppendEvent(db, existing.ID, "", "", "reclassified", strPtr(result.Reasoning))
			existing.WorkflowName = &result.WorkflowName
			existing.Step = &result.Step
			existing.Status = "queued"
			task = existing
		} else {
			// Insert new task
			id := newID()
			runID := strings.TrimSpace(req.RunID)
			_, err = db.Exec(
				`INSERT INTO tasks (id, workflow_name, step, run_id, title, context, context_hash, domain, priority,
				  visibility_timeout_s, max_attempts, retry_backoff_s, status, attempt_count, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'queued', 0, ?, ?)`,
				id, result.WorkflowName, result.Step, nullStr(runID), req.Title,
				ctxJSON, contextHash, nullStr(result.Domain), result.Priority,
				visibilityTimeoutS, maxAttempts, retryBackoffS, now, now,
			)
			if err != nil {
				http.Error(w, "db insert error: "+err.Error(), http.StatusInternalServerError)
				return
			}
			_ = AppendEvent(db, id, "", "", "classified", strPtr(result.Reasoning))
			task = taskFromInsert(id, req.Title, result, ctxJSON, contextHash, runID, visibilityTimeoutS, maxAttempts, retryBackoffS, now)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(ClassifyResponse{
			Task:         task,
			Classified:   true,
			ReusedCache:  false,
			WorkflowName: result.WorkflowName,
			Step:         result.Step,
			Reasoning:    result.Reasoning,
		})
	}
}

func hashContext(title, ctxJSON string) string {
	h := sha256.Sum256([]byte(title + ctxJSON))
	return fmt.Sprintf("%x", h)
}

func findTaskByTitle(db *sql.DB, title string) (*Task, error) {
	row := db.QueryRow(
		`SELECT id, workspace_id, workflow_name, step, run_id, domain, title, priority, context, context_hash,
		        visibility_timeout_s, max_attempts, retry_backoff_s, hard_deadline_s,
		        status, assigned_worker_id, lease_expires_at, retry_after, attempt_count, created_at, updated_at
		 FROM tasks WHERE title = ? ORDER BY created_at DESC LIMIT 1`, title,
	)
	return scanTaskRow(row)
}

func scanTaskRow(row *sql.Row) (*Task, error) {
	var t Task
	var workspaceID, workflowName, step, runID, domain, context, contextHash sql.NullString
	var hardDeadlineS sql.NullInt64
	var assignedWorkerID, leaseExpiresAt, retryAfter sql.NullString

	err := row.Scan(
		&t.ID, &workspaceID, &workflowName, &step, &runID, &domain, &t.Title, &t.Priority,
		&context, &contextHash,
		&t.VisibilityTimeoutS, &t.MaxAttempts, &t.RetryBackoffS, &hardDeadlineS,
		&t.Status, &assignedWorkerID, &leaseExpiresAt, &retryAfter, &t.AttemptCount, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if workspaceID.Valid {
		t.WorkspaceID = &workspaceID.String
	}
	if workflowName.Valid {
		t.WorkflowName = &workflowName.String
	}
	if step.Valid {
		t.Step = &step.String
	}
	if runID.Valid {
		t.RunID = &runID.String
	}
	if domain.Valid {
		t.Domain = &domain.String
	}
	if context.Valid {
		t.Context = &context.String
	}
	if contextHash.Valid {
		t.ContextHash = &contextHash.String
	}
	if hardDeadlineS.Valid {
		v := int(hardDeadlineS.Int64)
		t.HardDeadlineS = &v
	}
	if assignedWorkerID.Valid {
		t.AssignedWorkerID = &assignedWorkerID.String
	}
	if leaseExpiresAt.Valid {
		t.LeaseExpiresAt = &leaseExpiresAt.String
	}
	if retryAfter.Valid {
		t.RetryAfter = &retryAfter.String
	}
	return &t, nil
}

func loadWorkflowDef(db *sql.DB, name string) (string, error) {
	var def string
	err := db.QueryRow(`SELECT definition FROM workflows WHERE name = ?`, name).Scan(&def)
	return def, err
}

func loadAllWorkflowDefs(db *sql.DB) ([]ai.WorkflowDef, error) {
	rows, err := db.Query(`SELECT name, definition FROM workflows ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var defs []ai.WorkflowDef
	for rows.Next() {
		var d ai.WorkflowDef
		if err := rows.Scan(&d.Name, &d.Definition); err != nil {
			return nil, err
		}
		defs = append(defs, d)
	}
	return defs, rows.Err()
}

func intOrDefault(v sql.NullInt64, def int) int {
	if v.Valid {
		return int(v.Int64)
	}
	return def
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func taskFromInsert(id, title string, r ai.ClassifyResult, ctxJSON, ctxHash, runID string, vt, ma, rb int, now string) *Task {
	t := &Task{
		ID:                 id,
		Title:              title,
		WorkflowName:       &r.WorkflowName,
		Step:               &r.Step,
		Priority:           r.Priority,
		Context:            &ctxJSON,
		ContextHash:        &ctxHash,
		VisibilityTimeoutS: vt,
		MaxAttempts:        ma,
		RetryBackoffS:      rb,
		Status:             "queued",
		AttemptCount:       0,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if runID != "" {
		t.RunID = &runID
	}
	if r.Domain != "" {
		t.Domain = &r.Domain
	}
	return t
}
