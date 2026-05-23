package queue_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/t01buddy/agent-task-center/internal/db"
	"github.com/t01buddy/agent-task-center/internal/queue"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := db.OpenDefault("file::memory:?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	if _, err := conn.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("disable FK: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// insertLeasedTask inserts a task with status='leased' and an expired lease.
func insertLeasedTask(t *testing.T, conn *sql.DB, taskID string, attemptCount int, expiredAt time.Time) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	exp := expiredAt.Format(time.RFC3339)
	_, err := conn.Exec(`
		INSERT INTO tasks (id, title, status, attempt_count, lease_expires_at, created_at, updated_at)
		VALUES (?, ?, 'leased', ?, ?, ?, ?)
	`, taskID, "test task", attemptCount, exp, now, now)
	if err != nil {
		t.Fatalf("insert task %s: %v", taskID, err)
	}
}

// taskStatus reads the current status of a task.
func taskStatus(t *testing.T, conn *sql.DB, taskID string) string {
	t.Helper()
	var status string
	if err := conn.QueryRow(`SELECT status FROM tasks WHERE id=?`, taskID).Scan(&status); err != nil {
		t.Fatalf("read task %s status: %v", taskID, err)
	}
	return status
}

// eventCount returns number of events of a given type for a task.
func eventCount(t *testing.T, conn *sql.DB, taskID, eventType string) int {
	t.Helper()
	var n int
	conn.QueryRow(`SELECT COUNT(*) FROM task_events WHERE task_id=? AND event_type=?`, taskID, eventType).Scan(&n)
	return n
}

func TestExpiry_RequeuesWhenAttemptsRemaining(t *testing.T) {
	conn := openTestDB(t)
	past := time.Now().UTC().Add(-5 * time.Second)
	insertLeasedTask(t, conn, "task-retry", 1, past) // attempt_count=1, max_attempts default=3

	cfg := queue.ExpiryConfig{IntervalS: 1}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go queue.RunExpiryLoop(ctx, conn, cfg)
	time.Sleep(2 * time.Second)

	status := taskStatus(t, conn, "task-retry")
	if status != "queued" {
		t.Errorf("expected status=queued after expiry, got %q", status)
	}
	if n := eventCount(t, conn, "task-retry", "timed_out"); n < 1 {
		t.Errorf("expected timed_out event, got %d", n)
	}
	if n := eventCount(t, conn, "task-retry", "retried"); n < 1 {
		t.Errorf("expected retried event, got %d", n)
	}
}

func TestExpiry_TerminalWhenMaxAttemptsReached(t *testing.T) {
	conn := openTestDB(t)
	past := time.Now().UTC().Add(-5 * time.Second)
	// attempt_count=3 >= max_attempts default=3 → terminal timed_out
	insertLeasedTask(t, conn, "task-terminal", 3, past)

	cfg := queue.ExpiryConfig{IntervalS: 1}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go queue.RunExpiryLoop(ctx, conn, cfg)
	time.Sleep(2 * time.Second)

	status := taskStatus(t, conn, "task-terminal")
	if status != "timed_out" {
		t.Errorf("expected status=timed_out after max attempts, got %q", status)
	}
	if n := eventCount(t, conn, "task-terminal", "timed_out"); n < 1 {
		t.Errorf("expected timed_out event, got %d", n)
	}
}

func TestExpiry_DoesNotAffectNonExpiredLeases(t *testing.T) {
	conn := openTestDB(t)
	future := time.Now().UTC().Add(60 * time.Second)
	insertLeasedTask(t, conn, "task-active", 1, future)

	cfg := queue.ExpiryConfig{IntervalS: 1}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go queue.RunExpiryLoop(ctx, conn, cfg)
	time.Sleep(1500 * time.Millisecond)

	status := taskStatus(t, conn, "task-active")
	if status != "leased" {
		t.Errorf("expected non-expired task to remain leased, got %q", status)
	}
}

func TestExpiry_RetryAfterSetOnRequeue(t *testing.T) {
	conn := openTestDB(t)
	past := time.Now().UTC().Add(-5 * time.Second)
	insertLeasedTask(t, conn, "task-backoff", 0, past) // first attempt

	cfg := queue.ExpiryConfig{IntervalS: 1}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go queue.RunExpiryLoop(ctx, conn, cfg)
	time.Sleep(2 * time.Second)

	var retryAfter sql.NullString
	conn.QueryRow(`SELECT retry_after FROM tasks WHERE id='task-backoff'`).Scan(&retryAfter)
	if !retryAfter.Valid || retryAfter.String == "" {
		t.Errorf("expected retry_after to be set after requeue")
	}
}

