package queue_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/t01buddy/agent-task-center/internal/db"
	"github.com/t01buddy/agent-task-center/internal/queue"
)

func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := db.OpenDefault("file::memory:?mode=memory&cache=shared&_" + t.Name())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func insertFixtures(t *testing.T, conn *sql.DB) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := conn.Exec(`INSERT INTO workspaces(id,name,created_at) VALUES('ws1','default',?)`, now)
	if err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	_, err = conn.Exec(`INSERT INTO task_types(id,name,default_visibility_timeout_s,max_attempts,retry_backoff_s,created_at)
		VALUES('tt1','test-type',300,3,5,?)`, now)
	if err != nil {
		t.Fatalf("insert task_type: %v", err)
	}
}

// TestLeaseExpiry_Requeue: leased task with attempt_count < max_attempts is requeued.
func TestLeaseExpiry_Requeue(t *testing.T) {
	conn := setupDB(t)
	insertFixtures(t, conn)

	now := time.Now().UTC()
	pastExpiry := now.Add(-5 * time.Second).Format(time.RFC3339)
	created := now.Format(time.RFC3339)

	_, err := conn.Exec(`INSERT INTO tasks(id,workspace_id,task_type_id,title,status,attempt_count,lease_expires_at,created_at,updated_at)
		VALUES('task1','ws1','tt1','test task','leased',1,?,?,?)`,
		pastExpiry, created, created)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	// Run expiry with 1s interval, cancel after 2 ticks.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go queue.Expiry(ctx, conn, 1)
	<-ctx.Done()

	var status string
	var retryAfter sql.NullString
	err = conn.QueryRow(`SELECT status, retry_after FROM tasks WHERE id='task1'`).Scan(&status, &retryAfter)
	if err != nil {
		t.Fatalf("query task: %v", err)
	}
	if status != "queued" {
		t.Errorf("expected status=queued, got %q", status)
	}
	if !retryAfter.Valid || retryAfter.String == "" {
		t.Error("expected retry_after to be set")
	}

	// Check timed_out and retried events were appended.
	var timedOut, retried int
	conn.QueryRow(`SELECT COUNT(*) FROM task_events WHERE task_id='task1' AND event_type='timed_out'`).Scan(&timedOut)
	conn.QueryRow(`SELECT COUNT(*) FROM task_events WHERE task_id='task1' AND event_type='retried'`).Scan(&retried)
	if timedOut == 0 {
		t.Error("expected timed_out event")
	}
	if retried == 0 {
		t.Error("expected retried event")
	}
}

// TestLeaseExpiry_TerminalTimeout: task at max_attempts transitions to timed_out.
func TestLeaseExpiry_TerminalTimeout(t *testing.T) {
	conn := setupDB(t)
	insertFixtures(t, conn)

	now := time.Now().UTC()
	pastExpiry := now.Add(-5 * time.Second).Format(time.RFC3339)
	created := now.Format(time.RFC3339)

	// attempt_count=3 == max_attempts=3 → terminal
	_, err := conn.Exec(`INSERT INTO tasks(id,workspace_id,task_type_id,title,status,attempt_count,lease_expires_at,created_at,updated_at)
		VALUES('task2','ws1','tt1','test task 2','leased',3,?,?,?)`,
		pastExpiry, created, created)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go queue.Expiry(ctx, conn, 1)
	<-ctx.Done()

	var status string
	err = conn.QueryRow(`SELECT status FROM tasks WHERE id='task2'`).Scan(&status)
	if err != nil {
		t.Fatalf("query task: %v", err)
	}
	if status != "timed_out" {
		t.Errorf("expected status=timed_out, got %q", status)
	}
}

// TestStaleAgent: agent with old heartbeat is marked stale.
func TestStaleAgent(t *testing.T) {
	conn := setupDB(t)
	insertFixtures(t, conn)

	now := time.Now().UTC()
	staleHB := now.Add(-120 * time.Second).Format(time.RFC3339)
	created := now.Format(time.RFC3339)

	_, err := conn.Exec(`INSERT INTO agents(id,name,runtime,domain,status,last_heartbeat_at,registered_at)
		VALUES('agent1','test-agent','shell','test','active',?,?)`, staleHB, created)
	if err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go queue.Expiry(ctx, conn, 1)
	<-ctx.Done()

	var status string
	err = conn.QueryRow(`SELECT status FROM agents WHERE id='agent1'`).Scan(&status)
	if err != nil {
		t.Fatalf("query agent: %v", err)
	}
	if status != "stale" {
		t.Errorf("expected status=stale, got %q", status)
	}
}
