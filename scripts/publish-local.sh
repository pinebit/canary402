#!/usr/bin/env sh
set -eu

NAME="canary402"
NAMESPACE="llm"
NETWORK="${CANARY_SELL_NETWORK:-base-sepolia}"
PRICE="${CANARY_SELL_PRICE:-0.001}"
HOSTNAME="${CANARY_HOSTNAME:-}"

if obol kubectl -n "$NAMESPACE" get serviceoffer "$NAME" >/dev/null 2>&1; then
  echo "ServiceOffer $NAMESPACE/$NAME already exists."
else
  PAY_TO="$(
    obol agent wallet address obol-agent --runtime hermes --output json 2>&1 \
      | sed -nE 's/.*(0x[[:xdigit:]]{40}).*/\1/p' \
      | tail -n 1
  )"
  if ! printf '%s\n' "$PAY_TO" | grep -Eq '^0x[[:xdigit:]]{40}$'; then
    echo "Could not resolve a valid agent payout wallet." >&2
    exit 1
  fi

  set -- obol sell http "$NAME" \
    --upstream canary402 \
    --port 8080 \
    --namespace "$NAMESPACE" \
    --health-path /health \
    --pay-to "$PAY_TO" \
    --network "$NETWORK" \
    --token USDC \
    --price "$PRICE" \
    --no-register \
    --route "path=/audit,methods=POST,gate=paid,price=$PRICE,summary=Run an x402 endpoint audit" \
    --route "path=/reports/*,methods=GET,gate=free,summary=Read a public audit report" \
    --route "path=/health,methods=GET,gate=free,summary=Check Canary402 health" \
    --rps 2

  "$@"
fi

# Keep the permanent hostname on the shared storefront. In v0.13.0 a
# dedicated offer hostname rewrites /audit before the verifier builds its x402
# challenge, so the challenged URL differs from the URL buyers requested.
if [ -n "$HOSTNAME" ] && obol kubectl -n traefik get httproute tunnel-storefront >/dev/null 2>&1; then
  STOREFRONT_HOST="$(obol kubectl -n traefik get httproute tunnel-storefront -o jsonpath='{.spec.hostnames[0]}')"
  STOREFRONT_PATH="$(obol kubectl -n traefik get httproute tunnel-storefront -o jsonpath='{.spec.rules[0].matches[0].path.value}')"
  OFFER_HOST="$(obol kubectl -n "$NAMESPACE" get serviceoffer "$NAME" -o jsonpath='{.spec.hostname}')"
  if [ -n "$OFFER_HOST" ]; then
    obol kubectl -n "$NAMESPACE" patch serviceoffer "$NAME" --type=merge \
      -p='{"spec":{"hostname":null}}'
  fi
  if [ "$STOREFRONT_HOST" = "$HOSTNAME" ] && [ "$STOREFRONT_PATH" != "/" ]; then
    obol kubectl -n traefik patch httproute tunnel-storefront --type=json \
      -p='[{"op":"replace","path":"/spec/rules/0/matches/0/path/value","value":"/"}]'
  fi
fi

obol kubectl apply -f deploy/x402-llm-reference-grant.yaml

AGENT_ID="$(
  obol kubectl -n x402 get agentidentity default \
    -o "jsonpath={.status.registrations[?(@.chain=='$NETWORK')].agentId}" \
    2>/dev/null || true
)"
if [ -n "$AGENT_ID" ]; then
  obol kubectl -n "$NAMESPACE" patch serviceoffer "$NAME" --type=merge \
    -p='{"spec":{"registration":{"enabled":true,"name":"Canary402","description":"Deterministic evidence-backed reliability audits for x402-paid HTTP services.","metadata":{"pricingUnit":"audit","x402Asset":"USDC","x402Network":"base-sepolia","x402Price":"0.001"}}}}'
else
  echo "No $NETWORK AgentIdentity exists; ERC-8004 registration remains disabled."
fi

if [ "$HOSTNAME" = "andrei-obol-agent.dvlabs.dev" ]; then
  obol kubectl apply -f deploy/public-tunnel-routes.yaml
fi

obol sell status "$NAME" -n "$NAMESPACE"
obol sell test "$NAME" -n "$NAMESPACE" --path /audit

echo "Local paid endpoint: http://obol.stack:8080/services/canary402/audit"
if [ -n "$HOSTNAME" ]; then
  echo "Permanent storefront: https://$HOSTNAME"
  echo "Public paid endpoint: https://$HOSTNAME/services/canary402/audit"
else
  echo "No permanent hostname was requested; set CANARY_HOSTNAME for public routing."
fi
echo "x402scan listing is not configured."
