# API Specification

Base path: `http://localhost:8765/api` (port configurable via `ATC_ADDR`).

All request and response bodies are JSON. All timestamps are ISO 8601 UTC strings.

---

## Error Format

```json
{ "error": "error_code" }
```

Common error codes: `not_found`, `conflict`, `stale_fencing_token`, `invalid_request`.

---

## Workspaces

### `POST /api/workspaces`

**Request**
```json
{ "name": "personal" }
```

**Response `201`**
```json
{ "id": "uuid", "name": "personal", "created_at": "2026-05-22T10:00:00Z" }
```

**Errors:** `409` if name already exists.

---

### `GET /api/workspaces`

**Response `200`**
```json
{ "workspaces": [ { "id": "uuid", "name": "personal", "created_at": "..." } ] }
```

---

## Workflows

### `POST /api/workflows`

Create a named workflow with a natural-language definition.

**Request**
```json
{
  "name": "bug-fix",
  "definition": "A bug-fix workflow with steps: triage (understand and reproduce), implement (write the fix), review (code review), deploy (ship it).",
  "default_visibility_timeout_s": 300,
  "default_max_attempts": 3,
  "default_retry_backoff_s": 60
}
```

**Response `201`**
```json
{
  "workflow": {
    "name": "bug-fix",
    "definition": "...",
    "default_visibility_timeout_s": 300,
    "default_max_attempts": 3,
    "default_retry_backoff_s": 60,
    "created_at": "...",
    "updated_at": "..."
  }
}
```

**Errors:** `409` if name already exists.

---

### `GET /api/workflows`

**Response `200`**
```json
{ "workflows": [ { ...workflow object... } ] }
```

---

### `GET /api/workflows/{name}`

**Response `200`**
```json
{ "workflow": { ...workflow object... } }
```

**Errors:** `404` if not found.

---

### `PUT /api/workflows/{name}`

Update definition and/or defaults. Only provided fields change.

**Request**
```json
{ "definition": "Updated definition.", "default_max_attempts": 5 }
```

**Response `200`**
```json
{ "workflow": { ...updated workflow... } }
```

---

### `DELETE /api/workflows/{name}`

**Response `204`**

---

## Classification

### `POST /api/classify`

Classify an incoming ticket (GitHub issue, Jira, Linear, etc.) into a workflow step and queue it for workers.

**Request**
```json
{
  "title": "Fix auth bypass in login",
  "context": {
    "source": "github_issue",
    "url": "https://github.com/org/repo/issues/42",
    "description": "Users can log in without a password."
  },
  "run_id": "issue-42",
  "workflow_name": "bug-fix"
}
```

- `title` (required) — task title; used as lookup key for deduplication.
- `context` (optional) — arbitrary JSON with ticket details.
- `run_id` (optional) — groups tasks from the same external ticket.
- `workflow_name` (optional) — skip detection if workflow is already known.

**Response `201`** — new task created or existing task updated:
```json
{
  "task": { ...task object... },
  "classified": true,
  "reused_cache": false,
  "workflow_name": "bug-fix",
  "step": "triage",
  "reasoning": "No prior steps for run issue-42; triage is the entry point."
}
```

**Response `200`** — cache hit (same title + same context):
```json
{
  "task": { ...existing task... },
  "classified": false,
  "reused_cache": true
}
```

**Errors:** `422` if no workflows exist or the specified `workflow_name` is not found.

---

## Tasks

### `POST /api/tasks`

**Request**
```json
{
  "title": "Review PR #42 for security issues",
  "workspace_id": "uuid",
  "workflow_name": "bug-fix",
  "step": "review",
  "run_id": "issue-42",
  "domain": "review",
  "priority": 10,
  "context": {
    "pr_url": "https://github.com/org/repo/pull/42"
  },
  "visibility_timeout_s": 600,
  "max_attempts": 3,
  "retry_backoff_s": 60
}
```

**Response `201`** (task object).

---

### Task Object

```json
{
  "id": "hex-uuid",
  "workspace_id": "uuid",
  "workflow_name": "bug-fix",
  "step": "triage",
  "run_id": "issue-42",
  "domain": "coding",
  "title": "Fix auth bypass in login",
  "priority": 50,
  "context": { "...": "..." },
  "context_hash": "sha256hex",
  "visibility_timeout_s": 300,
  "max_attempts": 3,
  "retry_backoff_s": 60,
  "status": "queued",
  "assigned_worker_id": null,
  "lease_expires_at": null,
  "attempt_count": 0,
  "created_at": "...",
  "updated_at": "..."
}
```

