package dashboard

import (
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"time"
)

var metricsTemplates = template.Must(template.New("metrics").Funcs(template.FuncMap{
	"pct":  func(f float64) string { return fmt.Sprintf("%.1f%%", f*100) },
	"fmtf": func(f float64) string { return fmt.Sprintf("%.1f", f) },
}).Parse(`
{{define "metrics-body"}}
<div class="content">
  <div class="card">
    <h2>Agents</h2>
    <div class="metric-row"><span class="metric-label">Active</span><span class="metric-value">{{.Agents.Active}}</span></div>
    <div class="metric-row"><span class="metric-label">Stale</span><span class="metric-value{{if gt .Agents.Stale 0}} stale{{end}}">{{.Agents.Stale}}</span></div>
    <div class="metric-row"><span class="metric-label">Offline</span><span class="metric-value">{{.Agents.Offline}}</span></div>
  </div>
  <div class="card">
    <h2>Tasks by Status</h2>
    <div class="metric-row"><span class="metric-label">Queued</span><span class="metric-value">{{.Tasks.Queued}}</span></div>
    <div class="metric-row"><span class="metric-label">Leased</span><span class="metric-value">{{.Tasks.Leased}}</span></div>
    <div class="metric-row"><span class="metric-label">Running</span><span class="metric-value">{{.Tasks.Running}}</span></div>
    <div class="metric-row"><span class="metric-label">Completed</span><span class="metric-value">{{.Tasks.Completed}}</span></div>
    <div class="metric-row"><span class="metric-label">Failed</span><span class="metric-value">{{.Tasks.Failed}}</span></div>
    <div class="metric-row"><span class="metric-label">Timed out</span><span class="metric-value">{{.Tasks.TimedOut}}</span></div>
    <div class="metric-row"><span class="metric-label">Cancelled</span><span class="metric-value">{{.Tasks.Cancelled}}</span></div>
  </div>
  <div class="card card-full">
    <h2>Rates (last 1 h / 10 min)</h2>
    <div class="metric-row"><span class="metric-label">Retry rate</span><span class="metric-value">{{pct .Rates.RetryRate1h}}</span></div>
    <div class="metric-row"><span class="metric-label">Throughput</span><span class="metric-value">{{fmtf .Rates.ThroughputPerMin10m}} tasks/min</span></div>
  </div>
  <div class="card card-full">
    <h2>Duration by Task Type (avg, last 1 h)</h2>
    {{if .DurationsByType}}
    {{range .DurationsByType}}
    <div class="metric-row"><span class="metric-label">{{.TaskType}}</span><span class="metric-value">{{fmtf .AvgS}} s</span></div>
    {{end}}
    {{else}}
    <div class="empty">No completed tasks in the last hour.</div>
    {{end}}
  </div>
</div>
{{end}}

{{define "metrics-page"}}<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Metrics — Agent Task Center</title>
<script src="https://unpkg.com/htmx.org@1.9.12" defer></script>
<script src="https://unpkg.com/alpinejs@3.14.0/dist/cdn.min.js" defer></script>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:monospace;font-size:13px;background:#f5f5f5;color:#222;min-width:900px}
header{background:#1a1a1a;color:#fff;padding:10px 20px;display:flex;align-items:center;gap:20px}
header h1{font-size:15px;font-weight:bold}
nav a{color:#aaa;text-decoration:none;padding:4px 8px}
nav a:hover,nav a.active{color:#fff;background:#333;border-radius:3px}
.content{padding:20px;display:grid;grid-template-columns:1fr 1fr;gap:20px}
.card{background:#fff;border:1px solid #ddd;padding:16px}
.card h2{font-size:11px;text-transform:uppercase;color:#666;margin-bottom:12px;letter-spacing:0.05em}
.card-full{grid-column:1/-1}
.metric-row{display:flex;justify-content:space-between;padding:4px 0;border-bottom:1px solid #f0f0f0}
.metric-row:last-child{border-bottom:none}
.metric-label{color:#555}
.metric-value{font-weight:bold}
.stale{color:#d97706}
.empty{color:#999;font-style:italic;padding:8px 0}
</style>
</head>
<body>
<header>
  <h1>Agent Task Center</h1>
  <nav>
    <a href="/" class="active">Metrics</a>
    <a href="/tasks">Tasks</a>
    <a href="/logs">Logs</a>
  </nav>
</header>
<div id="metrics-region"
     hx-get="/api/metrics-partial"
     hx-trigger="every 5s"
     hx-swap="innerHTML">
  {{template "metrics-body" .}}
</div>
</body>
</html>
{{end}}
`))

// MetricsAgents mirrors api.MetricsAgents without import cycle.
type MetricsAgents struct {
	Active  int
	Stale   int
	Offline int
}

