#!/usr/bin/env sh
# Generic shell worker for agent-task-center
# Requirements: curl, jq
#
# Usage:
#   ATC_URL=http://localhost:8765 \
#   WORKER_ID=my-worker-1 \
#   WORKFLOW_NAME=bug-fix \
#   STEP=implement \
#   ./examples/worker.sh

set -e

ATC_URL="${ATC_URL:-http://localhost:8765}"
WORKER_ID="${WORKER_ID:-shell-worker-1}"
WORKFLOW_NAME="${WORKFLOW_NAME:-}"
STEP="${STEP:-}"
DOMAIN="${DOMAIN:-}"
POLL_INTERVAL="${POLL_INTERVAL:-5}"

echo "[worker] starting — id=$WORKER_ID workflow=$WORKFLOW_NAME step=$STEP"

while true; do
  # Build lease request — include only non-empty filters
  LEASE_BODY=$(printf '{"worker_id":"%s"' "$WORKER_ID")
  [ -n "$WORKFLOW_NAME" ] && LEASE_BODY="$LEASE_BODY,\"workflow_name\":\"$WORKFLOW_NAME\""
  [ -n "$STEP" ]          && LEASE_BODY="$LEASE_BODY,\"step\":\"$STEP\""
  [ -n "$DOMAIN" ]        && LEASE_BODY="$LEASE_BODY,\"domain\":\"$DOMAIN\""
  LEASE_BODY="$LEASE_BODY}"

  LEASE=$(curl -s -w "\n%{http_code}" -X POST "$ATC_URL/api/tasks/lease" \
    -H "Content-Type: application/json" \
    -d "$LEASE_BODY")
  HTTP_CODE=$(printf '%s' "$LEASE" | tail -1)
  LEASE_BODY_RESP=$(printf '%s' "$LEASE" | head -n -1)

  # 204 = no task available
  if [ "$HTTP_CODE" = "204" ]; then
    sleep "$POLL_INTERVAL"
    continue
  fi

  TASK_ID=$(printf '%s' "$LEASE_BODY_RESP" | jq -r '.task.id // empty')
  if [ -z "$TASK_ID" ]; then
    sleep "$POLL_INTERVAL"
    continue
  fi

  FENCING_TOKEN=$(printf '%s' "$LEASE_BODY_RESP" | jq -r '.fencing_token')
  TASK_STEP=$(printf '%s' "$LEASE_BODY_RESP" | jq -r '.task.step // ""')
  TASK_WORKFLOW=$(printf '%s' "$LEASE_BODY_RESP" | jq -r '.task.workflow_name // ""')
  INSTRUCTIONS=$(printf '%s' "$LEASE_BODY_RESP" | jq -r '.task.context.instructions // "no instructions"')

  echo "[worker] leased task=$TASK_ID workflow=$TASK_WORKFLOW step=$TASK_STEP token=$FENCING_TOKEN"
  echo "[worker] instructions: $INSTRUCTIONS"

  # Heartbeat in background every 30 s while working
  (
    while true; do
      sleep 30
      curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/heartbeat" \
        -H "Content-Type: application/json" \
        -d "{\"worker_id\":\"$WORKER_ID\",\"fencing_token\":$FENCING_TOKEN,\"message\":\"working\"}" > /dev/null
    done
  ) &
  HEARTBEAT_PID=$!

  # Execute task — replace this block with real domain logic
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
      -d "{\"worker_id\":\"$WORKER_ID\",\"fencing_token\":$FENCING_TOKEN,\"result\":$RESULT}" > /dev/null
    echo "[worker] completed task $TASK_ID"
  else
    curl -s -X POST "$ATC_URL/api/tasks/$TASK_ID/fail" \
      -H "Content-Type: application/json" \
      -d "{\"worker_id\":\"$WORKER_ID\",\"fencing_token\":$FENCING_TOKEN,\"reason\":\"exit code $EXIT_CODE\",\"retry_hint\":true}" > /dev/null
    echo "[worker] failed task $TASK_ID (exit $EXIT_CODE)"
  fi

  sleep "$POLL_INTERVAL"
done
