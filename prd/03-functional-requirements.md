# Functional Requirements

All requirements are for v1 unless labelled `[v2]`.

---

## FR Index

| FR | Area | Title | Priority |
|----|------|-------|----------|
| FR-01 | Workspace | Create workspace | Must |
| FR-02 | Workspace | List workspaces | Must |
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
| FR-31 | Workflow | Create workflow definition | Must |
| FR-32 | Workflow | CRUD workflows by name | Must |
| FR-33 | Classify | AI-powered ticket classification | Must |
| FR-34 | Classify | Context-hash deduplication | Must |
| FR-35 | Classify | run_id grouping | Should |

---

## Workspace

### FR-01 — Create Workspace

A client can create a named workspace. A workspace is a logical namespace for tasks. `workspace_id` is a user-defined string label (e.g. `personal`, `research`, `team-alpha`).

- `name` is required, must be non-empty, max 100 characters.
- Duplicate names are rejected with `409 Conflict`.

### FR-02 — List Workspaces

A client can retrieve all workspaces ordered by creation time.

---

## Workflow

### FR-31 — Create Workflow Definition

`POST /api/workflows` creates a named workflow:
- `name` — primary key (e.g. `bug-fix`, `feature-dev`). LLM uses this name for routing.
- `definition` — free-form natural language describing the workflow and its steps. The LLM reads this to decide the next step.
- `default_visibility_timeout_s` (default: 300).
- `default_max_attempts` (default: 3).
- `default_retry_backoff_s` (default: 60).

Duplicate names return `409 Conflict`.

### FR-32 — Workflow CRUD

- `GET /api/workflows` — list all workflows.
- `GET /api/workflows/{name}` — get single workflow.
- `PUT /api/workflows/{name}` — update definition and/or defaults.
- `DELETE /api/workflows/{name}` — delete.

---

## Classification

### FR-33 — AI-Powered Ticket Classification

`POST /api/classify` accepts:
- `title` (required) — task title; used as lookup key.
- `context` (optional) — arbitrary JSON object with ticket details.
- `run_id` (optional) — caller-provided group ID linking tasks from the same run.
- `workflow_name` (optional) — skip workflow detection if already known.

**Logic:**
1. Compute `context_hash = SHA-256(title + contextJSON)`.
2. Look up existing task by `title`.
3. If found and `context_hash` unchanged → return existing task (`reused_cache: true`).
4. Determine `workflow_name`: from request → from existing task → LLM detects from all definitions.
5. Call LLM with workflow definition + current task state → outputs `step`, `domain`, `priority`, `reasoning`.
6. If existing task: `UPDATE` task, re-queue with new step.
7. If new task: `INSERT` task with operational config inherited from workflow.
8. Return `{task, classified: true, workflow_name, step, reasoning}`.

Returns `201` for new tasks, `200` for cache hits.

### FR-34 — Context-Hash Deduplication

If `POST /api/classify` is called with the same `title` and identical `context`, the existing task is returned without calling the LLM (`reused_cache: true`, `classified: false`).

### FR-35 — run_id Grouping

`run_id` is an optional caller-provided string that groups all tasks belonging to the same external ticket or automation run. The LLM can see the current task state (step + status) when `run_id` is provided, enabling it to advance the step.

---

## Task

### FR-09 — Create Task

`POST /api/tasks` creates a task with:
- `title` — human/agent-readable name (required).
- `workspace_id` — optional; narrows visibility.
- `workflow_name` — optional; associates the task with a named workflow.
- `step` — optional; the workflow step this task represents (e.g. `triage`, `implement`).
- `run_id` — optional; groups tasks from the same external ticket or run.
- `domain` — optional worker-group filter.
- `priority` — integer (higher = more urgent; default 0).
- `context` — arbitrary JSON object.
- `visibility_timeout_s`, `max_attempts`, `retry_backoff_s` — override workflow defaults.
- `status` defaults to `queued`.

### FR-10 — Query Tasks

`GET /api/tasks` with optional filters:
- `workspace_id`, `workflow_name`, `step`, `run_id`, `domain`, `status`, `assigned_worker_id`, `priority_gte`.
- Pagination: `limit` (default 50, max 200), `offset`.
- Ordered by `priority DESC, created_at ASC` by default.

### FR-11 — Update Task Metadata

`PATCH /api/tasks/{id}` allows updating `title`, `priority`, `context`, `domain`, `workspace_id`, `workflow_name`, `step` when the task is in `queued` or `blocked` state.

### FR-12 — Cancel Task

`DELETE /api/tasks/{id}` transitions the task to `cancelled`. Only allowed when status is `queued` or `blocked`. Returns `409` if the task is currently leased.

---

## Lease

### FR-13 — Atomic Task Lease

`POST /api/tasks/lease` body: `worker_id` (free string, no registration required), optional `workspace_id`, `workflow_name`, `step`, `domain`, `priority_gte`.

