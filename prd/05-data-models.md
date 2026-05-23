# Data Models

SQLite schema for Agent Task Center.

---

## workspaces

```sql
CREATE TABLE workspaces (
    id         TEXT PRIMARY KEY,           -- UUID
    name       TEXT NOT NULL UNIQUE,       -- user-defined label, e.g. "personal"
    created_at TEXT NOT NULL               -- ISO 8601 UTC
);
```

---

## workflows

Named processes with natural-language definitions. The LLM reads the definition to decide the next step.

```sql
CREATE TABLE workflows (
    name                         TEXT PRIMARY KEY,      -- "bug-fix", "feature-dev" — LLM uses this name
    definition                   TEXT NOT NULL,         -- natural language; LLM reads this
    default_visibility_timeout_s INTEGER NOT NULL DEFAULT 300,
    default_max_attempts         INTEGER NOT NULL DEFAULT 3,
    default_retry_backoff_s      INTEGER NOT NULL DEFAULT 60,
    created_at                   TEXT NOT NULL,
    updated_at                   TEXT NOT NULL
);
```

---

## tasks

A task is a workflow instance at a specific step. One task = one unit of work for one worker.

```sql
CREATE TABLE tasks (
    id                   TEXT PRIMARY KEY,
    workspace_id         TEXT REFERENCES workspaces(id),
    workflow_name        TEXT REFERENCES workflows(name),  -- human-readable key; LLM output
    step                 TEXT,                             -- current step ("triage", "implement", …)
    run_id               TEXT,                             -- groups tasks from the same external ticket/run
    domain               TEXT,                             -- optional worker domain filter
    title                TEXT NOT NULL,                    -- idempotency key for /classify
    priority             INTEGER NOT NULL DEFAULT 0,
    context              TEXT,                             -- JSON object (ticket content)
    context_hash         TEXT,                             -- SHA-256(title+context) for classify dedup
    visibility_timeout_s INTEGER NOT NULL DEFAULT 300,
    max_attempts         INTEGER NOT NULL DEFAULT 3,
    retry_backoff_s      INTEGER NOT NULL DEFAULT 60,
    hard_deadline_s      INTEGER,
    status               TEXT NOT NULL DEFAULT 'queued',
                         -- queued | leased | completed | failed | timed_out | cancelled
    assigned_worker_id   TEXT,                             -- free string; no FK (no agents table)
    lease_expires_at     TEXT,
    retry_after          TEXT,
    attempt_count        INTEGER NOT NULL DEFAULT 0,
    created_at           TEXT NOT NULL,
    updated_at           TEXT NOT NULL
);

CREATE INDEX idx_tasks_status        ON tasks(status);
CREATE INDEX idx_tasks_workspace     ON tasks(workspace_id);
CREATE INDEX idx_tasks_workflow      ON tasks(workflow_name);
CREATE INDEX idx_tasks_step          ON tasks(step);
CREATE INDEX idx_tasks_run           ON tasks(run_id);
CREATE INDEX idx_tasks_domain        ON tasks(domain);
CREATE INDEX idx_tasks_worker        ON tasks(assigned_worker_id);
CREATE INDEX idx_tasks_priority      ON tasks(priority DESC, created_at ASC);
CREATE INDEX idx_tasks_lease_expiry  ON tasks(lease_expires_at) WHERE status = 'leased';
```

**Status state machine:**

```
queued → leased → completed
       ↗ (retry)  → failed        (worker-reported)
                   → timed_out    (attempts exhausted)
queued → cancelled
```

---

## task_attempts

One row per lease/attempt.

```sql
CREATE TABLE task_attempts (
    id            TEXT PRIMARY KEY,
    task_id       TEXT NOT NULL REFERENCES tasks(id),
    worker_id     TEXT,                    -- free string; matches assigned_worker_id
    fencing_token INTEGER NOT NULL,        -- monotonically increasing per task
    started_at    TEXT NOT NULL,
    expires_at    TEXT NOT NULL,
    ended_at      TEXT,
    result_code   TEXT                     -- completed | failed | timed_out | superseded
);

CREATE INDEX idx_attempts_task ON task_attempts(task_id);
```

`fencing_token` is `MAX(fencing_token) + 1` for the task at lease time (starts at 1).

---

## task_events

Append-only audit log. Never updated or deleted.

```sql
CREATE TABLE task_events (
    id         TEXT PRIMARY KEY,
    task_id    TEXT NOT NULL REFERENCES tasks(id),
    attempt_id TEXT REFERENCES task_attempts(id),
    worker_id  TEXT,
    event_type TEXT NOT NULL,
               -- created | classified | reclassified | leased | heartbeat | progress |
               -- completed | failed | timed_out | retried | cancelled
    payload    TEXT,                  -- JSON; event-type-specific data
    created_at TEXT NOT NULL
);

CREATE INDEX idx_events_task ON task_events(task_id, created_at);
```

---

## task_logs

Structured log lines attached to a task/attempt/worker.

```sql
CREATE TABLE task_logs (
    id         TEXT PRIMARY KEY,
    task_id    TEXT NOT NULL REFERENCES tasks(id),
    attempt_id TEXT REFERENCES task_attempts(id),
    worker_id  TEXT,
    level      TEXT NOT NULL,         -- debug | info | warn | error
    message    TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_logs_task   ON task_logs(task_id, created_at);
CREATE INDEX idx_logs_worker ON task_logs(worker_id, created_at);
CREATE INDEX idx_logs_level  ON task_logs(level);
```

---

## Migration Strategy

- Migrations are numbered SQL files embedded in the binary (`migrations/0001_init.sql`, etc.).
- Applied in order on startup; idempotent (tracked in a `schema_migrations` table).
- No external migration tool required.
