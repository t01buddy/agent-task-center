# Functional Requirements

All requirements are for v1 unless labelled `[v2]`.

---

## FR Index

| FR | Area | Title | Priority |
|----|------|-------|----------|
| FR-01 | Workspace | Create workspace | Must |
| FR-02 | Workspace | List workspaces | Must |
| FR-03 | Agent | Register agent | Must |
| FR-04 | Agent | Agent heartbeat | Must |
| FR-05 | Agent | List agents | Must |
| FR-06 | Agent | Detect stale agents | Must |
| FR-07 | Task Type | Create task type | Must |
| FR-08 | Task Type | List task types | Must |
| FR-09 | Task | Create task | Must |
| FR-10 | Task | Query tasks | Must |
| FR-11 | Task | Update task metadata | Must |
| FR-12 | Task | Cancel task | Must |
| FR-13 | Lease | Atomic task lease | Must |
| FR-14 | Lease | Task heartbeat / lease extension | Must |
| FR-15 | Lease | Complete task | Must |
| FR-16 | Lease | Fail task | Must |
| FR-17 | Lease | Fencing token enforcement | Must |
| FR-18 | Retry | Automatic lease expiry and requeue | Must |
| FR-19 | Retry | Max attempts and timed-out transition | Must |
| FR-20 | Events | Append-only task events | Must |
| FR-21 | Logs | Ingest task logs | Must |
| FR-22 | Logs | Query task logs | Must |
| FR-23 | Dashboard | Metrics view | Must |
| FR-24 | Dashboard | Task list view | Must |
| FR-25 | Dashboard | Logs view | Must |
| FR-26 | Dashboard | Task detail drawer | Must |
| FR-27 | Examples | Claude Code worker example | Should |
| FR-28 | Examples | Codex worker example | Should |
| FR-29 | Examples | Generic shell worker example | Should |
| FR-30 | Config | Service configuration via env/flags | Must |

---

## Workspace

### FR-01 — Create Workspace

A client can create a named workspace. A workspace is a logical namespace for tasks and agents. `workspace_id` is a user-defined string label (e.g. `personal`, `research`, `team-alpha`). There is no repo-path binding in v1.

- `name` is required, must be non-empty, max 100 characters.
- Duplicate names are rejected with `409 Conflict`.

### FR-02 — List Workspaces

A client can retrieve all workspaces ordered by creation time.

---

## Agent

### FR-03 — Register Agent

A worker calls `POST /api/agents/register` to identify itself. Fields: `agent_id` (caller-provided, idempotent), `name`, `runtime`, `runtime_version`, `domain` (user-defined grouping label, e.g. `codex`, `claude-code`, `review`), `workspace_id` (optional), `capabilities` (optional JSON array of strings).

Repeated registration with the same `agent_id` updates fields rather than creating a duplicate row.

### FR-04 — Agent Heartbeat

A registered agent calls `POST /api/agents/{id}/heartbeat` periodically to signal it is alive. Updates `last_heartbeat_at`.

### FR-05 — List Agents

Returns all agents. Filterable by `workspace_id`, `domain`, `status`.

### FR-06 — Detect Stale Agents

An agent whose `last_heartbeat_at` is older than the configured stale threshold (default: 60 s, overridable per task type) is marked `status = stale`. Stale status is informational; it does not forcibly release leases (lease expiry handles that).

---

## Task Type

### FR-07 — Create Task Type

Defines default configuration for a category of tasks:
- `name` — unique identifier (e.g. `code-review`, `research`).
- `default_visibility_timeout_s` — default lease duration in seconds.
- `max_attempts` — maximum retry count before transitioning to `timed_out`.
- `retry_backoff_s` — seconds added between retry attempts.
- `hard_deadline_s` — maximum total wall-clock seconds from task creation; after this the task is cancelled regardless of attempts.
- `stale_heartbeat_threshold_s` — inactivity duration before a leased task's agent is considered stale.

### FR-08 — List Task Types

Returns all task types.

---

## Task

### FR-09 — Create Task

`POST /api/tasks` creates a task with:
- `title` — human/agent-readable name (required).
- `workspace_id` — optional; narrows visibility.
- `domain` — optional worker-group filter (e.g. `codex`, `review`, `any`).
- `task_type_id` — optional; inherits timeout/retry defaults from the task type.
- `priority` — integer (higher = more urgent; default 0).
- `context` — arbitrary JSON object. See `06-api-spec.md` for recommended keys.
- `status` defaults to `queued`.

### FR-10 — Query Tasks

`GET /api/tasks` with optional filters:
- `workspace_id`, `domain`, `task_type_id`, `status`, `assigned_agent_id`, `priority_gte`.
- Pagination: `limit` (default 50, max 200), `offset`.
- Ordered by `priority DESC, created_at ASC` by default.

### FR-11 — Update Task Metadata

`PATCH /api/tasks/{id}` allows updating `title`, `priority`, `context`, `domain`, `workspace_id` when the task is in `queued` or `blocked` state. Updates to leased/running tasks are rejected.

### FR-12 — Cancel Task

`DELETE /api/tasks/{id}` transitions the task to `cancelled`. Only allowed when status is `queued` or `blocked`. Returns `409` if the task is currently leased.

---

## Lease

### FR-13 — Atomic Task Lease

`POST /api/tasks/lease` body: `agent_id`, optional `workspace_id`, `domain`, `task_type_id`, `priority_gte`.

