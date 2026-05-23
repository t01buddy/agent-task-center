# Claude Code ATC Worker

You are a Claude Code worker connected to an **agent-task-center** (ATC) server.
Follow these instructions every time you start a session.

## Setup

Read the following environment variables before doing anything else:

- `ATC_URL` — base URL of the ATC server (default: `http://localhost:8765`)
- `WORKER_ID` — your unique worker ID (default: `claude-code-worker-1`)
- `WORKFLOW_NAME` — workflow to poll (e.g. `bug-fix`; required)
- `STEP` — step within that workflow to claim (e.g. `implement`, `review`; required)
- `DOMAIN` — optional domain filter (e.g. `coding`, `review`)

No registration required. Workers are identified by `WORKER_ID` — a free string you choose.

## Step 1: Poll for a Task

```bash
curl -s -X POST "$ATC_URL/api/tasks/lease" \
  -H "Content-Type: application/json" \
  -d "{
    \"worker_id\": \"$WORKER_ID\",
    \"workflow_name\": \"$WORKFLOW_NAME\",
    \"step\": \"$STEP\",
    \"domain\": \"$DOMAIN\"
  }"
```

- **200** → task leased. Extract `task.id`, `task.context`, and `fencing_token`.
- **204** → no task available. Wait 5 s and retry.

## Step 2: Execute the Task

Read `task.context` carefully:

| Key | Meaning |
|-----|---------|
| `instructions` | What you must do |
| `input` | Domain-specific input (e.g. PR URL, file paths) |
| `expected_output` | What a correct result looks like |
| `repo_path` | Local repo path to work in (if any) |
| `refs` | URLs or file paths for reference material |

Do the work. For tasks taking more than 30 s, send heartbeats to extend the lease:

```bash
curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/heartbeat" \
  -H "Content-Type: application/json" \
  -d "{\"worker_id\":\"$WORKER_ID\",\"fencing_token\":$FENCING_TOKEN,\"progress\":50,\"message\":\"halfway\"}"
```

## Step 3: Report Result

**Success:**
```bash
curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/complete" \
  -H "Content-Type: application/json" \
  -d "{\"worker_id\":\"$WORKER_ID\",\"fencing_token\":$FENCING_TOKEN,\"result\":{\"summary\":\"...\"}}"
```

**Failure:**
```bash
curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/fail" \
  -H "Content-Type: application/json" \
  -d "{\"worker_id\":\"$WORKER_ID\",\"fencing_token\":$FENCING_TOKEN,\"reason\":\"...\",\"retry_hint\":true}"
```

## Step 4: Loop

Go back to Step 1 and poll again.

## Rules

- Always heartbeat for tasks that take more than 30 s.
- Never mark a task complete unless you have verified the output.
- Use `retry_hint: true` for transient failures (network, timeout). Use `false` for permanent failures.
- Log each `task.id`, `step`, and result for observability.
- The `fencing_token` is your authority — include it on every heartbeat, complete, and fail call.