// MetricsTasks mirrors api.MetricsTasks without import cycle.
type MetricsTasks struct {
	Queued    int
	Leased    int
	Running   int
	Completed int
	Failed    int
	TimedOut  int
	Cancelled int
}

// MetricsRates mirrors api.MetricsRates without import cycle.
type MetricsRates struct {
	RetryRate1h         float64
	ThroughputPerMin10m float64
}

// MetricsDuration holds one task-type average.
type MetricsDuration struct {
	TaskType string
	AvgS     float64
}

// MetricsPageData is the template data for the Metrics view.
type MetricsPageData struct {
	Agents          MetricsAgents
	Tasks           MetricsTasks
	Rates           MetricsRates
	DurationsByType []MetricsDuration
}

func loadMetricsData(db *sql.DB) (*MetricsPageData, error) {
	now := time.Now().UTC()
	staleThreshold := now.Add(-60 * time.Second).Format(time.RFC3339)
	offlineThreshold := now.Add(-24 * time.Hour).Format(time.RFC3339)
	oneHourAgo := now.Add(-time.Hour).Format(time.RFC3339)
	tenMinAgo := now.Add(-10 * time.Minute).Format(time.RFC3339)

	var d MetricsPageData

	// Agent counts
	row := db.QueryRow(`
		SELECT
		  SUM(CASE WHEN last_heartbeat_at >= ? THEN 1 ELSE 0 END),
		  SUM(CASE WHEN last_heartbeat_at < ? AND last_heartbeat_at >= ? THEN 1 ELSE 0 END),
		  SUM(CASE WHEN last_heartbeat_at IS NULL OR last_heartbeat_at < ? THEN 1 ELSE 0 END)
		FROM agents`,
		staleThreshold, staleThreshold, offlineThreshold, offlineThreshold,
	)
	var active, stale, offline sql.NullInt64
	if err := row.Scan(&active, &stale, &offline); err != nil {
		return nil, err
	}
	d.Agents.Active = int(active.Int64)
	d.Agents.Stale = int(stale.Int64)
	d.Agents.Offline = int(offline.Int64)

	// Task counts
	rows, err := db.Query(`SELECT status, COUNT(*) FROM tasks GROUP BY status`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			rows.Close()
			return nil, err
		}
		switch status {
		case "queued":
			d.Tasks.Queued = count
		case "leased":
			d.Tasks.Leased = count
		case "running":
			d.Tasks.Running = count
		case "completed":
			d.Tasks.Completed = count
		case "failed":
			d.Tasks.Failed = count
		case "timed_out":
			d.Tasks.TimedOut = count
		case "cancelled":
			d.Tasks.Cancelled = count
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Retry rate (last 1h)
	var totalAttempts int
	var retryNull sql.NullInt64
	row = db.QueryRow(`
		SELECT COUNT(*), SUM(CASE WHEN result_code='retry' THEN 1 ELSE 0 END)
		FROM task_attempts WHERE started_at >= ?`, oneHourAgo)
	if err := row.Scan(&totalAttempts, &retryNull); err != nil {
		return nil, err
	}
	if totalAttempts > 0 {
		d.Rates.RetryRate1h = float64(retryNull.Int64) / float64(totalAttempts)
	}

	// Throughput (last 10min)
	var completed10m int
	row = db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE status='completed' AND updated_at >= ?`, tenMinAgo)
	if err := row.Scan(&completed10m); err != nil {
		return nil, err
	}
	d.Rates.ThroughputPerMin10m = float64(completed10m) / 10.0

	// Avg duration by task type (last 1h)
	rows, err = db.Query(`
		SELECT tt.name, AVG((julianday(ta.ended_at)-julianday(ta.started_at))*86400.0)
		FROM task_attempts ta
		JOIN tasks t ON t.id=ta.task_id
		JOIN task_types tt ON tt.id=t.task_type_id
		WHERE ta.ended_at IS NOT NULL AND ta.result_code='completed' AND ta.started_at >= ?
		GROUP BY tt.name ORDER BY tt.name`, oneHourAgo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry MetricsDuration
		if err := rows.Scan(&entry.TaskType, &entry.AvgS); err != nil {
			return nil, err
		}
		d.DurationsByType = append(d.DurationsByType, entry)
	}
	return &d, rows.Err()
}

// MetricsPageHandler serves GET / — the full Metrics page.
func MetricsPageHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := loadMetricsData(db)
		if err != nil {
			http.Error(w, "metrics error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := metricsTemplates.ExecuteTemplate(w, "metrics-page", data); err != nil {
			http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

// MetricsPartialHandler serves GET /api/metrics-partial — the HTMX-swappable region.
func MetricsPartialHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := loadMetricsData(db)
		if err != nil {
			http.Error(w, "metrics error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := metricsTemplates.ExecuteTemplate(w, "metrics-body", data); err != nil {
			http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		}
	}
}
