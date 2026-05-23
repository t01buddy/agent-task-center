# agent-task-center

AI-powered task queue for multi-agent workflows. Incoming tickets are classified by an LLM into the right workflow and step, then queued for workers to poll and execute. Built with Go + SQLite — single binary, zero external dependencies.

## How it works

```
External ticket → POST /api/classify
  → LLM reads workflow definition → picks next step
  → Task queued with workflow_name + step
  → Worker polls: POST /api/tasks/lease {"workflow_name":"bug-fix","step":"implement"}
  → Worker heartbeats, then completes or fails
```

## Prerequisites

- Go 1.22+
- `curl` and `jq` (for shell worker examples)
- An OpenAI-compatible API key **or** the [Codex CLI](https://github.com/openai/codex) (for classification)

## Quick Start

```bash
# With OpenAI
ATC_LLM_API_KEY=sk-... go run ./cmd/atc

# With Codex CLI
ATC_LLM_PROVIDER=codex go run ./cmd/atc
```

The server starts on `http://localhost:8765`. Set `ATC_ADDR` to override.

## One-Command Demo

```bash
ATC=http://localhost:8765

# 1. Create a workflow
curl -s -X POST $ATC/api/workflows \
  -H "Content-Type: application/json" \
  -d '{
    "name": "bug-fix",
    "definition": "Bug-fix workflow: triage (reproduce the bug), implement (write the fix), review (code review), deploy (ship it)."
  }' | jq .

# 2. Classify a ticket — LLM decides the step
curl -s -X POST $ATC/api/classify \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Login bypass vulnerability",
    "context": {"source": "github_issue", "description": "Users can log in without a password."},
    "run_id": "issue-42"
  }' | jq '{step: .step, reasoning: .reasoning}'

# 3. A worker polls and claims the task
curl -s -X POST $ATC/api/tasks/lease \
  -H "Content-Type: application/json" \
  -d '{"worker_id": "my-worker-1", "workflow_name": "bug-fix", "step": "triage"}' | jq .
```

Open `http://localhost:8765` to see the dashboard.

## Configuration

| Env var | Default | Description |
|---------|---------|-------------|
| `ATC_ADDR` | `:8765` | Listen address |
| `ATC_DB_PATH` | `./agent-task-center.db` | SQLite database path |
| `ATC_LLM_PROVIDER` | `openai` | LLM provider: `openai` or `codex` |
| `ATC_LLM_BASE_URL` | `https://api.openai.com/v1` | OpenAI-compatible base URL |
| `ATC_LLM_API_KEY` | — | API key for OpenAI-compatible provider |
| `ATC_LLM_MODEL` | `gpt-4o-mini` | Model name |
| `ATC_CODEX_MODEL` | — | Model override for Codex CLI |
| `ATC_LOG_FORMAT` | `json` | Log format: `json` or `text` |
| `ATC_EXPIRY_INTERVAL_S` | `10` | Lease expiry check interval (s) |
| `ATC_DRAIN_TIMEOUT_S` | `5` | Graceful shutdown timeout (s) |

## Worker Examples

| Path | Description |
|------|-------------|
| [`examples/worker.sh`](examples/worker.sh) | POSIX shell worker using `curl` + `jq` |
| [`examples/claude-code-worker/AGENTS.md`](examples/claude-code-worker/AGENTS.md) | Claude Code session acting as a worker |
| [`examples/codex-worker/AGENTS.md`](examples/codex-worker/AGENTS.md) | Codex CLI worker |

Workers need no registration. Identify with any `worker_id` string and poll by `workflow_name` + `step`.

## Documentation

- [PRD](prd/) — product requirements
- [API Spec](prd/06-api-spec.md) — full REST API reference
- [Data Models](prd/05-data-models.md) — SQLite schema
