package db

import (
	"database/sql"

	"github.com/t01buddy/agent-task-center/migrations"
)

// OpenDefault opens the database at path and applies the embedded migrations.
// This is the primary entry point for production code.
func OpenDefault(path string) (*sql.DB, error) {
	return Open(path, migrations.FS)
}
