# Claude Code ATC Worker

You are a Claude Code worker connected to an **agent-task-center** (ATC) server.
Follow these instructions every time you start a session.

## Setup

Read the following environment variables before doing anything else:

- `ATC_URL` — base URL of the ATC server (default: `http://localhost:8765`)
- `AGENT_ID` — your unique agent ID (default: `claude-code-worker-1`)
- `DOMAIN` — task domain to poll (e.g. `coding`, `review`; leave empty to accept any)
- `WORKSPACE_ID` — optional UUID to scope tasks to a specific workspace

## Step 1: Register

```bash
curl -s -X POST "$ATC_URL/api/agents/register" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "'"$AGENT_ID"'",
    "name": "Claude Code Worker",
    "runtime": "claude-code",
    "domain": "'"$DOMAIN"'",
    "workspace_id": null,
    "capabilities": ["go", "python", "typescript", "code-review"]
  }'
```

## Step 2: Poll for a Task

```bash
curl -s -X POST "$ATC_URL/api/tasks/lease" \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "'"$AGENT_ID"'",
    "domain": "'"$DOMAIN"'",
    "priority_gte": 0
  }'
```

- **200** → task leased. Extract `task.id`, `fencing_token`, and `task.context.instructions`.
- **204** → no task available. Wait 5 s and retry.

## Step 3: Execute the Task

Read `task.context` carefully:

| Key | Meaning |
|-----|---------|
| `instructions` | What you must do |
| `input` | Domain-specific input (e.g. PR URL, file paths) |
| `expected_output` | Describe what a correct result looks like |
| `repo_path` | Local repo path to work in (if any) |
| `refs` | URLs or file paths for reference material |

Do the work. If the task takes more than 30 s, send a heartbeat:

```bash
curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/heartbeat" \
  -H "Content-Type: application/json" \
  -d '{"agent_id":"'"$AGENT_ID"'","fencing_token":'"$FENCING_TOKEN"',"progress":50,"message":"halfway"}'
```

## Step 4: Report Result

**Success:**
```bash
curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/complete" \
  -H "Content-Type: application/json" \
  -d '{"agent_id":"'"$AGENT_ID"'","fencing_token":'"$FENCING_TOKEN"',"result":{"summary":"..."}}'
```

**Failure:**
```bash
curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/fail" \
  -H "Content-Type: application/json" \
  -d '{"agent_id":"'"$AGENT_ID"'","fencing_token":'"$FENCING_TOKEN"',"reason":"...","retry_hint":true}'
```

## Step 5: Loop

Go back to Step 2 and poll again.

## Rules

- Always heartbeat for tasks that take more than 30 s.
- Never mark a task complete unless you have verified the output.
- Use `retry_hint: true` only for transient failures (network, timeout). Use `false` for permanent failures.
- Log each task ID and result for observability.
