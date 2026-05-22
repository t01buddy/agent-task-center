# Product Requirements Document — Agent Task Center

**Version:** 0.1 (MVP)  
**Status:** Draft

---

## One-liner

A local-first task queue and operations dashboard where multiple AI agents can claim work, respect timeouts, update status, and leave auditable logs through a simple HTTP API.

---

## Problem

Power users now run several agents in parallel: coding agents, research agents, review agents, and business-ops agents. Coordinating them without dedicated infrastructure means:

- no shared view of available work,
- no atomic claim mechanism — two agents pick the same task,
- no visibility into whether an agent is still alive,
- no timeout or retry logic when a worker disappears,
- no auditable log of what happened during execution,
- no way to filter tasks by worker type or project.

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

> "A tiny local task center where your coding and business agents pick up work and report back."

Agent Task Center owns a narrow boundary:

1. Agents ask for eligible tasks.
2. The system atomically leases one task to one agent.
3. Task-specific timeout rules protect against hung agents.
4. Agents report progress, logs, result metadata, and completion.
5. Humans and supervising agents inspect metrics, task state, and logs from a small read-oriented dashboard.

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
- Full workflow DAG engine or workflow definition language.
- Replacing Linear, GitHub Issues, or any human project management system.
- Deep IDE integration.
- Role-based access control or authentication.
- Launching agents directly from the product.
- Human-first task editing workflows in the dashboard.
- Push/webhook delivery to agents.
- SSE live-stream updates in the dashboard.

---

## Alternatives Considered

| Tool | Gap |
|------|-----|
| **Vibe Kanban** | Focuses on coding workspaces, diffs, and PR flow — not a general-purpose task API for any local agent. |
| **Mission Control-style dashboards** | Broader fleet management with adapters, cost tracking, skills, and governance — heavier than the proposed wedge. |
| **Linear / GitHub Issues / Jira** | Optimised for humans; agents need atomic claim, visibility timeout, heartbeat, and machine-friendly filters. |
| **Temporal / Celery / Redis** | Robust but requires infrastructure setup; designed for durability at scale, not local-first simplicity. |
| **LangGraph / CrewAI / AutoGen** | Define workflows inside an application; do not give a universal external inbox for unrelated agents. |
