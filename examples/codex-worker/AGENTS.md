# Codex CLI ATC Worker

You are a Codex CLI worker connected to an **agent-task-center** (ATC) server.
Follow these instructions every session.

## Setup

Read environment variables:

- `ATC_URL` — base URL (default: `http://localhost:8765`)
- `WORKER_ID` — your unique worker ID (default: `codex-worker-1`)
- `WORKFLOW_NAME` — workflow to poll (e.g. `bug-fix`; required)
- `STEP` — step within that workflow to claim (e.g. `implement`; required)
- `DOMAIN` — optional domain filter (e.g. `coding`)

No registration required. Workers are identified by `WORKER_ID` — a free string you choose.

## Step 1: Lease a Task

```bash
LEASE=$(curl -s -X POST "$ATC_URL/api/tasks/lease" \
  -H "Content-Type: application/json" \
  -d "{
    \"worker_id\": \"$WORKER_ID\",
    \"workflow_name\": \"$WORKFLOW_NAME\",
    \"step\": \"$STEP\",
    \"domain\": \"$DOMAIN\"
  }")

TASK_ID=$(echo "$LEASE" | jq -r '.task.id // empty')
FENCING_TOKEN=$(echo "$LEASE" | jq -r '.fencing_token // empty')
INSTRUCTIONS=$(echo "$LEASE" | jq -r '.task.context.instructions // empty')
```

- If `TASK_ID` is empty → 204 No Content (no task). Sleep 5 s and retry.

## Step 2: Run Codex on the Task

```bash
REPO_PATH=$(echo "$LEASE" | jq -r '.task.context.repo_path // "."')

codex exec --dangerously-bypass-approvals-and-sandbox "$INSTRUCTIONS"
```

Send a heartbeat every 30 s while Codex is running:

```bash
curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/heartbeat" \
  -H "Content-Type: application/json" \
  -d "{\"worker_id\":\"$WORKER_ID\",\"fencing_token\":$FENCING_TOKEN,\"progress\":50,\"message\":\"codex running\"}"
```

## Step 3: Report

**Complete:**
```bash
curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/complete" \
  -H "Content-Type: application/json" \
  -d "{\"worker_id\":\"$WORKER_ID\",\"fencing_token\":$FENCING_TOKEN,\"result\":{\"output\":\"...\"}}"
```

**Fail:**
```bash
curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/fail" \
  -H "Content-Type: application/json" \
  -d "{\"worker_id\":\"$WORKER_ID\",\"fencing_token\":$FENCING_TOKEN,\"reason\":\"codex exited non-zero\",\"retry_hint\":false}"
```

## Step 4: Loop

Return to Step 1.

## Context Conventions

| Key | Purpose |
|-----|---------|
| `instructions` | Primary prompt for Codex |
| `input` | Domain-specific input payload |
| `expected_output` | Success criteria |
| `repo_path` | Local repo path (run Codex here) |
| `refs` | Reference URLs/paths to pass as context |

Tolerate missing keys — not all tasks will use all fields.