The server atomically selects the highest-priority `queued` task matching the filters, sets `status = leased`, `assigned_agent_id`, `lease_expires_at = now + visibility_timeout`, increments `attempt_count`, creates a `task_attempts` row with a new `fencing_token`, and returns the task with its `fencing_token`.

If no eligible task exists, returns `204 No Content`.

### FR-14 — Task Heartbeat / Lease Extension

`POST /api/tasks/{id}/heartbeat` body: `agent_id`, `fencing_token`, optional `progress` (0–100 integer), optional `message`.

Extends `lease_expires_at` by the task type's `default_visibility_timeout_s`. Appends a `heartbeat` event. Rejects stale fencing tokens with `409`.

### FR-15 — Complete Task

`POST /api/tasks/{id}/complete` body: `agent_id`, `fencing_token`, optional `result` (JSON object).

Validates fencing token. Sets `status = completed`, records result, closes the attempt row. Appends a `completed` event.

### FR-16 — Fail Task

`POST /api/tasks/{id}/fail` body: `agent_id`, `fencing_token`, `reason` (string), optional `retry_hint` (boolean).

Validates fencing token. If `attempt_count < max_attempts` and `retry_hint` is not false, sets `status = queued` for retry (respecting backoff). If attempts exhausted, sets `status = failed`. Appends a `failed` event.

### FR-17 — Fencing Token Enforcement

Any request to `complete`, `fail`, or `heartbeat` that supplies a fencing token not matching the current active attempt must return `409 Conflict` with body `{"error": "stale_fencing_token"}`. The task state is not modified.

---

## Retry and Timeout

### FR-18 — Automatic Lease Expiry and Requeue

A background process checks for tasks where `status = leased` and `lease_expires_at < now`. For each: if `attempt_count < max_attempts`, transition to `queued` (respects retry backoff by setting a `retry_after` timestamp). Append a `timed_out` event.

Check interval: configurable, default 10 s.

### FR-19 — Max Attempts and Timed-Out Transition

When a task's `attempt_count >= max_attempts` during expiry processing, transition to `timed_out` (terminal state). This is distinct from `failed` (agent-reported) and `cancelled` (human/API-initiated).

---

## Events

### FR-20 — Append-Only Task Events

All state transitions append a row to `task_events`. Event types:

`created`, `leased`, `heartbeat`, `progress`, `completed`, `failed`, `timed_out`, `retried`, `cancelled`.

Events are never deleted or updated. The events table is the audit log. Agents may also emit custom event types via the heartbeat or a dedicated event endpoint.

---

## Logs

### FR-21 — Ingest Task Logs

`POST /api/logs` (batch) body: array of `{ task_id, attempt_id (optional), agent_id (optional), level, message, timestamp }`.

`level` values: `debug`, `info`, `warn`, `error`.

Accepted synchronously; returns `201` after write.

### FR-22 — Query Task Logs

`GET /api/logs` with filters: `task_id`, `agent_id`, `level`, `since` (ISO timestamp), `until`, `limit` (default 100, max 1000), `offset`.

Ordered `created_at ASC` by default.

---

## Dashboard

### FR-23 — Metrics View

Displays aggregate counters and rates:
- Active agents (heartbeat within threshold).
- Task counts by status: `queued`, `leased`, `running`, `completed`, `failed`, `timed_out`, `cancelled`.
- Retry rate (retried events / total attempts, last 1 h).
- Average task duration by task type (last 1 h).
- Throughput: tasks completed per minute (last 10 min).
- Auto-refreshes via HTMX every 5 s.

### FR-24 — Task List View

Filterable table of tasks. Filters: `workspace_id`, `domain`, `task_type_id`, `status`, `assigned_agent_id`, `priority_gte`.

Columns: task ID (truncated), title, status, owner agent, lease expiry (relative), attempt count, priority, created at.

Clicking a row opens the task detail drawer (FR-26).

Pagination: 50 rows per page.

### FR-25 — Logs View

Paginated log table. Filters: `task_id`, `agent_id`, `level`, time range (since/until).

Columns: timestamp, task ID, agent ID, level, message.

No live stream in v1; user refreshes or re-queries.

### FR-26 — Task Detail Drawer

Slide-in panel (Alpine.js) showing full task data:
- Task metadata (all fields).
- `context` JSON rendered as formatted code block.
- Task event timeline (all events in order).
- Recent log lines for the task (last 50).

Accessible from the Task List view without full page navigation.

---

## Worker Examples

### FR-27 — Claude Code Worker Example

A shell script or `AGENTS.md` snippet that shows a Claude Code session how to:
1. Register as an agent.
2. Poll for tasks by `domain`.
3. Heartbeat during execution.
4. Report completion or failure.

### FR-28 — Codex Worker Example

Same pattern for a Codex CLI worker.

### FR-29 — Generic Shell Worker Example

A POSIX shell script (`worker.sh`) that demonstrates the polling loop with `curl` and `jq`. No agent-specific runtime assumptions.

---

## Configuration

### FR-30 — Service Configuration

Configurable via environment variables or a TOML config file (env takes precedence):

| Setting | Env var | Default |
|---------|---------|---------|
| Database path | `ATC_DB_PATH` | `./agent-task-center.db` |
| Listen address | `ATC_ADDR` | `:8765` |
| Lease expiry check interval | `ATC_EXPIRY_INTERVAL_S` | `10` |
| Graceful drain timeout | `ATC_DRAIN_TIMEOUT_S` | `5` |
| Log format | `ATC_LOG_FORMAT` | `json` (`text` for dev) |
