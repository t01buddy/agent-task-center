# Roadmap

---

## v1 — Local MVP

**Goal:** An AI-powered local task center where workers claim work, the LLM routes tickets to the right workflow step, and everything is auditable.

### Must-have

- Single binary, SQLite backend, WAL mode.
- Full REST API: workspaces, workflows, classify, tasks, leasing, heartbeat, completion, failure, logs, metrics.
- AI classification: `POST /api/classify` with OpenAI-compatible API and Codex CLI provider support.
- Context-hash deduplication in classify endpoint.
- Atomic leasing with fencing tokens; no agent registration required (worker_id is a free string).
- Automatic lease expiry and requeue (config read from task row, no task_types JOIN).
- Append-only task events and log ingestion.
- Dashboard: Metrics view, Task List view, Logs view (server-rendered, HTMX refresh).
- Worker examples: Claude Code worker, Codex worker, generic shell worker.
- `README.md` with quick-start instructions and one-command local demo.

### Out of scope for v1

Everything listed in `01-prd.md` § Out of Scope. Notably: full DAG engine, agent/worker registration, task_types table.

### Distribution

- Open-source GitHub repository.
- `go install` one-liner and pre-built binaries for macOS, Linux, and Windows.
- Blog post demonstrating a parallel agent workflow (research → implement → review).
- Example `AGENTS.md` and Codex prompt snippets for worker configuration.

---

## v2 — Stability and Visibility

**Goal:** Make Agent Task Center reliable enough that small teams adopt it as their durable agent ops layer.

### Candidates

- SSE stream (`/api/stream`) for live dashboard updates without polling.
- Policy files: per-workspace or per-task-type rules (rate limits, domain restrictions).
- Long-term log retention with configurable compaction.
- Agent capability matching: lease only tasks whose `required_capabilities` are met by the requesting agent.
- Push/webhook delivery for agents that cannot poll.
- Basic authentication (API key) for non-localhost deployments.
- Prometheus metrics endpoint (`/metrics`).
- Structured worker SDK in Go and Python.

---

## v3 — Team and Remote Features

**Goal:** Support small teams with remote access and cross-machine coordination.

### Candidates

- Optional hosted control plane for remote access and notifications.
- Cross-machine worker coordination (agent registers with a remote ATC instance).
- Team dashboards with multi-user views.
- Paid desktop app or pro binary tier with enhanced dashboards and packaged worker templates.

---

## Validation Milestones

| Milestone | Signal |
|-----------|--------|
| v1 shipped | 10+ GitHub stars within 2 weeks of launch post |
| Real adoption | 3+ independent users report running v1 in daily agent workflows |
| Team signal | 1+ team asks for shared/remote access (triggers v3 prioritisation) |
| Paid intent | 5+ users indicate willingness to pay for pro features |
