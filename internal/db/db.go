// Package db opens a SQLite database and runs schema migrations.
package db

import (
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Open opens (or creates) the SQLite database at path, enables WAL mode, and
// applies any pending migrations from the provided FS. Pass nil to skip
// migrations. Use "file::memory:?mode=memory&cache=shared" for in-memory
// databases.
func Open(path string, migrationsFS fs.FS) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("db open: %w", err)
	}

	if _, err = db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if _, err = db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if migrationsFS != nil {
		if err = runMigrations(db, migrationsFS); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrations: %w", err)
		}
	}
	return db, nil
}

// runMigrations creates the schema_migrations tracking table if needed, then
// applies each *.sql file in the provided FS that has not yet been recorded.
func runMigrations(db *sql.DB, migrationsFS fs.FS) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, ".")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		version := e.Name()

		var count int
		if err = db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&count); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if count > 0 {
			continue
		}

		sqlBytes, err := fs.ReadFile(migrationsFS, version)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}

		if _, err = db.Exec(string(sqlBytes)); err != nil {
			return fmt.Errorf("apply migration %s: %w", version, err)
		}

		if _, err = db.Exec(
			`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`,
			version, time.Now().UTC().Format(time.RFC3339),
		); err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}
	}
	return nil
}
