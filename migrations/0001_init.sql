-- Migration 0001: initial schema

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS workspaces (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS workflows (
    name                         TEXT PRIMARY KEY,
    definition                   TEXT NOT NULL,
    default_visibility_timeout_s INTEGER NOT NULL DEFAULT 300,
    default_max_attempts         INTEGER NOT NULL DEFAULT 3,
    default_retry_backoff_s      INTEGER NOT NULL DEFAULT 60,
    created_at                   TEXT NOT NULL,
    updated_at                   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
    id                   TEXT PRIMARY KEY,
    workspace_id         TEXT REFERENCES workspaces(id),
    workflow_name        TEXT REFERENCES workflows(name),
    step                 TEXT,
    run_id               TEXT,
    domain               TEXT,
    title                TEXT NOT NULL,
    priority             INTEGER NOT NULL DEFAULT 0,
    context              TEXT,
    context_hash         TEXT,
    visibility_timeout_s INTEGER NOT NULL DEFAULT 300,
    max_attempts         INTEGER NOT NULL DEFAULT 3,
    retry_backoff_s      INTEGER NOT NULL DEFAULT 60,
    hard_deadline_s      INTEGER,
    status               TEXT NOT NULL DEFAULT 'queued',
    assigned_worker_id   TEXT,
    lease_expires_at     TEXT,
    retry_after          TEXT,
    attempt_count        INTEGER NOT NULL DEFAULT 0,
    created_at           TEXT NOT NULL,
    updated_at           TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_status        ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_workspace     ON tasks(workspace_id);
CREATE INDEX IF NOT EXISTS idx_tasks_workflow      ON tasks(workflow_name);
CREATE INDEX IF NOT EXISTS idx_tasks_step          ON tasks(step);
CREATE INDEX IF NOT EXISTS idx_tasks_run           ON tasks(run_id);
CREATE INDEX IF NOT EXISTS idx_tasks_domain        ON tasks(domain);
CREATE INDEX IF NOT EXISTS idx_tasks_worker        ON tasks(assigned_worker_id);
CREATE INDEX IF NOT EXISTS idx_tasks_priority      ON tasks(priority DESC, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_tasks_lease_expiry  ON tasks(lease_expires_at) WHERE status = 'leased';

CREATE TABLE IF NOT EXISTS task_attempts (
    id            TEXT PRIMARY KEY,
    task_id       TEXT NOT NULL REFERENCES tasks(id),
    worker_id     TEXT,
    fencing_token INTEGER NOT NULL,
    started_at    TEXT NOT NULL,
    expires_at    TEXT NOT NULL,
    ended_at      TEXT,
    result_code   TEXT
);

CREATE INDEX IF NOT EXISTS idx_attempts_task ON task_attempts(task_id);

CREATE TABLE IF NOT EXISTS task_events (
    id         TEXT PRIMARY KEY,
    task_id    TEXT NOT NULL REFERENCES tasks(id),
    attempt_id TEXT REFERENCES task_attempts(id),
    worker_id  TEXT,
    event_type TEXT NOT NULL,
    payload    TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_task ON task_events(task_id, created_at);

CREATE TABLE IF NOT EXISTS task_logs (
    id         TEXT PRIMARY KEY,
    task_id    TEXT NOT NULL REFERENCES tasks(id),
    attempt_id TEXT REFERENCES task_attempts(id),
    worker_id  TEXT,
    level      TEXT NOT NULL,
    message    TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_logs_task   ON task_logs(task_id, created_at);
CREATE INDEX IF NOT EXISTS idx_logs_worker ON task_logs(worker_id, created_at);
CREATE INDEX IF NOT EXISTS idx_logs_level  ON task_logs(level);
