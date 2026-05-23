package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

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
	RetryRate1h         float64 `json:"retry_rate_1h"`
	ThroughputPerMin10m float64 `json:"throughput_per_min_10m"`
}

// DurationByStep holds average task duration for one workflow step.
type DurationByStep struct {
	WorkflowName string  `json:"workflow_name"`
	Step         string  `json:"step"`
	AvgS         float64 `json:"avg_s"`
}

// MetricsResponse is the full GET /api/metrics payload.
type MetricsResponse struct {
	Tasks           MetricsTasks     `json:"tasks"`
	Rates           MetricsRates     `json:"rates"`
	DurationsByStep []DurationByStep `json:"durations_by_step"`
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
		Tasks:           tasks,
		Rates:           rates,
		DurationsByStep: durations,
	}, nil
}

func queryTaskCounts(db *sql.DB) (MetricsTasks, error) {
	rows, err := db.Query(`SELECT status, COUNT(*) FROM tasks GROUP BY status`)
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

	var completed10m int
	row = db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE status = 'completed' AND updated_at >= ?`, tenMinAgo)
	if err := row.Scan(&completed10m); err != nil {
		return MetricsRates{}, err
	}

	return MetricsRates{
		RetryRate1h:         retryRate,
		ThroughputPerMin10m: float64(completed10m) / 10.0,
	}, nil
}

// queryDurations computes average task duration by workflow + step for the last 1 h.
func queryDurations(db *sql.DB, now time.Time) ([]DurationByStep, error) {
	oneHourAgo := now.Add(-time.Hour).Format(time.RFC3339)

	rows, err := db.Query(`
		SELECT COALESCE(t.workflow_name, ''), COALESCE(t.step, ''),
		       AVG((julianday(ta.ended_at) - julianday(ta.started_at)) * 86400.0) AS avg_s
		FROM task_attempts ta
		JOIN tasks t ON t.id = ta.task_id
		WHERE ta.ended_at IS NOT NULL
		  AND ta.result_code = 'completed'
		  AND ta.started_at >= ?
		GROUP BY t.workflow_name, t.step
		ORDER BY t.workflow_name, t.step`, oneHourAgo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []DurationByStep
	for rows.Next() {
		var d DurationByStep
		if err := rows.Scan(&d.WorkflowName, &d.Step, &d.AvgS); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	if result == nil {
		result = []DurationByStep{}
	}
	return result, rows.Err()
}
