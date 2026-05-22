#!/usr/bin/env sh
# Generic shell worker for agent-task-center
# Requirements: curl, jq
# Usage: ATC_URL=http://localhost:8765 AGENT_ID=my-worker-1 ./examples/worker.sh

set -e

ATC_URL="${ATC_URL:-http://localhost:8765}"
AGENT_ID="${AGENT_ID:-shell-worker-1}"
DOMAIN="${DOMAIN:-}"
WORKSPACE_ID="${WORKSPACE_ID:-}"
POLL_INTERVAL="${POLL_INTERVAL:-5}"

# Register agent
echo "[worker] registering agent $AGENT_ID"
curl -s -X POST "$ATC_URL/api/agents/register" \
  -H "Content-Type: application/json" \
  -d "{
    \"agent_id\": \"$AGENT_ID\",
    \"name\": \"Shell Worker\",
    \"runtime\": \"shell\",
    \"runtime_version\": \"$(sh --version 2>&1 | head -1 || echo 'sh')\",
    \"domain\": \"${DOMAIN}\",
    \"workspace_id\": $([ -n "$WORKSPACE_ID" ] && echo "\"$WORKSPACE_ID\"" || echo "null"),
    \"capabilities\": []
  }" | jq -r '.agent_id // "registered"'

echo "[worker] starting poll loop (interval: ${POLL_INTERVAL}s)"

while true; do
  # Heartbeat agent
  curl -s -X POST "$ATC_URL/api/agents/$AGENT_ID/heartbeat" \
    -H "Content-Type: application/json" \
    -d '{}' > /dev/null

  # Lease a task
  LEASE=$(curl -s -X POST "$ATC_URL/api/tasks/lease" \
    -H "Content-Type: application/json" \
    -d "{
      \"agent_id\": \"$AGENT_ID\",
      \"workspace_id\": $([ -n "$WORKSPACE_ID" ] && echo "\"$WORKSPACE_ID\"" || echo "null"),
      \"domain\": $([ -n "$DOMAIN" ] && echo "\"$DOMAIN\"" || echo "null"),
      \"priority_gte\": 0
    }")

  # 204 = no task available
  TASK_ID=$(echo "$LEASE" | jq -r '.task.id // empty' 2>/dev/null)
  if [ -z "$TASK_ID" ]; then
    sleep "$POLL_INTERVAL"
    continue
  fi

  FENCING_TOKEN=$(echo "$LEASE" | jq -r '.fencing_token')
  INSTRUCTIONS=$(echo "$LEASE" | jq -r '.task.context.instructions // "no instructions"')
  echo "[worker] leased task $TASK_ID (token: $FENCING_TOKEN)"
  echo "[worker] instructions: $INSTRUCTIONS"

  # Heartbeat every 30 s while doing work
  (
    while true; do
      sleep 30
      curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/heartbeat" \
        -H "Content-Type: application/json" \
        -d "{\"agent_id\":\"$AGENT_ID\",\"fencing_token\":$FENCING_TOKEN}" > /dev/null
    done
  ) &
  HEARTBEAT_PID=$!

  # Execute task — replace this block with real work
  RESULT="{}"
  set +e
  echo "[worker] executing task $TASK_ID"
  # TODO: implement domain-specific logic here
  sleep 1
  EXIT_CODE=$?
  set -e

  kill "$HEARTBEAT_PID" 2>/dev/null || true

  if [ "$EXIT_CODE" -eq 0 ]; then
    curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/complete" \
      -H "Content-Type: application/json" \
      -d "{\"agent_id\":\"$AGENT_ID\",\"fencing_token\":$FENCING_TOKEN,\"result\":$RESULT}" > /dev/null
    echo "[worker] completed task $TASK_ID"
  else
    curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/fail" \
      -H "Content-Type: application/json" \
      -d "{\"agent_id\":\"$AGENT_ID\",\"fencing_token\":$FENCING_TOKEN,\"reason\":\"exit code $EXIT_CODE\",\"retry_hint\":true}" > /dev/null
    echo "[worker] failed task $TASK_ID (exit $EXIT_CODE)"
  fi

  sleep "$POLL_INTERVAL"
done
