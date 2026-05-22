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

CREATE TABLE IF NOT EXISTS agents (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL,
    runtime             TEXT NOT NULL,
    runtime_version     TEXT,
    domain              TEXT NOT NULL,
    workspace_id        TEXT REFERENCES workspaces(id),
    capabilities        TEXT,
    last_heartbeat_at   TEXT,
    status              TEXT NOT NULL DEFAULT 'active',
    registered_at       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agents_domain    ON agents(domain);
CREATE INDEX IF NOT EXISTS idx_agents_workspace ON agents(workspace_id);
CREATE INDEX IF NOT EXISTS idx_agents_status    ON agents(status);

CREATE TABLE IF NOT EXISTS task_types (
    id                           TEXT PRIMARY KEY,
    name                         TEXT NOT NULL UNIQUE,
    default_visibility_timeout_s INTEGER NOT NULL DEFAULT 300,
    max_attempts                 INTEGER NOT NULL DEFAULT 3,
    retry_backoff_s              INTEGER NOT NULL DEFAULT 60,
    hard_deadline_s              INTEGER,
    stale_heartbeat_threshold_s  INTEGER NOT NULL DEFAULT 60,
    created_at                   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
    id                TEXT PRIMARY KEY,
    workspace_id      TEXT REFERENCES workspaces(id),
    domain            TEXT,
    task_type_id      TEXT REFERENCES task_types(id),
    title             TEXT NOT NULL,
    priority          INTEGER NOT NULL DEFAULT 0,
    context           TEXT,
    status            TEXT NOT NULL DEFAULT 'queued',
    assigned_agent_id TEXT REFERENCES agents(id),
    lease_expires_at  TEXT,
    retry_after       TEXT,
    attempt_count     INTEGER NOT NULL DEFAULT 0,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_status       ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_workspace    ON tasks(workspace_id);
CREATE INDEX IF NOT EXISTS idx_tasks_domain       ON tasks(domain);
CREATE INDEX IF NOT EXISTS idx_tasks_task_type    ON tasks(task_type_id);
CREATE INDEX IF NOT EXISTS idx_tasks_agent        ON tasks(assigned_agent_id);
CREATE INDEX IF NOT EXISTS idx_tasks_priority     ON tasks(priority DESC, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_tasks_lease_expiry ON tasks(lease_expires_at) WHERE status = 'leased';

CREATE TABLE IF NOT EXISTS task_attempts (
    id            TEXT PRIMARY KEY,
    task_id       TEXT NOT NULL REFERENCES tasks(id),
    agent_id      TEXT REFERENCES agents(id),
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
    agent_id   TEXT REFERENCES agents(id),
    event_type TEXT NOT NULL,
    payload    TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_task ON task_events(task_id, created_at);

CREATE TABLE IF NOT EXISTS task_logs (
    id         TEXT PRIMARY KEY,
    task_id    TEXT NOT NULL REFERENCES tasks(id),
    attempt_id TEXT REFERENCES task_attempts(id),
    agent_id   TEXT REFERENCES agents(id),
    level      TEXT NOT NULL,
    message    TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_logs_task  ON task_logs(task_id, created_at);
CREATE INDEX IF NOT EXISTS idx_logs_agent ON task_logs(agent_id, created_at);
CREATE INDEX IF NOT EXISTS idx_logs_level ON task_logs(level);
