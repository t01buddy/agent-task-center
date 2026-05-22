# agent-task-center

A lightweight task queue and agent coordination server. Agents register, poll for tasks, heartbeat during execution, and report results. Built with Go + SQLite.

## Prerequisites

- Go 1.22+
- `curl` and `jq` (for shell worker examples)

## Quick Start

```bash
go run ./cmd/atc
```

The server starts on `http://localhost:8765` by default. Set `ATC_ADDR` to override.

## One-Command Demo

Run each command in a separate terminal:

```bash
# Terminal 1: start the server
go run ./cmd/atc

# Terminal 2: create a workspace, add a task, run a worker
ATC=http://localhost:8765

# Create workspace
WS=$(curl -s -X POST $ATC/api/workspaces \
  -H "Content-Type: application/json" \
  -d '{"name":"demo"}' | jq -r '.id')

# Create a task
TASK=$(curl -s -X POST $ATC/api/tasks \
  -H "Content-Type: application/json" \
  -d "{\"title\":\"hello world\",\"workspace_id\":\"$WS\",\"domain\":\"demo\",\"priority\":10,\"context\":{\"instructions\":\"Print hello world and exit.\"}}" | jq -r '.id')

echo "Task created: $TASK"

# Run the shell worker (it will pick up the task and complete it)
ATC_URL=$ATC AGENT_ID=demo-worker-1 DOMAIN=demo ./examples/worker.sh
```

Watch the worker pick up and complete the task.

## Examples

| Path | Description |
|------|-------------|
| [`examples/worker.sh`](examples/worker.sh) | POSIX shell worker using `curl` + `jq` |
| [`examples/claude-code-worker/AGENTS.md`](examples/claude-code-worker/AGENTS.md) | Instructions for a Claude Code session acting as a worker |
| [`examples/codex-worker/AGENTS.md`](examples/codex-worker/AGENTS.md) | Instructions for a Codex CLI worker |

## Documentation

- [PRD](prd/) — product requirements and API specification
- [API Spec](prd/06-api-spec.md) — full REST API reference
