# Canary402

Canary402 is a service-contract inspector and mystery shopper for x402-paid services:

> Before an agent integrates with or pays a service, Canary402 checks the contract and verifies the delivery.

It inspects public service specifications, generates value-free repair templates, validates the live x402 challenge, optionally makes one strictly budgeted downstream payment through the Obol remote signer, evaluates the delivered result with OpenRouter, and retains an evidence-backed report.

The original concept is preserved in [idea.md](idea.md).

## Live Agent

The public product is the **Canary402 Agent** at:

```text
POST https://andrei-obol-agent.dvlabs.dev/services/canary402-agent/v1/chat/completions
```

The Agent is a Hermes/OpenRouter service sold for `0.001 USDC` per request on **Base mainnet**. Its deterministic Go audit API remains deployed inside the Obol cluster and is not sold, advertised, or routed publicly. Calling the Agent therefore incurs one Canary402 charge, not a second internal API charge.

Canary402 is registered as [ERC-8004 Agent ID 59094 on Base mainnet](https://basescan.org/nft/0x8004A169FB4a3325136EB29fA0ceB6D2e539a432/59094), matching its x402 payment network. The same signer also retains the legacy [Agent ID 8104 on Base Sepolia](https://sepolia.basescan.org/nft/0x8004A818BFB912233c491871b3d84c89A494BD9e/8104). Both registration entries point to the same public Agent and are owned by the Canary402 wallet.

Public discovery documents are available from the shared storefront:

- `GET https://andrei-obol-agent.dvlabs.dev/openapi.json`
- `GET https://andrei-obol-agent.dvlabs.dev/.well-known/agent-registration.json`

x402scan crawled the permanent origin and registered its single paid resource (`1/1`) after the consolidation to one Base-mainnet offer. The listing command warned only that the generated OpenAPI lacks `info.contact.email`; that field is optional for indexing but enables ownership/contact customization.

The public registration document is active and advertises the Base Agent endpoint. Its Base registration is the primary identity; the Sepolia registration is retained for historical continuity.

The old `/services/canary402` HTTP offer and its public health/report routes have intentionally been removed. Reports are still retained internally, and the Agent returns the report ID and an evidence-based interpretation in its response.

## Buy an Agent request

The endpoint accepts OpenAI-style chat-completions JSON behind an x402 v2 gate. The buyer wallet needs canonical USDC on Base mainnet; the inbound payment itself does not require the buyer to fund a separate Canary402 wallet.

Keep all private keys outside source control:

```bash
npm install @x402/fetch @x402/evm viem
```

```js
import { x402Client, wrapFetchWithPayment } from "@x402/fetch";
import { registerExactEvmScheme } from "@x402/evm/exact/client";
import { privateKeyToAccount } from "viem/accounts";

const signer = privateKeyToAccount(process.env.EVM_PRIVATE_KEY);
const client = new x402Client();
registerExactEvmScheme(client, { signer });
const fetchWithPayment = wrapFetchWithPayment(fetch, client);

const response = await fetchWithPayment(
  "https://andrei-obol-agent.dvlabs.dev/services/canary402-agent/v1/chat/completions",
  {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      model: "openrouter/auto",
      stream: false,
      messages: [{
        role: "user",
        content: "Review the specification and probe https://seller.example.com/weather. Generate repair templates, but do not make a downstream payment.",
      }],
    }),
  },
);

console.log(response.status, await response.json());
```

An unpaid request should return `402 Payment Required`. Its challenge should advertise:

- network `eip155:8453` (Base mainnet)
- canonical Base USDC
- amount `1000` atomic units (`0.001 USDC`)
- the exact HTTPS Agent URL as its resource

The Agent defaults to **no downstream payment**. Paying to talk to Canary402 does not authorize Canary402 to pay the inspected service.

## Ask Canary402 for an audit

A normal request can combine these duties:

- **Specification review** inspects bounded copies of `/openapi.json`, `/.well-known/agent-registration.json`, and `/skill.md`, then compares them with the requested operation and live 402 metadata.
- **Repair generation** proposes value-free OpenAPI and Bazaar fragments. These are starting points for seller review, not inferred business truth.
- **Probe-only verification** validates reachability and the x402 challenge without signing or spending anything downstream.
- **Paid verification** makes one budget-capped downstream payment, retries the target, and evaluates the delivered response.

Example prompts:

```text
Review the specification and probe https://seller.example.com/services/weather.
Generate repair templates, but do not make a downstream payment.
```

```text
Make a real paid audit of https://seller.example.com/service.
Use Base mainnet and do not spend more than 20000 atomic USDC units.
```

For a real downstream payment, the user must explicitly supply both:

- `payment_network`: `base` or `base-sepolia`
- `max_payment_atomic`: the maximum atomic token amount Canary402 may authorize

For six-decimal USDC, `1000` atomic units is `0.001 USDC`. Canary402 also enforces its deployment-wide cap, currently `20000` atomic units (`0.02 USDC`).

## Report meaning

The Agent summarizes the API's report ID, verdict, score, coverage, specification status, findings, repair artifacts, individual checks, and limitations. Reports may contain these checks:

- `service_discovery`
- `request_contract`
- `reachability`
- `x402_challenge`
- `payment_budget`
- `paid_delivery`
- `task_outcome`

| Verdict | Meaning |
|---|---|
| `PROBE_PASS` | The payment challenge is valid, but no downstream payment was made |
| `PROBE_PASS_WITH_WARNINGS` | The probe worked, but service-contract metadata needs repair |
| `PASS` | Challenge, paid delivery, and requested outcome passed |
| `PASS_WITH_WARNINGS` | Paid delivery worked, but some evaluation was unavailable or uncertain |
| `FAIL` | At least one required check failed |

`score` measures checks that ran. `coverage_percent` shows how much of the full paid flow was exercised, so a probe-only result must be read with its lower coverage.

Stored reports do not contain purchased response bodies or fetched discovery-document bodies. They retain status, latency, content type, captured byte count, SHA-256 digests, payment terms, settlement evidence when present, and a short evaluator reason. Caller-supplied JSON property names can appear in generated repair fragments, so do not use repair generation for confidential request shapes.

## Proven settlement history

Before the current Base-mainnet-only Agent offer was selected, a paid Agent call settled `0.001` Base Sepolia test USDC in [transaction `0x8dd8…f91b`](https://sepolia.basescan.org/tx/0x8dd8842556ea4a4f7a07fbd45d6647cd769370ed326dd921317d93de4026f91b).

Canary402 has also made a downstream payment of `0.02` Base mainnet USDC while auditing another hackathon service; [transaction `0x9eb2…431c`](https://basescan.org/tx/0x9eb237c760b8ac34dcebed5a0722c0db155e4a1d04705e3956841a0d3ff1431c) confirms the transfer.

These are point-in-time tests, not guarantees about either service.

## Run locally

Prerequisites are an Obol Stack v0.13.0 cluster, Docker, Go 1.25+, and a working LiteLLM/OpenRouter configuration.

```bash
make test
make deploy
make smoke
```

`make deploy` builds and imports the local image, applies [deploy/k8s.yaml](deploy/k8s.yaml), and keeps reports on the `canary402-reports` PVC. `make smoke` port-forwards the internal API and verifies its health without publishing it.

Publish or reconcile only the Agent offer:

```bash
make sell-agent
```

This command:

1. creates or updates the Hermes Agent from [agent/objective.md](agent/objective.md);
2. applies the Agent egress and x402 routing resources;
3. reconciles the x402 price to `0.001 USDC` on Base mainnet;
4. activates the offer against the existing Base Agent ID without minting another identity; and
5. verifies the local offer.

Forks using another hostname should replace `andrei-obol-agent.dvlabs.dev` in [deploy/public-tunnel-routes.yaml](deploy/public-tunnel-routes.yaml).

## Direct development requests

The Go API is accessible only inside the cluster or through an operator-created port-forward:

```bash
obol kubectl -n llm port-forward service/canary402 18080:8080
```

Then run a no-spend audit directly:

```bash
curl --noproxy '*' -sS http://127.0.0.1:18080/audit \
  -H 'Content-Type: application/json' \
  --data '{
    "url": "https://seller.example.com/services/weather/current",
    "method": "POST",
    "body": {"city": "Istanbul"},
    "expectation": "The response contains a numeric temperature for Istanbul.",
    "spec_review": true,
    "generate_repairs": true,
    "pay": false
  }'
```

A complete request is also available in [examples/audit-spec-review.json](examples/audit-spec-review.json).

## Safety model

Canary402 fails closed:

- Only public HTTPS targets on port 443 are accepted.
- Loopback, private, link-local, metadata, benchmark, documentation, carrier-grade NAT, IPv4-mapped IPv6, and standard NAT64 translation ranges are blocked.
- DNS is checked again at connection time to reduce rebinding risk.
- Redirects are never followed, especially after payment authorization is attached.
- Request bodies are JSON and capped at 64 KiB; target responses are capped at 1 MiB.
- Response bodies and fetched specification bodies are not persisted.
- Discovery fetches only three fixed same-origin paths, does not follow redirects, and caps each document at 256 KiB.
- Query strings and credentials are removed from stored report URLs.
- Client authentication, cookies, arbitrary headers, and payment headers are not forwarded.
- Downstream payment is opt-in and bounded by both the request cap and `CANARY_MAX_PAYMENT_ATOMIC`.
- Target content is treated as untrusted evidence, including in the semantic-evaluation prompt.
- Objective protocol checks remain separate from the subjective model verdict.

The downstream client supports x402 v2 `exact`/EIP-3009 with canonical USDC on Base (`eip155:8453`) and Base Sepolia (`eip155:84532`). Permit2, arbitrary ERC-20 assets, redirects, asynchronous jobs, custom caller headers, and SSE targets are deliberately unsupported.

## Verification

```bash
make test
make vet
make status
make logs
```

The test suite covers specification discovery, path-template matching, deterministic repair generation, challenge comparison, caps, remote signing, paid retry, settlement evidence, semantic evaluation, persistent reports, IPv4/mapped-IPv6 equivalence, NAT64 blocks, and the invariant that inferred schemas contain no example values.

## Configuration

Internal API runtime:

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | `8080` | HTTP listen port |
| `CANARY_REPORT_DIR` | `/data/reports` | Persistent report directory |
| `CANARY_MAX_PAYMENT_ATOMIC` | `20000` | Absolute downstream per-audit cap |
| `CANARY_ALLOWED_NETWORKS` | `base-sepolia,base` | Allowed downstream networks |
| `CANARY_MAX_CONCURRENT` | `4` | Concurrent audit limit |
| `CANARY_TARGET_TIMEOUT_SECONDS` | `20` | Per-target timeout |
| `CANARY_ALLOW_HTTP` | `false` | Development-only HTTP target support |
| `CANARY_ALLOW_PRIVATE_TARGETS` | `false` | Test-only private target support; never enable publicly |
| `LITELLM_BASE_URL` | in-cluster LiteLLM | OpenAI-compatible evaluator URL |
| `LITELLM_MASTER_KEY` | empty | LiteLLM bearer token; missing means semantic evaluation is disabled |
| `CANARY_MODEL` | `openrouter/auto` | Semantic evaluator model |
| `REMOTE_SIGNER_URL` | in-cluster signer | Obol typed-data signer URL |
| `REMOTE_SIGNER_TOKEN` | empty | Optional signer bearer token |

Agent publication:

| Variable | Default | Meaning |
|---|---|---|
| `CANARY_AGENT_MODEL` | `openrouter/auto` | Hermes Agent model |
| `CANARY_AGENT_NETWORK` | `base` | Inbound x402 payment network |
| `CANARY_AGENT_IDENTITY_NETWORK` | `base` | ERC-8004 identity lookup chain; normally match the payment network |
| `CANARY_AGENT_PRICE` | `0.001` | Per-request USDC price |

LiteLLM authentication comes from the existing `llm/litellm-secrets` Kubernetes secret. Payment signing uses the existing remote signer; its private key is never mounted into Canary402. The signer address is intentionally cached for the process lifetime, so a coordinated signer-key rotation requires restarting Canary402.

## Secrets and source control

Local Obol/OpenRouter and tunnel credentials belong only in `.env`, which Git and Docker ignore. Never commit wallet private keys, remote-signer credentials, `LLM_API_KEY`, or the secret-bearing tunnel command.

```bash
git status --short --ignored
git check-ignore -v .env
```

## Obol and registration notes

Cloudflare terminates TLS before the in-cluster gateway. Obol v0.13.0 otherwise constructs an `http://` challenge resource for an external HTTPS request, so [deploy/public-tunnel-routes.yaml](deploy/public-tunnel-routes.yaml) restores the external scheme and host for the Agent route.

The primary identity is Base Agent ID `59094`. Its [mint transaction](https://basescan.org/tx/0xa23a51b8a6beb357fd798864b2fd1ca0b97a80f7ca2f66b3ef47b25fadb6fcf5) and [agentURI correction](https://basescan.org/tx/0x4727585e079ae2b06f6faa2a96c0aad8c4c17027c1d409d5ed2979f9aebad60f) succeeded. The corrected on-chain URI is `https://andrei-obol-agent.dvlabs.dev/.well-known/agent-registration.json`.

Legacy Base Sepolia Agent ID `8104` remains owned by the same wallet; its [mint transaction](https://sepolia.basescan.org/tx/0xdf47245cdceda5a7849485cb7ab86f00dec6b681636064fe39e73e7ff1c2c17e) also succeeded. Registration is idempotent only while `x402/AgentIdentity` contains the correct per-chain IDs. Do not delete or overwrite that durable record, and always pass the storefront origin—not a service path—to `obol sell register --endpoint`, because v0.13 appends `/.well-known/agent-registration.json` itself.

ERC-8004 identity, an active x402 ServiceOffer, and x402scan discovery are separate states. x402scan previously rejected the shared origin while two Base Sepolia offers were present even though its public checker found both. After consolidation, `obol sell register x402scan` successfully registered the one Base-mainnet Agent resource.
