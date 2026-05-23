// Package dashboard serves the Alpine.js dashboard UI.
package dashboard

import (
	"database/sql"
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	dbpkg "github.com/t01buddy/agent-task-center/internal/db"
	"github.com/t01buddy/agent-task-center/internal/model"
)

// Handler returns an http.Handler for the dashboard routes.
func Handler(db *sql.DB) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/tasks", taskListHandler(db))
	mux.HandleFunc("/tasks/detail", taskDetailHandler(db))
	return mux
}

// ---- helpers ----

var allStatuses = []string{"queued", "leased", "completed", "failed", "timed_out", "cancelled"}

func relTime(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	d := time.Since(t)
	if d < 0 {
		d = -d
		switch {
		case d < time.Minute:
			return "in <1m"
		case d < time.Hour:
			return "in " + strconv.Itoa(int(d.Minutes())) + "m"
		default:
			return "in " + strconv.Itoa(int(d.Hours())) + "h"
		}
	}
	switch {
	case d < time.Minute:
		return "<1m ago"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d ago"
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func prettyJSON(s *string) string {
	if s == nil {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(*s), &v); err != nil {
		return *s
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return *s
	}
	return string(b)
}

var funcMap = template.FuncMap{
	"relTime":    relTime,
	"truncate":   truncate,
	"derefStr":   derefStr,
	"prettyJSON": prettyJSON,
	"add": func(a, b int) int { return a + b },
}

// ---- task list ----

var taskListTmpl = template.Must(template.New("tasks").Funcs(funcMap).Parse(taskListHTML))

func taskListHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f := model.TaskFilter{}

		q := r.URL.Query()
		f.Workspace = q.Get("workspace")
		f.Domain = q.Get("domain")
		f.WorkflowName = q.Get("workflow_name")
		f.Step = q.Get("step")
		f.WorkerID = q.Get("worker_id")
		if ss := q["status"]; len(ss) > 0 {
			for _, s := range ss {
				if s != "" {
					f.Statuses = append(f.Statuses, s)
				}
			}
		}
		if mp := q.Get("min_priority"); mp != "" {
			if n, err := strconv.Atoi(mp); err == nil {
				f.MinPriority = &n
			}
		}
		if p := q.Get("page"); p != "" {
			if n, err := strconv.Atoi(p); err == nil {
				f.Page = n
			}
		}

		page, err := dbpkg.ListTasks(db, f)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = taskListTmpl.ExecuteTemplate(w, "tasks", map[string]any{
			"Page":       page,
			"AllStatuses": allStatuses,
			"Query":      q,
		})
	}
}

// ---- task detail ----

var taskDetailTmpl = template.Must(template.New("detail").Funcs(funcMap).Parse(taskDetailHTML))

func taskDetailHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}
		detail, err := dbpkg.GetTaskDetail(db, id)
		if err != nil {
			if strings.Contains(err.Error(), "not_found") {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = taskDetailTmpl.ExecuteTemplate(w, "detail", detail)
	}
}

// ---- HTML templates ----

