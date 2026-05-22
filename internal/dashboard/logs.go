package dashboard

import (
	"database/sql"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const pageSize = 100

var logsTemplate = template.Must(template.New("logs").Funcs(template.FuncMap{
	"levelClass": func(level string) string {
		switch strings.ToLower(level) {
		case "error":
			return "badge badge-error"
		case "warn":
			return "badge badge-warn"
		case "info":
			return "badge badge-info"
		default:
			return "badge badge-debug"
		}
	},
	"truncate": func(s string, n int) string {
		if len(s) <= n {
			return s
		}
		return s[:n] + "…"
	},
	"reltime": func(s string) string {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return s
		}
		d := time.Since(t)
		switch {
		case d < time.Minute:
			return "just now"
		case d < time.Hour:
			return strconv.Itoa(int(d.Minutes())) + " min ago"
		case d < 24*time.Hour:
			return strconv.Itoa(int(d.Hours())) + " hr ago"
		default:
			return strconv.Itoa(int(d.Hours()/24)) + " days ago"
		}
	},
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Logs — Agent Task Center</title>
<script src="https://unpkg.com/htmx.org@1.9.12" defer></script>
<script src="https://unpkg.com/alpinejs@3.14.0/dist/cdn.min.js" defer></script>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:monospace;font-size:13px;background:#f5f5f5;color:#222;min-width:900px}
header{background:#1a1a1a;color:#fff;padding:10px 20px;display:flex;align-items:center;gap:20px}
header h1{font-size:15px;font-weight:bold}
nav a{color:#aaa;text-decoration:none;padding:4px 8px}
nav a:hover,nav a.active{color:#fff;background:#333;border-radius:3px}
.filter-bar{background:#fff;border-bottom:1px solid #ddd;padding:10px 20px;display:flex;gap:10px;flex-wrap:wrap;align-items:flex-end}
.filter-bar label{display:flex;flex-direction:column;gap:3px;font-size:11px;color:#555}
.filter-bar input,.filter-bar select{border:1px solid #ccc;padding:4px 6px;font-family:monospace;font-size:12px}
.filter-bar button{padding:5px 14px;background:#1a1a1a;color:#fff;border:none;cursor:pointer;font-family:monospace}
.filter-bar button:hover{background:#333}
.content{padding:20px}
table{width:100%;border-collapse:collapse;background:#fff;border:1px solid #ddd}
th{background:#f0f0f0;padding:6px 10px;text-align:left;font-size:11px;text-transform:uppercase;color:#666;border-bottom:1px solid #ddd}
td{padding:6px 10px;border-bottom:1px solid #eee;vertical-align:top}
tr:hover{background:#f9f9f9}
.badge{display:inline-block;padding:1px 6px;border-radius:3px;font-size:11px;font-weight:bold;text-transform:uppercase}
.badge-error{background:#fee2e2;color:#b91c1c}
.badge-warn{background:#fef3c7;color:#92400e}
.badge-info{background:#dbeafe;color:#1e40af}
.badge-debug{background:#f3f4f6;color:#6b7280}
.task-link{color:#1a1a1a;text-decoration:none;font-family:monospace}
.task-link:hover{text-decoration:underline}
.pagination{display:flex;gap:10px;margin-top:12px;align-items:center;justify-content:flex-end}
.pagination a,.pagination span{padding:4px 10px;border:1px solid #ccc;background:#fff;color:#222;text-decoration:none;font-family:monospace;font-size:12px}
.pagination a:hover{background:#f0f0f0}
.pagination span.disabled{color:#aaa}
.ts{color:#888;font-size:11px}
.empty{padding:40px;text-align:center;color:#999}
</style>
</head>
<body>
<header>
  <h1>Agent Task Center</h1>
  <nav>
    <a href="/">Metrics</a>
    <a href="/tasks">Tasks</a>
    <a href="/logs" class="active">Logs</a>
  </nav>
</header>

<form class="filter-bar"
      hx-get="/logs"
      hx-target="body"
      hx-push-url="true"
      hx-swap="outerHTML">
  <label>Task ID<input type="text" name="task_id" value="{{.Filters.TaskID}}" placeholder="task-…"></label>
  <label>Agent ID<input type="text" name="agent_id" value="{{.Filters.AgentID}}" placeholder="agent-…"></label>
  <label>Level
    <select name="level">
      {{range .LevelOptions}}
      <option value="{{.}}"{{if eq . $.Filters.Level}} selected{{end}}>{{.}}</option>
      {{end}}
    </select>
  </label>
  <label>Since<input type="datetime-local" name="since" value="{{.Filters.Since}}"></label>
  <label>Until<input type="datetime-local" name="until" value="{{.Filters.Until}}"></label>
  <button type="submit">Apply</button>
</form>

<div class="content">
  {{if .Logs}}
  <table>
    <thead>
      <tr>
        <th>Timestamp</th>
        <th>Task ID</th>
        <th>Agent</th>
        <th>Level</th>
        <th>Message</th>
      </tr>
    </thead>
    <tbody>
      {{range .Logs}}
      <tr>
        <td class="ts" title="{{.CreatedAt}}">{{reltime .CreatedAt}}</td>
        <td><a class="task-link" href="/tasks?id={{.TaskID}}">{{truncate .TaskID 12}}</a></td>
        <td>{{.AgentID}}</td>
        <td><span class="{{levelClass .Level}}">{{.Level}}</span></td>
        <td>{{.Message}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
  <div class="pagination">
    {{if gt .Page 1}}
    <a href="{{.PrevURL}}"
       hx-get="{{.PrevURL}}"
       hx-target="body"
       hx-push-url="true"
       hx-swap="outerHTML">← Prev</a>
    {{else}}
    <span class="disabled">← Prev</span>
    {{end}}
    <span>Page {{.Page}}</span>
    {{if .HasNext}}
    <a href="{{.NextURL}}"
       hx-get="{{.NextURL}}"
       hx-target="body"
       hx-push-url="true"
       hx-swap="outerHTML">Next →</a>
    {{else}}
    <span class="disabled">Next →</span>
    {{end}}
  </div>
  {{else}}
  <div class="empty">No logs found. Adjust filters or wait for agents to produce logs.</div>
  {{end}}
</div>
</body>
</html>
`))

// LogEntry is a single row from task_logs.
type LogEntry struct {
	ID        string
	TaskID    string
	AgentID   string
	Level     string
	Message   string
	CreatedAt string
}

// LogFilters holds the parsed query params for /logs.
type LogFilters struct {
	TaskID  string
	AgentID string
	Level   string
	Since   string
	Until   string
}

// LogsPageData is passed to the template.
type LogsPageData struct {
	Logs         []LogEntry
	Filters      LogFilters
	LevelOptions []string
	Page         int
	HasNext      bool
	PrevURL      string
	NextURL      string
}

// LogsHandler returns an http.Handler for GET /logs.
func LogsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		filters := LogFilters{
			TaskID:  strings.TrimSpace(q.Get("task_id")),
			AgentID: strings.TrimSpace(q.Get("agent_id")),
			Level:   q.Get("level"),
			Since:   q.Get("since"),
			Until:   q.Get("until"),
		}
		if filters.Level == "" {
			filters.Level = "all"
		}

		page := 1
		if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 1 {
			page = p
		}
		offset := (page - 1) * pageSize

		logs, err := queryLogs(db, filters, offset, pageSize+1)
		if err != nil {
			http.Error(w, "query error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		hasNext := len(logs) > pageSize
		if hasNext {
			logs = logs[:pageSize]
		}

		data := LogsPageData{
			Logs:         logs,
			Filters:      filters,
			LevelOptions: []string{"all", "debug", "info", "warn", "error"},
			Page:         page,
			HasNext:      hasNext,
			PrevURL:      pageURL(r, page-1),
			NextURL:      pageURL(r, page+1),
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := logsTemplate.Execute(w, data); err != nil {
			http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

func queryLogs(db *sql.DB, f LogFilters, offset, limit int) ([]LogEntry, error) {
	var conditions []string
	var args []any

	if f.TaskID != "" {
		conditions = append(conditions, "task_id LIKE ?")
		args = append(args, f.TaskID+"%")
	}
	if f.AgentID != "" {
		conditions = append(conditions, "agent_id LIKE ?")
		args = append(args, f.AgentID+"%")
	}
	if f.Level != "" && f.Level != "all" {
		conditions = append(conditions, "level = ?")
		args = append(args, strings.ToLower(f.Level))
	}
	if f.Since != "" {
		t, err := time.ParseInLocation("2006-01-02T15:04", f.Since, time.UTC)
		if err == nil {
			conditions = append(conditions, "created_at >= ?")
			args = append(args, t.Format(time.RFC3339))
		}
	}
	if f.Until != "" {
		t, err := time.ParseInLocation("2006-01-02T15:04", f.Until, time.UTC)
		if err == nil {
			conditions = append(conditions, "created_at <= ?")
			args = append(args, t.Format(time.RFC3339))
		}
	}

	query := "SELECT id, task_id, COALESCE(agent_id,''), level, message, created_at FROM task_logs"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		if err := rows.Scan(&e.ID, &e.TaskID, &e.AgentID, &e.Level, &e.Message, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// pageURL builds a URL preserving current query params but replacing page.
func pageURL(r *http.Request, page int) string {
	q := r.URL.Query()
	if page <= 1 {
		q.Del("page")
	} else {
		q.Set("page", strconv.Itoa(page))
	}
	u := *r.URL
	u.RawQuery = q.Encode()
	return u.String()
}
