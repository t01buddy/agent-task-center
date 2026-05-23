package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// MetricsAgents counts agents by status bucket.
type MetricsAgents struct {
	Active  int `json:"active"`
	Stale   int `json:"stale"`
	Offline int `json:"offline"`
}

// MetricsTasks counts tasks by status.
type MetricsTasks struct {
	Queued    int `json:"queued"`
	Leased    int `json:"leased"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	TimedOut  int `json:"timed_out"`
	Cancelled int `json:"cancelled"`
}

// MetricsRates holds computed rate metrics.
type MetricsRates struct {
	RetryRate1h          float64 `json:"retry_rate_1h"`
	ThroughputPerMin10m  float64 `json:"throughput_per_min_10m"`
}

// DurationByType holds average task duration for one task type.
type DurationByType struct {
	TaskType string  `json:"task_type"`
	AvgS     float64 `json:"avg_s"`
}

// MetricsResponse is the full GET /api/metrics payload.
type MetricsResponse struct {
	Agents          MetricsAgents    `json:"agents"`
	Tasks           MetricsTasks     `json:"tasks"`
	Rates           MetricsRates     `json:"rates"`
	DurationsByType []DurationByType `json:"durations_by_type"`
}

// MetricsHandler returns an http.Handler for GET /api/metrics.
func MetricsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		now := time.Now().UTC()
		resp, err := collectMetrics(db, now)
		if err != nil {
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func collectMetrics(db *sql.DB, now time.Time) (*MetricsResponse, error) {
	agents, err := queryAgentCounts(db, now)
	if err != nil {
		return nil, err
	}

	tasks, err := queryTaskCounts(db)
	if err != nil {
		return nil, err
	}

	rates, err := queryRates(db, now)
	if err != nil {
		return nil, err
	}

	durations, err := queryDurations(db, now)
	if err != nil {
		return nil, err
	}

	return &MetricsResponse{
		Agents:          agents,
		Tasks:           tasks,
		Rates:           rates,
		DurationsByType: durations,
	}, nil
}

// queryAgentCounts buckets agents into active / stale / offline.
// Active   = last_heartbeat_at within stale threshold (default 60 s).
// Stale    = last_heartbeat_at outside threshold but within 24 h.
// Offline  = no heartbeat at all or older than 24 h.
func queryAgentCounts(db *sql.DB, now time.Time) (MetricsAgents, error) {
	staleThreshold := now.Add(-60 * time.Second).Format(time.RFC3339)
	offlineThreshold := now.Add(-24 * time.Hour).Format(time.RFC3339)

	rows, err := db.Query(`
		SELECT
			SUM(CASE WHEN last_heartbeat_at >= ? THEN 1 ELSE 0 END) AS active,
			SUM(CASE WHEN last_heartbeat_at < ? AND last_heartbeat_at >= ? THEN 1 ELSE 0 END) AS stale,
			SUM(CASE WHEN last_heartbeat_at IS NULL OR last_heartbeat_at < ? THEN 1 ELSE 0 END) AS offline
		FROM agents`,
		staleThreshold,
		staleThreshold, offlineThreshold,
		offlineThreshold,
	)
	if err != nil {
		return MetricsAgents{}, err
	}
	defer rows.Close()

	var a MetricsAgents
	if rows.Next() {
		var active, stale, offline sql.NullInt64
		if err := rows.Scan(&active, &stale, &offline); err != nil {
			return MetricsAgents{}, err
		}
		a.Active = int(active.Int64)
		a.Stale = int(stale.Int64)
		a.Offline = int(offline.Int64)
	}
	return a, rows.Err()
}

func queryTaskCounts(db *sql.DB) (MetricsTasks, error) {
	rows, err := db.Query(`
		SELECT status, COUNT(*) FROM tasks GROUP BY status`)
	if err != nil {
		return MetricsTasks{}, err
	}
	defer rows.Close()

	var t MetricsTasks
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return MetricsTasks{}, err
		}
		switch status {
		case "queued":
			t.Queued = count
		case "leased":
			t.Leased = count
		case "running":
			t.Running = count
		case "completed":
			t.Completed = count
		case "failed":
			t.Failed = count
		case "timed_out":
			t.TimedOut = count
		case "cancelled":
			t.Cancelled = count
		}
	}
	return t, rows.Err()
}

func queryRates(db *sql.DB, now time.Time) (MetricsRates, error) {
	oneHourAgo := now.Add(-time.Hour).Format(time.RFC3339)
	tenMinAgo := now.Add(-10 * time.Minute).Format(time.RFC3339)

	// Retry rate = retried attempts / total attempts in last 1 h.
	var totalAttempts, retryAttempts int
	row := db.QueryRow(`
		SELECT COUNT(*), SUM(CASE WHEN result_code = 'retry' THEN 1 ELSE 0 END)
		FROM task_attempts WHERE started_at >= ?`, oneHourAgo)
	var retryNull sql.NullInt64
	if err := row.Scan(&totalAttempts, &retryNull); err != nil {
		return MetricsRates{}, err
	}
	retryAttempts = int(retryNull.Int64)

	var retryRate float64
	if totalAttempts > 0 {
		retryRate = float64(retryAttempts) / float64(totalAttempts)
	}

	// Throughput = completed tasks in last 10 min / 10.
	var completed10m int
	row = db.QueryRow(`
		SELECT COUNT(*) FROM tasks WHERE status = 'completed' AND updated_at >= ?`, tenMinAgo)
	if err := row.Scan(&completed10m); err != nil {
		return MetricsRates{}, err
	}

	throughput := float64(completed10m) / 10.0

	return MetricsRates{
		RetryRate1h:         retryRate,
		ThroughputPerMin10m: throughput,
	}, nil
}

// queryDurations computes average task duration by task type for the last 1 h.
// Duration = ended_at - started_at for completed attempts.
func queryDurations(db *sql.DB, now time.Time) ([]DurationByType, error) {
	oneHourAgo := now.Add(-time.Hour).Format(time.RFC3339)

	rows, err := db.Query(`
		SELECT tt.name,
		       AVG(
		           (julianday(ta.ended_at) - julianday(ta.started_at)) * 86400.0
		       ) AS avg_s
		FROM task_attempts ta
		JOIN tasks t  ON t.id = ta.task_id
		JOIN task_types tt ON tt.id = t.task_type_id
		WHERE ta.ended_at IS NOT NULL
		  AND ta.result_code = 'completed'
		  AND ta.started_at >= ?
		GROUP BY tt.name
		ORDER BY tt.name`, oneHourAgo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []DurationByType
	for rows.Next() {
		var d DurationByType
		if err := rows.Scan(&d.TaskType, &d.AvgS); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	if result == nil {
		result = []DurationByType{}
	}
	return result, rows.Err()
}
