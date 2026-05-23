package queue

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"time"
)

// ExpiryConfig holds tuning parameters for the expiry loop.
type ExpiryConfig struct {
	IntervalS int // ATC_EXPIRY_INTERVAL_S tick
}

// RunExpiryLoop starts the background goroutine that expires leased tasks
// whose lease_expires_at has passed.
// It returns when ctx is cancelled. Call as a goroutine.
func RunExpiryLoop(ctx context.Context, db *sql.DB, cfg ExpiryConfig) {
	interval := time.Duration(cfg.IntervalS) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := runExpiry(db); err != nil {
				slog.Error("expiry loop error", "err", err)
			}
		}
	}
}

// runExpiry finds expired leased tasks and either requeues or permanently fails them.
func runExpiry(db *sql.DB) error {
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	// Read max_attempts and retry_backoff_s directly from task row (no JOIN needed).
	rows, err := db.Query(`
		SELECT id, attempt_count, max_attempts, retry_backoff_s
		FROM tasks
		WHERE status = 'leased' AND lease_expires_at < ?
	`, nowStr)
	if err != nil {
		return err
	}
	defer rows.Close()

	type expiredTask struct {
		id           string
		attemptCount int
		maxAttempts  int
		retryBackoff int
	}

	var expired []expiredTask
	for rows.Next() {
		var e expiredTask
		if err := rows.Scan(&e.id, &e.attemptCount, &e.maxAttempts, &e.retryBackoff); err != nil {
			return err
		}
		expired = append(expired, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, e := range expired {
		if err := processExpiredTask(db, e.id, e.attemptCount, e.maxAttempts, e.retryBackoff, now); err != nil {
			slog.Error("process expired task", "task_id", e.id, "err", err)
		}
	}
	return nil
}

// processExpiredTask handles a single expired-lease task.
func processExpiredTask(db *sql.DB, taskID string, attemptCount, maxAttempts, retryBackoffS int, now time.Time) error {
	nowStr := now.Format(time.RFC3339)

	if attemptCount < maxAttempts {
		retryAfter := now.Add(time.Duration(retryBackoffS) * time.Second).Format(time.RFC3339)

		_, err := db.Exec(`
			UPDATE tasks
			SET status = 'queued',
			    assigned_worker_id = NULL,
			    lease_expires_at = NULL,
			    retry_after = ?,
			    updated_at = ?
			WHERE id = ?
		`, retryAfter, nowStr, taskID)
		if err != nil {
			return err
		}

		if err := appendEvent(db, taskID, "", "", "timed_out", nil); err != nil {
			slog.Error("append timed_out event", "task_id", taskID, "err", err)
		}
		if err := appendEvent(db, taskID, "", "", "retried", nil); err != nil {
			slog.Error("append retried event", "task_id", taskID, "err", err)
		}

		slog.Info("expired lease requeued", "task_id", taskID, "attempt_count", attemptCount, "retry_after", retryAfter)
	} else {
		_, err := db.Exec(`
			UPDATE tasks
			SET status = 'timed_out',
			    assigned_worker_id = NULL,
			    lease_expires_at = NULL,
			    updated_at = ?
			WHERE id = ?
		`, nowStr, taskID)
		if err != nil {
			return err
		}

		if err := appendEvent(db, taskID, "", "", "timed_out", nil); err != nil {
			slog.Error("append timed_out terminal event", "task_id", taskID, "err", err)
		}

		slog.Info("expired lease terminal timed_out", "task_id", taskID, "attempt_count", attemptCount)
	}
	return nil
}

// appendEvent inserts an immutable event row. Mirrors api.AppendEvent to avoid circular imports.
func appendEvent(db interface {
	Exec(string, ...any) (sql.Result, error)
}, taskID, attemptID, workerID, eventType string, payload *string) error {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	now := time.Now().UTC().Format(time.RFC3339)

	var aID, wID, p any
	if attemptID != "" {
		aID = attemptID
	}
	if workerID != "" {
		wID = workerID
	}
	if payload != nil {
		p = *payload
	}

	_, err := db.Exec(
		`INSERT INTO task_events (id, task_id, attempt_id, worker_id, event_type, payload, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, taskID, aID, wID, eventType, p, now,
	)
	return err
}
