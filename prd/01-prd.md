# Product Requirements Document — Agent Task Center

**Version:** 0.1 (MVP)  
**Status:** Draft

---

## One-liner

A local-first AI-powered task center that classifies incoming tickets (GitHub issues, Jira, Linear) into the right workflow and step, queues tasks for workers to claim, and provides an auditable operations dashboard — all through a simple HTTP API.

---

## Problem

Power users now run several agents in parallel: coding agents, research agents, review agents, and business-ops agents. Coordinating them without dedicated infrastructure means:

- no shared view of available work,
- no atomic claim mechanism — two workers pick the same task,
- no timeout or retry logic when a worker disappears,
- no auditable log of what happened during execution,
- no intelligence to route incoming tickets to the right workflow and step,
- no way to filter tasks by workflow, step, or worker.

Human-facing project management tools (Linear, GitHub Issues, Jira) are optimised for people, not for autonomous workers that need atomic leasing, heartbeat, idempotent status updates, and machine-readable filters. Full orchestration frameworks (Temporal, LangGraph, CrewAI) can coordinate workflows inside an application, but they do not provide a universal local inbox where independently launched agents can discover and coordinate external tasks.

---

## Target Users

| User | Description |
|------|-------------|
| **Primary** | Agentic-engineering power users running several local coding or business agents in parallel. |
| **Secondary** | Small teams experimenting with Claude Code, Codex CLI, OpenCode, Hermes, and custom scripts as local worker fleets. |
| **Buyer** | Individual developers first; later small teams that need local auditability, shared task state, and simple governance before adopting heavier orchestration platforms. |

---

## Product Vision

> "An AI-powered local command center that classifies incoming tickets into the right workflow step and queues them for workers to pick up."

Agent Task Center owns a narrow boundary:

1. External tickets (GitHub issues, Jira, Linear) arrive via `POST /api/classify`.
2. An LLM reads the workflow definition and decides the next step.
3. The task is queued with `workflow_name` + `step`.
4. Workers poll for tasks matching their workflow + step, atomically leasing one at a time.
5. Task-specific timeout rules protect against hung workers.
6. Workers report progress, logs, result metadata, and completion.
7. Humans and supervising workers inspect metrics, task state, and logs from a small read-oriented dashboard.

The differentiation is intentional simplicity: one local service, one SQLite file, a tiny HTTP API, and a dashboard that can be understood in minutes.

---

## Guiding Principles

1. **Correctness before features.** Atomic leasing and fencing tokens must be right before UI polish is added.
2. **Pull-only in v1.** Agents poll for tasks. No push notifications or webhooks in v1.
3. **API-first.** Task creation and updates happen through the API. The dashboard is read-oriented; it does not replace the API for data entry.
4. **Local-first.** Runs as a single binary backed by one SQLite file. No distributed infrastructure required.
5. **No opinions on agent internals.** Agent Task Center coordinates externally launched workers. It does not launch, supervise, or introspect agent processes.
6. **Minimal footprint.** If a feature requires significant complexity without a validated use case, it is deferred.

---

## Out of Scope (v1)

- Distributed cluster coordination or multi-node deployment.
- Cloud-hosted multi-tenant SaaS.
- Full DAG workflow engine with conditional branching or parallel steps.
- Replacing Linear, GitHub Issues, or any human project management system.
- Deep IDE integration.
- Role-based access control or authentication.
- Launching workers directly from the product.
- Human-first task editing workflows in the dashboard.
- Push/webhook delivery to workers.
- SSE live-stream updates in the dashboard.
- Worker/agent registration (workers are free strings — no sign-up required).

---

## Alternatives Considered

| Tool | Gap |
|------|-----|
| **Vibe Kanban** | Focuses on coding workspaces, diffs, and PR flow — not a general-purpose task API for any local agent. |
| **Mission Control-style dashboards** | Broader fleet management with adapters, cost tracking, skills, and governance — heavier than the proposed wedge. |
| **Linear / GitHub Issues / Jira** | Optimised for humans; agents need atomic claim, visibility timeout, heartbeat, and machine-friendly filters. |
| **Temporal / Celery / Redis** | Robust but requires infrastructure setup; designed for durability at scale, not local-first simplicity. |
| **LangGraph / CrewAI / AutoGen** | Define workflows inside an application; do not give a universal external inbox for unrelated agents. |
