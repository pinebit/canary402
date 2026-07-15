#!/usr/bin/env sh
set -eu

NAME="canary402"
NAMESPACE="agent-canary402"
MODEL="${CANARY_AGENT_MODEL:-openrouter/auto}"
NETWORK="${CANARY_AGENT_NETWORK:-base-sepolia}"
PRICE="${CANARY_AGENT_PRICE:-0.001}"
PATH_PREFIX="/services/canary402-agent"
OBJECTIVE_FILE="agent/objective.md"

if obol kubectl -n "$NAMESPACE" get agent "$NAME" >/dev/null 2>&1; then
  obol agent update "$NAME" \
    --model "$MODEL" \
    --objective "$(sed -n '1,$p' "$OBJECTIVE_FILE")"
else
  obol agent new "$NAME" \
    --model "$MODEL" \
    --objective "$(sed -n '1,$p' "$OBJECTIVE_FILE")"
fi

obol kubectl apply -f deploy/agent-egress.yaml
obol kubectl apply -f deploy/x402-llm-reference-grant.yaml
obol kubectl -n "$NAMESPACE" rollout status deployment/hermes --timeout=180s

if obol kubectl -n "$NAMESPACE" get serviceoffer "$NAME" >/dev/null 2>&1; then
  echo "ServiceOffer $NAMESPACE/$NAME already exists."
else
  PAY_TO="$(
    obol agent wallet address obol-agent --runtime hermes --output json 2>&1 \
      | sed -nE 's/.*(0x[[:xdigit:]]{40}).*/\1/p' \
      | tail -n 1
  )"
  if ! printf '%s\n' "$PAY_TO" | grep -Eq '^0x[[:xdigit:]]{40}$'; then
    echo "Could not resolve a valid payout wallet." >&2
    exit 1
  fi

  obol sell agent "$NAME" \
    --pay-to "$PAY_TO" \
    --network "$NETWORK" \
    --token USDC \
    --price "$PRICE" \
    --path "$PATH_PREFIX" \
    --no-register
fi

# --description cannot be combined with --no-register in v0.13.0. Keep the
# public catalog concise and activate the existing shared identity only after
# its Base Sepolia Agent ID is present. This never mints a duplicate identity.
AGENT_ID="$(
  obol kubectl -n x402 get agentidentity default \
    -o "jsonpath={.status.registrations[?(@.chain=='$NETWORK')].agentId}" \
    2>/dev/null || true
)"
if [ -n "$AGENT_ID" ]; then
  REGISTRATION_ENABLED=true
else
  REGISTRATION_ENABLED=false
fi
obol kubectl -n "$NAMESPACE" patch serviceoffer "$NAME" --type=merge \
  -p="{\"spec\":{\"registration\":{\"enabled\":$REGISTRATION_ENABLED,\"name\":\"Canary402\",\"description\":\"Canary402 probes an x402-paid service, optionally makes one budget-capped downstream payment, and returns a public evidence-backed reliability report.\"}}}"

if [ "$REGISTRATION_ENABLED" = false ]; then
  echo "No $NETWORK AgentIdentity exists; ERC-8004 registration remains disabled."
fi

obol kubectl apply -f deploy/public-tunnel-routes.yaml

obol sell status "$NAME" -n "$NAMESPACE"
obol sell test "$NAME" -n "$NAMESPACE"

echo "Agent endpoint: https://andrei-obol-agent.dvlabs.dev$PATH_PREFIX/v1/chat/completions"
echo "x402scan listing is not configured."
