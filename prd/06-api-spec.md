# API Specification

Base path: `http://localhost:8765/api` (port configurable via `ATC_ADDR`).

All request and response bodies are JSON. All timestamps are ISO 8601 UTC strings. All IDs are UUIDs unless stated otherwise.

---

## Error Format

```json
{
  "error": "error_code",
  "message": "Human-readable description."
}
```

Common error codes: `not_found`, `conflict`, `stale_fencing_token`, `invalid_request`, `task_not_leasable`.

---

## Workspaces

### `POST /api/workspaces`

Create a workspace.

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

## Agents

### `POST /api/agents/register`

Register or update a worker.

**Request**
```json
{
  "agent_id": "my-codex-worker-1",
  "name": "Codex Worker 1",
  "runtime": "codex",
  "runtime_version": "1.2.0",
  "domain": "coding",
  "workspace_id": "uuid-or-null",
  "capabilities": ["go", "python"]
}
```

**Response `200`** (upsert — same shape as agent object).

---

### `POST /api/agents/{id}/heartbeat`

**Request** (empty body accepted)

**Response `200`**
```json
{ "agent_id": "my-codex-worker-1", "status": "active", "last_heartbeat_at": "..." }
```

---

### `GET /api/agents`

Query parameters: `workspace_id`, `domain`, `status`.

**Response `200`**
```json
{ "agents": [ { ...agent object... } ] }
```

---

## Task Types

### `POST /api/task-types`

**Request**
```json
{
  "name": "code-review",
  "default_visibility_timeout_s": 600,
  "max_attempts": 3,
  "retry_backoff_s": 120,
  "hard_deadline_s": 7200,
  "stale_heartbeat_threshold_s": 90
}
```

**Response `201`** (task type object).

---

### `GET /api/task-types`

**Response `200`**
```json
{ "task_types": [ { ...task type object... } ] }
```

---

## Tasks

### `POST /api/tasks`

**Request**
```json
{
  "title": "Review PR #42 for security issues",
  "workspace_id": "uuid",
  "domain": "review",
  "task_type_id": "uuid-or-null",
  "priority": 10,
  "context": {
    "instructions": "Review the diff for SQL injection and XSS vulnerabilities.",
    "input": { "pr_url": "https://github.com/org/repo/pull/42" },
    "expected_output": "A structured findings report.",
    "repo_path": "/home/user/projects/myapp",
    "refs": ["https://owasp.org/www-community/attacks/SQL_Injection"]
  }
}
```

**Response `201`** (task object, see below).

---

### Task Object

```json
{
  "id": "uuid",
  "workspace_id": "uuid",
  "domain": "review",
  "task_type_id": "uuid",
  "title": "Review PR #42 for security issues",
  "priority": 10,
  "context": { "..." : "..." },
  "status": "queued",
  "assigned_agent_id": null,
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
| `domain` | string | Filter by domain |
| `task_type_id` | string | Filter by task type |
| `status` | string | Filter by status (comma-separated for multiple) |
| `assigned_agent_id` | string | Filter by owner agent |
| `priority_gte` | integer | Minimum priority |
| `limit` | integer | Max results (default 50, max 200) |
| `offset` | integer | Pagination offset |

**Response `200`**
```json
{ "tasks": [ { ...task object... } ], "total": 142 }
```

---

### `PATCH /api/tasks/{id}`

Update `title`, `priority`, `context`, `domain`, or `workspace_id`. Only allowed when `status` is `queued` or `blocked`.

**Request** (partial update — only provided fields are changed)
```json
{ "priority": 20, "context": { "instructions": "Updated instructions." } }
```

**Response `200`** (updated task object).

---

### `DELETE /api/tasks/{id}`

Cancel a task. Only allowed when `status` is `queued` or `blocked`.

**Response `204`**

**Errors:** `409` if task is currently `leased`.

---

## Leasing

### `POST /api/tasks/lease`

Atomically claim the next eligible task.

**Request**
```json
{
  "agent_id": "my-codex-worker-1",
  "workspace_id": "uuid",
  "domain": "coding",
  "task_type_id": "uuid-or-null",
  "priority_gte": 0
}
```

**Response `200`** — task leased:
```json
{
  "task": { ...task object with status "leased"... },
  "fencing_token": 3,
  "lease_expires_at": "2026-05-22T10:10:00Z"
}
```

**Response `204`** — no eligible task available.

---

### `POST /api/tasks/{id}/heartbeat`

Extend lease and optionally report progress.

**Request**
```json
{
  "agent_id": "my-codex-worker-1",
  "fencing_token": 3,
  "progress": 45,
  "message": "Running security scanner."
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
  "agent_id": "my-codex-worker-1",
  "fencing_token": 3,
  "result": { "findings": 0, "report_url": "..." }
}
```

**Response `200`** (updated task object with `status: "completed"`).

**Errors:** `409` for stale fencing token.

---

### `POST /api/tasks/{id}/fail`

**Request**
```json
{
  "agent_id": "my-codex-worker-1",
  "fencing_token": 3,
  "reason": "Timeout waiting for external API.",
  "retry_hint": true
}
```

**Response `200`** (updated task object; `status` is `queued` if retrying, `failed` if exhausted).

**Errors:** `409` for stale fencing token.

---

## Logs

### `POST /api/logs`

Batch ingest log lines.

**Request**
```json
{
  "logs": [
    {
      "task_id": "uuid",
      "attempt_id": "uuid",
      "agent_id": "my-codex-worker-1",
      "level": "info",
      "message": "Starting security scan.",
      "timestamp": "2026-05-22T10:05:00Z"
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

Query parameters: `task_id`, `agent_id`, `level`, `since` (ISO timestamp), `until`, `limit` (default 100, max 1000), `offset`.

**Response `200`**
```json
{ "logs": [ { ...log object... } ], "total": 840 }
```

---

## Metrics

### `GET /api/metrics`

**Response `200`**
```json
{
  "agents": {
    "active": 4,
    "stale": 1,
    "offline": 0
  },
  "tasks": {
    "queued": 12,
    "leased": 3,
    "running": 3,
    "completed": 284,
    "failed": 7,
    "timed_out": 2,
    "cancelled": 1
  },
  "rates": {
    "retry_rate_1h": 0.03,
    "throughput_per_min_10m": 2.4
  },
  "durations_by_type": [
    { "task_type": "code-review", "avg_s": 142 }
  ]
}
```

---

## `context` JSON Conventions

The `context` field is an arbitrary JSON object. The server does not enforce a schema. The following keys are recommended for interoperability between workers and task creators:

| Key | Type | Purpose |
|-----|------|---------|
| `instructions` | string | Human/agent-readable task instruction |
| `input` | object | Domain-specific input payload |
| `expected_output` | string | Success criteria or output description |
| `repo_path` | string | Local filesystem path to a relevant repository |
| `refs` | array of strings | URLs or file paths for reference material |

Workers should tolerate missing keys; not all tasks will use all conventions.
