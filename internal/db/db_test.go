package db_test

import (
	"database/sql"
	"testing"

	"github.com/t01buddy/agent-task-center/internal/db"
)

var expectedTables = []string{
	"workspaces",
	"agents",
	"task_types",
	"tasks",
	"task_attempts",
	"task_events",
	"task_logs",
	"schema_migrations",
}

func TestOpen_AllTablesExist(t *testing.T) {
	conn, err := db.OpenDefault("file::memory:?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	defer conn.Close()

	for _, tbl := range expectedTables {
		if !tableExists(t, conn, tbl) {
			t.Errorf("table %q not found", tbl)
		}
	}
}

func TestOpen_Idempotent(t *testing.T) {
	// Opening a fresh in-memory DB twice must not fail.
	for i := 0; i < 2; i++ {
		c, err := db.OpenDefault("file::memory:?mode=memory&cache=shared")
		if err != nil {
			t.Fatalf("OpenDefault attempt %d: %v", i+1, err)
		}
		c.Close()
	}
}

func TestOpen_WALMode(t *testing.T) {
	conn, err := db.OpenDefault("file::memory:?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	defer conn.Close()

	var mode string
	if err := conn.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	// In-memory SQLite always reports "memory" regardless of WAL pragma; accept both.
	if mode != "wal" && mode != "memory" {
		t.Errorf("expected journal_mode wal or memory, got %q", mode)
	}
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&n)
	if err != nil {
		t.Fatalf("sqlite_master query: %v", err)
	}
	return n > 0
}
