package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

// Task is a single task row returned from the API.
type Task struct {
	ID              string  `json:"id"`
	WorkspaceID     *string `json:"workspace_id"`
	Domain          *string `json:"domain"`
	TaskTypeID      *string `json:"task_type_id"`
	Title           string  `json:"title"`
	Priority        int     `json:"priority"`
	Context         *string `json:"context"`
	Status          string  `json:"status"`
	AssignedAgentID *string `json:"assigned_agent_id"`
	LeaseExpiresAt  *string `json:"lease_expires_at"`
	AttemptCount    int     `json:"attempt_count"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func getTaskByID(db *sql.DB, id string) (Task, error) {
	row := db.QueryRow(
		`SELECT id, workspace_id, domain, task_type_id, title, priority, context, status,
		        assigned_agent_id, lease_expires_at, attempt_count, created_at, updated_at
		 FROM tasks WHERE id = ?`, id,
	)
	return scanTask(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(s scanner) (Task, error) {
	var t Task
	var workspaceID, domain, taskTypeID, context, assignedAgentID, leaseExpiresAt sql.NullString
	err := s.Scan(
		&t.ID, &workspaceID, &domain, &taskTypeID, &t.Title, &t.Priority,
		&context, &t.Status, &assignedAgentID, &leaseExpiresAt,
		&t.AttemptCount, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return Task{}, err
	}
	if workspaceID.Valid {
		t.WorkspaceID = &workspaceID.String
	}
	if domain.Valid {
		t.Domain = &domain.String
	}
	if taskTypeID.Valid {
		t.TaskTypeID = &taskTypeID.String
	}
	if context.Valid {
		t.Context = &context.String
	}
	if assignedAgentID.Valid {
		t.AssignedAgentID = &assignedAgentID.String
	}
	if leaseExpiresAt.Valid {
		t.LeaseExpiresAt = &leaseExpiresAt.String
	}
	return t, nil
}