const taskListHTML = `
{{define "tasks"}}<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Agent Task Center — Tasks</title>
<script src="https://unpkg.com/htmx.org@1.9.12" defer></script>
<script src="https://unpkg.com/alpinejs@3.x.x/dist/cdn.min.js" defer></script>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:monospace;font-size:13px;background:#111;color:#ddd;min-width:900px}
nav{background:#1a1a1a;border-bottom:1px solid #333;padding:8px 16px;display:flex;align-items:center;gap:16px}
nav strong{color:#fff;font-size:15px}
nav a{color:#aaa;text-decoration:none}nav a:hover,nav a.active{color:#fff}
.main{padding:16px}
form{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:12px;background:#1a1a1a;padding:10px;border:1px solid #333;border-radius:4px}
form label{font-size:11px;color:#888}
form select,form input{background:#222;border:1px solid #444;color:#ddd;padding:4px 6px;border-radius:3px;font-size:12px;font-family:monospace}
form button{background:#333;border:1px solid #555;color:#ddd;padding:4px 10px;border-radius:3px;cursor:pointer}
form button:hover{background:#444}
table{width:100%;border-collapse:collapse}
th{background:#1a1a1a;color:#aaa;font-weight:normal;text-align:left;padding:6px 8px;border-bottom:1px solid #333;font-size:11px;text-transform:uppercase}
td{padding:6px 8px;border-bottom:1px solid #1e1e1e;white-space:nowrap;max-width:300px;overflow:hidden;text-overflow:ellipsis}
tr:hover td{background:#1a1a1a;cursor:pointer}
.badge{display:inline-block;padding:1px 6px;border-radius:3px;font-size:11px;font-weight:bold}
.badge-queued{background:#444;color:#ccc}
.badge-leased{background:#1a3a6b;color:#6af}
.badge-completed{background:#1a4a2a;color:#6f6}
.badge-failed{background:#4a1a1a;color:#f66}
.badge-timed_out{background:#4a2e10;color:#fa6}
.badge-cancelled{background:#2a2a2a;color:#888}
.pagination{display:flex;gap:8px;margin-top:12px;align-items:center}
.pagination a{color:#aaa;text-decoration:none;padding:3px 8px;border:1px solid #333;border-radius:3px}
.pagination a:hover{color:#fff;border-color:#666}
.pagination span{color:#666;font-size:11px}
/* drawer */
.drawer-overlay{position:fixed;inset:0;background:rgba(0,0,0,.5);z-index:100;display:none}
.drawer-overlay.open{display:block}
.drawer{position:fixed;top:0;right:0;bottom:0;width:520px;background:#161616;border-left:1px solid #333;overflow-y:auto;z-index:101;padding:16px;transform:translateX(100%);transition:transform .2s}
.drawer.open{transform:translateX(0)}
.drawer h2{font-size:14px;color:#fff;margin-bottom:12px;word-break:break-all}
.drawer h3{font-size:11px;color:#888;text-transform:uppercase;margin:14px 0 6px}
.drawer pre{background:#111;padding:10px;border-radius:3px;overflow-x:auto;font-size:11px;color:#aaa;white-space:pre-wrap;word-break:break-all}
.meta-grid{display:grid;grid-template-columns:120px 1fr;gap:4px 8px;font-size:12px}
.meta-grid .label{color:#666}
.event-list{list-style:none}
.event-list li{padding:4px 0;border-bottom:1px solid #1e1e1e;font-size:11px;color:#aaa}
.event-list li strong{color:#ccc}
.log-line{padding:3px 0;border-bottom:1px solid #1a1a1a;font-size:11px;display:flex;gap:8px}
.log-line .ts{color:#555;flex-shrink:0}
.log-line .msg{color:#bbb;word-break:break-all}
.drawer-close{float:right;background:none;border:none;color:#888;font-size:18px;cursor:pointer;line-height:1}
.drawer-close:hover{color:#fff}
.loading{color:#666;padding:20px;text-align:center}
</style>
</head>
<body x-data="{drawerOpen:false,drawerHTML:'',drawerLoading:false}"
      @keydown.escape.window="drawerOpen=false">

<nav>
  <strong>Agent Task Center</strong>
  <a href="/">Metrics</a>
  <a href="/tasks" class="active">Tasks</a>
  <a href="/logs">Logs</a>
</nav>

<div class="main">
<form hx-get="/tasks" hx-push-url="true" hx-target="body" hx-swap="outerHTML">
  <div>
    <label>Workspace</label><br>
    <select name="workspace">
      <option value="">Any</option>
      {{range .Page.Workspaces}}<option value="{{.}}" {{if eq . $.Page.Filter.Workspace}}selected{{end}}>{{.}}</option>{{end}}
    </select>
  </div>
  <div>
    <label>Domain</label><br>
    <select name="domain">
      <option value="">Any</option>
      {{range .Page.Domains}}<option value="{{.}}" {{if eq . $.Page.Filter.Domain}}selected{{end}}>{{.}}</option>{{end}}
    </select>
  </div>
  <div>
    <label>Workflow</label><br>
    <select name="workflow_name">
      <option value="">Any</option>
      {{range .Page.Workflows}}<option value="{{.}}" {{if eq . $.Page.Filter.WorkflowName}}selected{{end}}>{{.}}</option>{{end}}
    </select>
  </div>
  <div>
    <label>Step</label><br>
    <select name="step">
      <option value="">Any</option>
      {{range .Page.Steps}}<option value="{{.}}" {{if eq . $.Page.Filter.Step}}selected{{end}}>{{.}}</option>{{end}}
    </select>
  </div>
  <div>
    <label>Status</label><br>
    <select name="status" multiple size="3">
      {{range $.AllStatuses}}<option value="{{.}}" {{range $.Page.Filter.Statuses}}{{if eq . $}}selected{{end}}{{end}}>{{.}}</option>{{end}}
    </select>
  </div>
  <div>
    <label>Worker</label><br>
    <input name="worker_id" value="{{.Page.Filter.WorkerID}}" placeholder="partial match">
  </div>
  <div>
    <label>Min Priority</label><br>
    <input type="number" name="min_priority" value="{{if .Page.Filter.MinPriority}}{{.Page.Filter.MinPriority}}{{end}}" style="width:70px">
  </div>
  <div style="align-self:flex-end">
    <button type="submit">Filter</button>
  </div>
</form>

<table>
<thead>
<tr>
  <th>Title</th>
  <th>Status</th>
  <th>Owner</th>
  <th>Lease Expires</th>
  <th>Attempts</th>
  <th>Priority</th>
  <th>Created</th>
</tr>
</thead>
<tbody>
{{range .Page.Tasks}}
<tr @click="drawerLoading=true; drawerOpen=true; drawerHTML=''; fetch('/tasks/detail?id={{.ID}}').then(r=>r.text()).then(h=>{drawerHTML=h;drawerLoading=false})">
  <td title="{{.Title}}">{{truncate .Title 60}}</td>
  <td><span class="badge badge-{{.Status}}">{{.Status}}</span></td>
  <td>{{derefStr .AssignedWorkerID}}</td>
  <td>{{if .LeaseExpiresAt}}{{relTime (derefStr .LeaseExpiresAt)}}{{else}}—{{end}}</td>
  <td>{{.AttemptCount}}</td>
  <td>{{.Priority}}</td>
  <td title="{{.CreatedAt}}">{{relTime .CreatedAt}}</td>
</tr>
{{else}}
<tr><td colspan="7" style="color:#555;padding:20px;text-align:center">No tasks match the current filter.</td></tr>
{{end}}
</tbody>
</table>

{{with .Page}}
<div class="pagination">
  {{if gt .Page 1}}
    <a hx-get="/tasks?page={{add .Page -1}}" hx-push-url="true" hx-target="body" hx-swap="outerHTML" href="#">← Prev</a>
  {{end}}
  <span>Page {{.Page}} of {{.TotalPages}} ({{.Total}} tasks)</span>
  {{if lt .Page .TotalPages}}
    <a hx-get="/tasks?page={{add .Page 1}}" hx-push-url="true" hx-target="body" hx-swap="outerHTML" href="#">Next →</a>
  {{end}}
</div>
{{end}}
</div>

<!-- drawer overlay -->
<div class="drawer-overlay" :class="{open:drawerOpen}" @click="drawerOpen=false"></div>
<div class="drawer" :class="{open:drawerOpen}">
  <template x-if="drawerLoading"><div class="loading">Loading…</div></template>
  <div x-html="drawerHTML"></div>
</div>

</body>
</html>
{{end}}
`

