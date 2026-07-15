#!/usr/bin/env sh
set -eu

PORT="${CANARY_LOCAL_PORT:-18080}"
LOG="/tmp/canary402-port-forward.log"

obol kubectl -n llm port-forward service/canary402 "$PORT:8080" >"$LOG" 2>&1 &
FORWARD_PID=$!
trap 'kill "$FORWARD_PID" >/dev/null 2>&1 || true' EXIT INT TERM

attempt=0
until curl --noproxy '*' -fsS "http://127.0.0.1:$PORT/health" >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo "Port-forward did not become ready. See $LOG" >&2
    exit 1
  fi
  sleep 1
done

curl --noproxy '*' -fsS "http://127.0.0.1:$PORT/health"
echo

if obol kubectl -n llm get serviceoffer canary402 >/dev/null 2>&1; then
  obol sell test canary402 -n llm --path /audit
else
  echo "Service is healthy; run 'make sell' to add the local x402 gate."
fi
