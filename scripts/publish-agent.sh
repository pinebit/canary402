#!/usr/bin/env sh
set -eu

NAME="canary402"
NAMESPACE="agent-canary402"
MODEL="${CANARY_AGENT_MODEL:-openrouter/auto}"
NETWORK="${CANARY_AGENT_NETWORK:-base}"
IDENTITY_NETWORK="${CANARY_AGENT_IDENTITY_NETWORK:-base-sepolia}"
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

OFFER_EXISTS=false
if obol kubectl -n "$NAMESPACE" get serviceoffer "$NAME" >/dev/null 2>&1; then
  OFFER_EXISTS=true
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

# Disable registration before a cross-chain payment migration. Otherwise the
# v0.13 controller can briefly publish the existing numeric ID against the new
# chain's unrelated registry token while it reconciles the payment update.
if [ "$OFFER_EXISTS" = true ] && [ "$NETWORK" != "$IDENTITY_NETWORK" ]; then
  obol kubectl -n "$NAMESPACE" patch serviceoffer "$NAME" --type=merge \
    -p='{"spec":{"registration":{"enabled":false}}}' >/dev/null
fi

# Reconcile an existing offer as well as a newly created one. Updating the
# payment network does not mint or replace the Agent's ERC-8004 identity.
obol sell update "$NAME" -n "$NAMESPACE" \
  --network "$NETWORK" \
  --per-request "$PRICE"

# --description cannot be combined with --no-register in v0.13.0. Keep the
# public catalog concise. Agent ID 8104 remains on Base Sepolia while buyers
# pay on Base, so v0.13 offer registration must remain disabled.
AGENT_ID="$(
  obol kubectl -n x402 get agentidentity default \
    -o "jsonpath={.status.registrations[?(@.chain=='$IDENTITY_NETWORK')].agentId}" \
    2>/dev/null || true
)"
# Obol v0.13 derives RegistrationRequest.spec.chain from the payment network.
# Enabling registration when these chains differ would advertise the same
# numeric Agent ID in the wrong registry (which can belong to another wallet).
if [ -n "$AGENT_ID" ] && [ "$NETWORK" = "$IDENTITY_NETWORK" ]; then
  REGISTRATION_ENABLED=true
else
  REGISTRATION_ENABLED=false
fi
REGISTRATION_PATCH="$(
  jq -cn \
    --argjson enabled "$REGISTRATION_ENABLED" \
    --arg model "$MODEL" \
    --arg network "$NETWORK" \
    --arg price "$PRICE" \
    '{spec:{registration:{enabled:$enabled,name:"Canary402",description:"Canary402 inspects an x402 service contract, generates repair templates, and can make one explicitly authorized budget-capped payment.",metadata:{model:$model,pricingUnit:"agent-turn",runtime:"hermes",x402Asset:"USDC",x402Network:$network,x402Price:$price}}}}'
)"
obol kubectl -n "$NAMESPACE" patch serviceoffer "$NAME" --type=merge \
  -p="$REGISTRATION_PATCH"

if [ -z "$AGENT_ID" ]; then
  echo "No $IDENTITY_NETWORK AgentIdentity exists; ERC-8004 registration remains disabled."
elif [ "$NETWORK" != "$IDENTITY_NETWORK" ]; then
  echo "Payment network $NETWORK differs from identity network $IDENTITY_NETWORK; offer-level ERC-8004 publication remains disabled to avoid advertising the wrong registry token."
fi

obol kubectl apply -f deploy/public-tunnel-routes.yaml

obol sell status "$NAME" -n "$NAMESPACE"
obol sell test "$NAME" -n "$NAMESPACE"

echo "Agent endpoint: https://andrei-obol-agent.dvlabs.dev$PATH_PREFIX/v1/chat/completions"
echo "Payment network: $NETWORK; identity network: $IDENTITY_NETWORK (Agent ID ${AGENT_ID:-not found})."
