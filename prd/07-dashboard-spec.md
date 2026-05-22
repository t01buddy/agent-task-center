# Dashboard Specification

The dashboard is a read-oriented operations interface for human operators and supervising agents. Task creation and updates happen through the API. The dashboard provides visibility and filtering; it does not replace the API for data entry.

---

## Technical Approach

| Concern | Implementation |
|---------|---------------|
| Page rendering | Server-rendered Templ components |
| Incremental updates | HTMX polling (`hx-get`, `hx-trigger="every 5s"`) for Metrics; manual trigger for Task List and Logs |
| Local interactions | Alpine.js for drawer open/close, dropdown state, filter panel toggle |
| Styles | Plain CSS; functional, monochrome, operator-oriented aesthetic |
| No build step | Alpine.js and HTMX loaded from CDN; no Node.js or bundler required |

---

## Navigation

Three top-level views accessible from a persistent sidebar or tab bar:

1. **Metrics** — `/` (default)
2. **Tasks** — `/tasks`
3. **Logs** — `/logs`

---

## View 1 — Metrics

### Purpose

Real-time aggregate counters and rates that answer: "Is the system healthy? Are agents working?"

### Layout

```
┌─────────────────────────────────────────────────────────────┐
│  Agent Task Center              [Metrics] [Tasks] [Logs]    │
├───────────────────────┬─────────────────────────────────────┤
│  AGENTS               │  TASKS BY STATUS                    │
│  Active:    4         │  Queued:     12                     │
│  Stale:     1         │  Leased:      3                     │
│  Offline:   0         │  Completed: 284                     │
│                       │  Failed:      7                     │
│                       │  Timed out:   2                     │
│                       │  Cancelled:   1                     │
├───────────────────────┴─────────────────────────────────────┤
│  RATES (last 1 h / 10 min)                                  │
│  Retry rate:       3.0%                                     │
│  Throughput:       2.4 tasks/min                            │
├─────────────────────────────────────────────────────────────┤
│  DURATION BY TASK TYPE (avg, last 1 h)                      │
│  code-review   142 s                                        │
│  research       87 s                                        │
└─────────────────────────────────────────────────────────────┘
```

### Behaviour

- HTMX polls `GET /api/metrics` every **5 seconds** and swaps the metrics region without a full page reload.
- No user input required; view is entirely read-only.
- Stale agents are highlighted (e.g. amber text) to draw operator attention.

---

## View 2 — Task List

### Purpose

Browse, filter, and inspect tasks. Operators use this to identify stuck, failed, or high-priority work.

### Filter Bar

Rendered as an HTML form submitted via HTMX (`hx-get="/tasks" hx-push-url="true"`):

| Filter | Input type | Values |
|--------|-----------|--------|
| Workspace | `<select>` | All workspaces + "Any" |
| Domain | `<select>` | All domains seen + "Any" |
| Task type | `<select>` | All task types + "Any" |
| Status | `<select multiple>` | All statuses |
| Agent | `<input>` | Partial match on agent ID or name |
| Min priority | `<input type="number">` | Integer |

Filters persist in URL query parameters so the view is bookmarkable.

### Table

Columns (default sort: `priority DESC`, then `created_at ASC`):

| Column | Source field | Notes |
|--------|-------------|-------|
| Title | `tasks.title` | Truncated to 60 chars |
| Status | `tasks.status` | Coloured badge |
| Owner | `tasks.assigned_agent_id` | Short agent ID or name |
| Lease expires | `tasks.lease_expires_at` | Relative time ("in 4m") |
| Attempts | `tasks.attempt_count` | Number |
| Priority | `tasks.priority` | Number |
| Created | `tasks.created_at` | Relative time |

Pagination: **50 rows per page**. Previous/next links update via HTMX.

Clicking a row opens the **Task Detail Drawer** (FR-26) without leaving the page.

### Task Detail Drawer

Alpine.js-controlled slide-in panel (right side). Triggered by row click; closed by clicking outside or pressing Escape.

Content:

1. **Task header** — ID, title, status badge, created/updated timestamps.
2. **Metadata** — workspace, domain, task type, priority, assigned agent, lease expiry, attempt count.
3. **Context** — `context` JSON rendered in a `<pre>` code block with syntax highlighting.
4. **Event timeline** — ordered list of `task_events` rows: timestamp, event type, agent, payload summary.
5. **Recent logs** — last 50 `task_logs` rows for this task: timestamp, level badge, message.

Data for the drawer is fetched from `GET /api/tasks/{id}`, `GET /api/logs?task_id={id}&limit=50`, and the events are included in the task detail response (or a separate `GET /api/tasks/{id}/events` endpoint).

---

## View 3 — Logs

### Purpose

Query and browse task and agent log output. Used to diagnose failures.

### Filter Bar

| Filter | Input type |
|--------|-----------|
| Task ID | `<input>` |
| Agent ID | `<input>` |
| Level | `<select>` (debug / info / warn / error / all) |
| Since | `<input type="datetime-local">` |
| Until | `<input type="datetime-local">` |

Submitting re-fetches via `GET /api/logs` with updated params.

### Table

Columns:

| Column | Source field |
|--------|-------------|
| Timestamp | `task_logs.created_at` |
| Task ID | `task_logs.task_id` (truncated, links to task drawer) |
| Agent | `task_logs.agent_id` |
| Level | `task_logs.level` (coloured badge) |
| Message | `task_logs.message` |

Pagination: **100 rows per page**. No live stream in v1; user re-runs query to refresh.

---

## General UI Rules

1. **No inline task creation or editing.** The dashboard is read-only except for filters. All mutations go through the API.
2. **Status badges use consistent colours across all views:** `queued` = grey, `leased` = blue, `completed` = green, `failed` = red, `timed_out` = orange, `cancelled` = dim.
3. **Relative timestamps** (e.g. "3 min ago") for human readability; hover shows the absolute ISO timestamp.
4. **Mobile layout is not a priority for v1.** The dashboard is designed for laptop/desktop viewports (min-width 900 px).
5. **No JavaScript framework beyond Alpine.js.** No React, Vue, or similar.
6. **No authentication UI in v1.** Assumes trusted local access.