const taskDetailHTML = `
{{define "detail"}}
<button class="drawer-close" @click="drawerOpen=false">✕</button>
<h2>{{.Task.Title}}</h2>
<span class="badge badge-{{.Task.Status}}">{{.Task.Status}}</span>

<h3>Metadata</h3>
<div class="meta-grid">
  <span class="label">ID</span><span>{{.Task.ID}}</span>
  <span class="label">Workspace</span><span>{{derefStr .Task.WorkspaceName}}</span>
  <span class="label">Domain</span><span>{{derefStr .Task.Domain}}</span>
  <span class="label">Workflow</span><span>{{derefStr .Task.WorkflowName}}</span>
  <span class="label">Step</span><span>{{derefStr .Task.Step}}</span>
  <span class="label">Priority</span><span>{{.Task.Priority}}</span>
  <span class="label">Worker</span><span>{{derefStr .Task.AssignedWorkerID}}</span>
  <span class="label">Lease Expires</span><span>{{if .Task.LeaseExpiresAt}}{{relTime (derefStr .Task.LeaseExpiresAt)}}{{else}}—{{end}}</span>
  <span class="label">Attempts</span><span>{{.Task.AttemptCount}}</span>
  <span class="label">Created</span><span title="{{.Task.CreatedAt}}">{{relTime .Task.CreatedAt}}</span>
  <span class="label">Updated</span><span title="{{.Task.UpdatedAt}}">{{relTime .Task.UpdatedAt}}</span>
</div>

<h3>Context</h3>
<pre>{{prettyJSON .Task.Context}}</pre>

<h3>Event Timeline ({{len .Events}})</h3>
<ul class="event-list">
{{range .Events}}
<li>
  <span style="color:#555">{{relTime .CreatedAt}}</span>
  <strong>{{.EventType}}</strong>
  {{if .WorkerID}}<span style="color:#777"> worker={{derefStr .WorkerID}}</span>{{end}}
  {{if .Payload}}<span style="color:#666"> {{derefStr .Payload}}</span>{{end}}
</li>
{{else}}<li style="color:#555">No events.</li>
{{end}}
</ul>

<h3>Recent Logs (last 50)</h3>
{{range .Logs}}
<div class="log-line">
  <span class="ts" title="{{.CreatedAt}}">{{relTime .CreatedAt}}</span>
  <span class="badge badge-{{.Level}}" style="flex-shrink:0">{{.Level}}</span>
  <span class="msg">{{.Message}}</span>
</div>
{{else}}<div style="color:#555;padding:8px 0">No logs.</div>
{{end}}
{{end}}
`
