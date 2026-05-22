// Package queue manages the task queue and expiry logic.
package queue

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// Expiry runs the lease-expiry and stale-agent detection loop.
// It ticks every intervalS seconds until ctx is cancelled.
func Expiry(ctx context.Context, db *sql.DB, intervalS int) {
	ticker := time.NewTicker(time.Duration(intervalS) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := runExpiry(db); err != nil {
				slog.Error("expiry run failed", "err", err)
			}
		}
	}
}

// runExpiry finds expired leases and either requeues or terminates tasks.
// It also marks agents whose heartbeat is overdue as stale.
func runExpiry(db *sql.DB) error {
	now := time.Now().UTC().Format(time.RFC3339)

	rows, err := db.Query(`
		SELECT t.id, t.attempt_count, tt.max_attempts, tt.retry_backoff_s
		FROM tasks t
		LEFT JOIN task_types tt ON t.task_type_id = tt.id
		WHERE t.status = 'leased' AND t.lease_expires_at < ?`, now)
	if err != nil {
		return fmt.Errorf("query expired leases: %w", err)
	}
	defer rows.Close()

	type expired struct {
		id            string
		attemptCount  int
		maxAttempts   int
		retryBackoffS int
	}
	var tasks []expired
	for rows.Next() {
		var e expired
		var maxAttempts sql.NullInt64
		var retryBackoffS sql.NullInt64
		if err := rows.Scan(&e.id, &e.attemptCount, &maxAttempts, &retryBackoffS); err != nil {
			return fmt.Errorf("scan expired: %w", err)
		}
		if maxAttempts.Valid {
			e.maxAttempts = int(maxAttempts.Int64)
		} else {
			e.maxAttempts = 3 // default
		}
		if retryBackoffS.Valid {
			e.retryBackoffS = int(retryBackoffS.Int64)
		} else {
			e.retryBackoffS = 60 // default
		}
		tasks = append(tasks, e)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows error: %w", err)
	}

	for _, t := range tasks {
		if err := expireTask(db, t.id, t.attemptCount, t.maxAttempts, t.retryBackoffS, now); err != nil {
			slog.Error("expire task", "task_id", t.id, "err", err)
		}
	}

	// Mark stale agents: status='active' with last_heartbeat_at overdue.
	// Uses stale_heartbeat_threshold_s from task_types (default 60s).
	// Since agents are global we use a fixed 60s threshold here.
	staleThreshold := time.Now().UTC().Add(-60 * time.Second).Format(time.RFC3339)
	if _, err := db.Exec(`
		UPDATE agents SET status = 'stale'
		WHERE status = 'active' AND last_heartbeat_at < ?`, staleThreshold); err != nil {
		slog.Error("mark stale agents", "err", err)
	}

	return nil
}

func expireTask(db *sql.DB, taskID string, attemptCount, maxAttempts, retryBackoffS int, now string) error {
	eventID := fmt.Sprintf("evt-%s-%d", taskID, time.Now().UnixNano())

	if attemptCount < maxAttempts {
		// Requeue with retry_after backoff.
		retryAfter := time.Now().UTC().Add(time.Duration(retryBackoffS) * time.Second).Format(time.RFC3339)
		_, err := db.Exec(`
			UPDATE tasks SET status='queued', assigned_agent_id=NULL,
			  retry_after=?, updated_at=?
			WHERE id=?`, retryAfter, now, taskID)
		if err != nil {
			return fmt.Errorf("requeue task %s: %w", taskID, err)
		}
		// Append timed_out event.
		if _, err := db.Exec(`
			INSERT INTO task_events(id,task_id,event_type,payload,created_at)
			VALUES(?,?,'timed_out','{}',?)`, eventID, taskID, now); err != nil {
			slog.Warn("insert timed_out event", "task_id", taskID, "err", err)
		}
		// Append retried event.
		retriedID := fmt.Sprintf("evt-%s-%d-r", taskID, time.Now().UnixNano())
		if _, err := db.Exec(`
			INSERT INTO task_events(id,task_id,event_type,payload,created_at)
			VALUES(?,?,'retried','{}',?)`, retriedID, taskID, now); err != nil {
			slog.Warn("insert retried event", "task_id", taskID, "err", err)
		}
		slog.Info("task requeued after lease expiry", "task_id", taskID, "retry_after", retryAfter)
	} else {
		// Terminal: timed_out status.
		_, err := db.Exec(`
			UPDATE tasks SET status='timed_out', assigned_agent_id=NULL, updated_at=?
			WHERE id=?`, now, taskID)
		if err != nil {
			return fmt.Errorf("terminate task %s: %w", taskID, err)
		}
		if _, err := db.Exec(`
			INSERT INTO task_events(id,task_id,event_type,payload,created_at)
			VALUES(?,?,'timed_out','{}',?)`, eventID, taskID, now); err != nil {
			slog.Warn("insert timed_out event", "task_id", taskID, "err", err)
		}
		slog.Info("task permanently timed out", "task_id", taskID, "attempts", attemptCount)
	}
	return nil
}
