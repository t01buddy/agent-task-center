# Tech Stack & Architecture

---

## Overview

Agent Task Center is a single local HTTP service with a minimal frontend and a SQLite database. The design goal is zero required external dependencies: one binary, one database file, and a browser.

---

## Backend

| Concern | Choice | Rationale |
|---------|--------|-----------|
| Language | **Go** | Single binary compilation, good HTTP stdlib, easy cross-platform distribution. |
| HTTP router | **`net/http` + minimal router** | No heavy framework needed for the small API surface. |
| Templating | **[Templ](https://templ.guide/)** | Type-safe Go HTML components, compiled into Go. Eliminates runtime template parsing. |
| Database | **SQLite (WAL mode)** | Zero server dependencies, file-based, proven for local-first services, good concurrent read performance in WAL mode. |
| SQLite driver | **`modernc.org/sqlite`** | Pure-Go SQLite — no CGO, simplifies cross-compilation and static binaries. |
| Migrations | **Embedded SQL migration files** | Applied on startup, versioned, idempotent. No external migration tool required. |
| AI Classification | **OpenAI-compatible API (raw HTTP) or Codex CLI** | Workflow + step detection from ticket title/context; provider-agnostic via `internal/ai.Provider` interface. |

---

## Frontend

| Concern | Choice | Rationale |
|---------|--------|-----------|
| Page rendering | **Templ** (server-side) | Dashboard pages are server-rendered Go templates; no separate frontend build step. |
| Incremental updates | **[HTMX](https://htmx.org/)** | Polls server for fresh HTML fragments. Keeps dashboard live without a JS framework. Used for metrics refresh and task list reloads. |
| Local interactions | **[Alpine.js](https://alpinejs.dev/)** | Small reactive sprinkles for drawers, dropdowns, and filter toggles. Loaded from CDN; no build step. |
| Styles | **Plain CSS or minimal utility classes** | No heavy CSS framework. Functional, monochrome, operator-oriented aesthetic. |

No client-side build pipeline (no Webpack, Vite, or Node.js required to run or contribute).

---

## Data Storage

- **Single SQLite file**, path configurable via env var or CLI flag (default: `./agent-task-center.db`).
- **WAL mode** enabled on startup for better concurrent read/write performance.
- All writes go through the Go service; no direct database access from agents.
- **Fencing tokens** (monotonically increasing integers) are stored per task attempt to reject late writes from timed-out workers.

---

## Queue Model

Visibility-timeout leasing, modelled on SQS semantics:

1. A lease request atomically sets `status = leased`, `lease_expires_at = now + visibility_timeout`, and returns a `fencing_token`.
2. The agent must heartbeat before `lease_expires_at` to extend the lease.
3. A background goroutine (or on-read check) expires leases past their deadline, requeuing the task if `attempt_count < max_attempts`.
4. `complete` and `fail` calls must include the `fencing_token`; stale tokens are rejected (the task has already been re-leased to another agent).

---

## API Style

- **REST over HTTP** with JSON request/response bodies.
- All endpoints under `/api/`.
- Dashboard pages served at `/` (Templ-rendered HTML).
- No authentication in v1 — assumes trusted local network or localhost.
- Optional SSE stream at `/api/stream` deferred to v2.

---

## Packaging & Distribution

| Target | Approach |
|--------|---------|
| Local dev | `go run ./cmd/atc` |
| Binary | `go build -o atc ./cmd/atc` — single static binary |
| Docker | Optional `Dockerfile` for container usage |
| Configuration | Environment variables and/or a simple TOML config file |

No installer, no background daemon framework. Agents and humans start the service manually or add it to a local process manager.

---

## Directory Layout (proposed)

```
agent-task-center/
├── cmd/
│   └── atc/          # main entry point
├── internal/
│   ├── ai/           # LLM provider abstraction (OpenAI, Codex CLI) + classify logic
│   ├── api/          # HTTP handlers (tasks, workflows, classify, logs, metrics, events)
│   ├── config/       # environment-based config
│   ├── db/           # SQLite open + migration runner
│   ├── model/        # domain types
│   ├── queue/        # lease expiry background loop
│   └── dashboard/    # server-rendered HTML dashboard
├── migrations/       # numbered SQL migration files
├── examples/         # worker example scripts
└── prd/              # this document and siblings
```