---

### `GET /api/tasks`

Query parameters:

| Param | Type | Description |
|-------|------|-------------|
| `workspace_id` | string | Filter by workspace |
| `workflow_name` | string | Filter by workflow |
| `step` | string | Filter by step |
| `run_id` | string | Filter by run group |
| `domain` | string | Filter by domain |
| `status` | string | Filter by status (comma-separated) |
| `assigned_worker_id` | string | Filter by worker |
| `priority_gte` | integer | Minimum priority |
| `limit` | integer | Max results (default 50, max 200) |
| `offset` | integer | Pagination offset |

**Response `200`**
```json
{ "tasks": [ { ...task object... } ], "total": 142 }
```

---

### `PATCH /api/tasks/{id}`

Update `title`, `priority`, `context`, `domain`, `workspace_id`, `workflow_name`, or `step`. Only allowed when `status` is `queued` or `blocked`.

**Response `200`** (updated task object).

---

### `DELETE /api/tasks/{id}`

Cancel a task. Only allowed when status is `queued` or `blocked`.

**Response `204`**

**Errors:** `409` if currently leased.

---

## Leasing

### `POST /api/tasks/lease`

Atomically claim the next eligible task.

**Request**
```json
{
  "worker_id": "codex-worker-1",
  "workflow_name": "bug-fix",
  "step": "implement",
  "domain": "coding",
  "priority_gte": 0
}
```

`worker_id` is a free string — no prior registration required.

**Response `200`** — task leased:
```json
{
  "task": { ...task object with status "leased"... },
  "fencing_token": 3,
  "attempt_id": "hex-uuid",
  "lease_expires_at": "2026-05-22T10:10:00Z"
}
```

**Response `204`** — no eligible task available.

---

### `POST /api/tasks/{id}/heartbeat`

**Request**
```json
{
  "worker_id": "codex-worker-1",
  "fencing_token": 3,
  "progress": 45,
  "message": "Running tests."
}
```

**Response `200`**
```json
{ "lease_expires_at": "2026-05-22T10:15:00Z" }
```

**Errors:** `409` for stale fencing token.

---

### `POST /api/tasks/{id}/complete`

**Request**
```json
{
  "worker_id": "codex-worker-1",
  "fencing_token": 3,
  "result": { "pr_url": "https://github.com/org/repo/pull/43" }
}
```

**Response `200`**
```json
{ "task": { ...task object with status "completed"... } }
```

---

### `POST /api/tasks/{id}/fail`

**Request**
```json
{
  "worker_id": "codex-worker-1",
  "fencing_token": 3,
  "reason": "Tests failed.",
  "retry_hint": true
}
```

**Response `200`**
```json
{ "task": { ...task object... } }
```

---

### `GET /api/tasks/{id}/events`

**Response `200`**
```json
{
  "events": [
    {
      "id": "hex",
      "task_id": "hex",
      "attempt_id": "hex",
      "worker_id": "codex-worker-1",
      "event_type": "leased",
      "payload": null,
      "created_at": "..."
    }
  ]
}
```

---

## Logs

### `POST /api/logs`

Batch-ingest log lines.

**Request**
```json
{
  "logs": [
    {
      "task_id": "hex",
      "attempt_id": "hex",
      "worker_id": "codex-worker-1",
      "level": "info",
      "message": "Starting implementation.",
      "timestamp": "2026-05-22T10:01:00Z"
    }
  ]
}
```

**Response `201`**
```json
{ "ingested": 1 }
```

---

### `GET /api/logs`

| Param | Type | Description |
|-------|------|-------------|
| `task_id` | string | Filter by task |
| `worker_id` | string | Filter by worker |
| `level` | string | Filter by level |
| `since` | ISO timestamp | Lower bound |
| `until` | ISO timestamp | Upper bound |
| `limit` | integer | Max results (default 100, max 1000) |
| `offset` | integer | Pagination offset |

**Response `200`**
```json
{ "logs": [ { ...log entry... } ], "total": 500 }
```

---

## Metrics

### `GET /api/metrics`

**Response `200`**
```json
{
  "tasks": {
    "queued": 5, "leased": 2, "running": 0,
    "completed": 120, "failed": 3, "timed_out": 1, "cancelled": 0
  },
  "rates": {
    "retry_rate_1h": 0.05,
    "throughput_per_min_10m": 2.3
  },
  "durations_by_step": [
    { "workflow_name": "bug-fix", "step": "implement", "avg_s": 142.5 }
  ]
}
```