The server atomically selects the highest-priority `queued` task matching the filters, sets `status = leased`, `assigned_worker_id`, `lease_expires_at = now + visibility_timeout_s`, increments `attempt_count`, creates a `task_attempts` row with a new `fencing_token`, and returns the task with its `fencing_token`.

If no eligible task exists, returns `204 No Content`.

### FR-14 — Task Heartbeat / Lease Extension

`POST /api/tasks/{id}/heartbeat` body: `worker_id`, `fencing_token`, optional `progress` (0–100), optional `message`.

Extends `lease_expires_at`. Appends a `heartbeat` event. Rejects stale fencing tokens with `409`.

### FR-15 — Complete Task

`POST /api/tasks/{id}/complete` body: `worker_id`, `fencing_token`, optional `result` (JSON object).

Validates fencing token. Sets `status = completed`, closes the attempt row. Appends a `completed` event.

### FR-16 — Fail Task

`POST /api/tasks/{id}/fail` body: `worker_id`, `fencing_token`, `reason` (string), optional `retry_hint` (boolean).

Validates fencing token. If `attempt_count < max_attempts` and `retry_hint` is not false, requeues with backoff. Otherwise sets `status = failed`. Appends a `failed` event.

### FR-17 — Fencing Token Enforcement

Any request to `complete`, `fail`, or `heartbeat` that supplies a fencing token not matching the current active attempt must return `409 Conflict` with `{"error": "stale_fencing_token"}`.

---

## Retry and Timeout

### FR-18 — Automatic Lease Expiry and Requeue

A background process checks for tasks where `status = leased` and `lease_expires_at < now`. For each: if `attempt_count < max_attempts` (read from task row), transition to `queued` respecting `retry_backoff_s`. Append a `timed_out` event.

Check interval: configurable, default 10 s.

### FR-19 — Max Attempts and Timed-Out Transition

When `attempt_count >= max_attempts` during expiry, transition to `timed_out` (terminal). Distinct from `failed` (worker-reported) and `cancelled` (API-initiated).

---

## Events

### FR-20 — Append-Only Task Events

All state transitions append a row to `task_events`. Event types:

`created`, `classified`, `reclassified`, `leased`, `heartbeat`, `progress`, `completed`, `failed`, `timed_out`, `retried`, `cancelled`.

Events are never deleted or updated.

---

## Logs

### FR-21 — Ingest Task Logs

`POST /api/logs` (batch) body: array of `{ task_id, attempt_id (optional), worker_id (optional), level, message, timestamp }`.

`level` values: `debug`, `info`, `warn`, `error`.

Returns `201` after write.

### FR-22 — Query Task Logs

`GET /api/logs` with filters: `task_id`, `worker_id`, `level`, `since`, `until`, `limit` (default 100, max 1000), `offset`.

Ordered `created_at ASC` by default.

---

## Dashboard

### FR-23 — Metrics View

Displays aggregate counters and rates:
- Task counts by status: `queued`, `leased`, `completed`, `failed`, `timed_out`, `cancelled`.
- Retry rate (last 1 h).
- Average task duration by workflow / step (last 1 h).
- Throughput: tasks completed per minute (last 10 min).
- Auto-refreshes via HTMX every 5 s.

### FR-24 — Task List View

Filterable table of tasks. Filters: `workspace_id`, `workflow_name`, `step`, `domain`, `status`, `worker_id`, `priority_gte`.

Columns: title, status, worker, lease expiry (relative), attempt count, priority, created at.

Clicking a row opens the task detail drawer (FR-26).

### FR-25 — Logs View

Paginated log table. Filters: `task_id`, `worker_id`, `level`, time range.

Columns: timestamp, task ID, worker ID, level, message.

### FR-26 — Task Detail Drawer

Slide-in panel showing full task data including workflow/step, context JSON, event timeline, and recent log lines.

---

## Worker Examples

### FR-27 — Claude Code Worker Example

A shell script showing a Claude Code session how to poll for tasks by `workflow_name` + `step`, heartbeat, and report completion.

### FR-28 — Codex Worker Example

Same pattern for a Codex CLI worker.

### FR-29 — Generic Shell Worker Example

A POSIX shell script (`worker.sh`) using `curl` and `jq`.

---

## Configuration

### FR-30 — Service Configuration

Configurable via environment variables:

| Setting | Env var | Default |
|---------|---------|---------|
| Database path | `ATC_DB_PATH` | `./agent-task-center.db` |
| Listen address | `ATC_ADDR` | `:8765` |
| Lease expiry check interval | `ATC_EXPIRY_INTERVAL_S` | `10` |
| Graceful drain timeout | `ATC_DRAIN_TIMEOUT_S` | `5` |
| Log format | `ATC_LOG_FORMAT` | `json` |
| LLM provider | `ATC_LLM_PROVIDER` | `openai` |
| LLM base URL | `ATC_LLM_BASE_URL` | `https://api.openai.com/v1` |
| LLM API key | `ATC_LLM_API_KEY` | — |
| LLM model | `ATC_LLM_MODEL` | `gpt-4o-mini` |
| Codex model override | `ATC_CODEX_MODEL` | — |
