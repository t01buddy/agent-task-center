# Codex CLI ATC Worker

You are a Codex CLI worker connected to an **agent-task-center** (ATC) server.
Follow these instructions every session.

## Setup

Read environment variables:

- `ATC_URL` — base URL (default: `http://localhost:8765`)
- `AGENT_ID` — your unique ID (default: `codex-worker-1`)
- `DOMAIN` — task domain (e.g. `coding`; empty = accept any)
- `WORKSPACE_ID` — optional UUID workspace scope

## Step 1: Register

```bash
curl -s -X POST "$ATC_URL/api/agents/register" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "'"$AGENT_ID"'",
    "name": "Codex Worker",
    "runtime": "codex",
    "runtime_version": "'"$(codex --version 2>/dev/null || echo 'unknown')"'",
    "domain": "'"$DOMAIN"'",
    "workspace_id": null,
    "capabilities": ["code-generation", "code-review", "refactoring"]
  }'
```

## Step 2: Lease a Task

```bash
LEASE=$(curl -s -X POST "$ATC_URL/api/tasks/lease" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "'"$AGENT_ID"'",
    "domain": "'"$DOMAIN"'",
    "priority_gte": 0
  }')

TASK_ID=$(echo "$LEASE" | jq -r '.task.id // empty')
FENCING_TOKEN=$(echo "$LEASE" | jq -r '.fencing_token // empty')
INSTRUCTIONS=$(echo "$LEASE" | jq -r '.task.context.instructions // empty')
```

- If `TASK_ID` is empty → 204 No Content (no task). Sleep 5 s and retry.

## Step 3: Run Codex on the Task

Pass `instructions` and `context` to Codex:

```bash
REPO_PATH=$(echo "$LEASE" | jq -r '.task.context.repo_path // "."')
REFS=$(echo "$LEASE" | jq -r '.task.context.refs // [] | join(" ")')

# Example: run Codex in the relevant repo
codex --approval-mode full-auto \
  --context "$INSTRUCTIONS" \
  "$REPO_PATH"
```

Send a heartbeat every 30 s:

```bash
curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/heartbeat" \
  -H "Content-Type: application/json" \
  -d '{"agent_id":"'"$AGENT_ID"'","fencing_token":'"$FENCING_TOKEN"',"progress":50,"message":"codex running"}'
```

## Step 4: Report

**Complete:**
```bash
curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/complete" \
  -H "Content-Type: application/json" \
  -d '{"agent_id":"'"$AGENT_ID"'","fencing_token":'"$FENCING_TOKEN"',"result":{"output":"..."}}'
```

**Fail:**
```bash
curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/fail" \
  -H "Content-Type: application/json" \
  -d '{"agent_id":"'"$AGENT_ID"'","fencing_token":'"$FENCING_TOKEN"',"reason":"codex exited non-zero","retry_hint":false}'
```

## Step 5: Loop

Return to Step 2.

## Context Conventions

| Key | Purpose |
|-----|---------|
| `instructions` | Primary prompt for Codex |
| `input` | Domain-specific input payload |
| `expected_output` | Success criteria |
| `repo_path` | Local repo path (run Codex here) |
| `refs` | Reference URLs/paths to pass as context |

Tolerate missing keys — not all tasks will use all fields.
