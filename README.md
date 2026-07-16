# Canary402

Canary402 is the service-contract inspector and mystery shopper for paid agents:

> Before your agent integrates with or pays a service, Canary402 checks the contract and verifies the delivery.

It inspects public service specifications, generates value-free repair templates, validates the live x402 challenge, optionally makes one strictly budgeted payment through the Obol remote signer, evaluates the delivered result through the configured OpenRouter model, and saves a public evidence-backed report.

The original concept is preserved in [idea.md](idea.md).

The hackathon deployment is live at **https://andrei-obol-agent.dvlabs.dev**. Two Base Sepolia offers are on sale for `0.001 USDC` each:

- **Canary402 Agent** — talk to the real Hermes/OpenRouter agent at `POST /services/canary402-agent/v1/chat/completions`.
- **Canary402 API** — call the deterministic audit service directly at `POST /services/canary402/audit`.

The Agent calls the API internally, so using the Agent does not incur a second Canary402 API charge. Health checks, discovery, and completed reports are free.

Both offers share [ERC-8004 Agent ID 8104 on Base Sepolia](https://sepolia.basescan.org/nft/0x8004A818BFB912233c491871b3d84c89A494BD9e/8104). The deployment is intentionally not listed on x402scan.

## Current MVP

The service exposes:

| Route | Obol gate | Purpose |
|---|---|---|
| `POST /services/canary402-agent/v1/chat/completions` | Paid | Ask the Hermes Agent to inspect, repair, probe, or verify a service |
| `POST /services/canary402/audit` | Paid | Inspect specifications, probe a target, and optionally make one downstream payment |
| `GET /services/canary402/reports/{id}` | Free | Retrieve a persisted public report |
| `GET /services/canary402/health` | Free | Readiness and configuration status |
| `GET /openapi.json` | Free | Storefront OpenAPI discovery for the current offers and routes |
| `GET /.well-known/agent-registration.json` | Free | Active ERC-8004 identity and service endpoints |

Specification inspection and repair generation never authorize downstream spend. The full flow still has two separate economic decisions:

1. A caller pays Canary402's `/audit` route through the Obol ServiceOffer.
2. Canary402 pays the audited target only when the request contains `"pay": true`, the challenge has exactly one supported option, and that option is within both spending caps.

Calling Canary402—or requesting a specification review—does not implicitly authorize a downstream payment.

## Talk to the Canary402 Agent

The Agent accepts OpenAI-style chat-completions JSON behind an x402 v2 gate. Ask it for a target audit in natural language:

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

Use a test wallet with Base Sepolia USDC and keep its key outside source control. The Agent defaults to no downstream payment. It will authorize one only when the message explicitly requests it and supplies both a network and maximum atomic amount.

A verified no-spend Agent run produced [report 76b56cd0a830d0779ecb7cdd86259856](https://andrei-obol-agent.dvlabs.dev/services/canary402/reports/76b56cd0a830d0779ecb7cdd86259856). The combined SpecSmith mode is also live: [report 433d427e0fba2283a5166af695b7dea7](https://andrei-obol-agent.dvlabs.dev/services/canary402/reports/433d427e0fba2283a5166af695b7dea7) matched the public Agent operation and x402 resource, validated all three discovery documents and Bazaar schemas, generated repair proposals, and made no downstream payment.

## Use the Canary402 API directly

Canary402 is an HTTP agent service. Give it the paid endpoint you want inspected, the request that endpoint expects, and—when testing delivery—a plain-language success condition.

### 1. Inspect the service and price

Open the [Canary402 landing page](https://andrei-obol-agent.dvlabs.dev), or inspect its machine-readable documents:

```bash
curl -sS https://andrei-obol-agent.dvlabs.dev/openapi.json
curl -sS https://andrei-obol-agent.dvlabs.dev/.well-known/agent-registration.json
curl -sS https://andrei-obol-agent.dvlabs.dev/services/canary402/health
```

A plain unpaid request to `/audit` intentionally returns `402 Payment Required` and a `PAYMENT-REQUIRED` header. It does not run the audit:

```bash
curl -sS -D - -o /dev/null \
  -H 'Content-Type: application/json' \
  --data '{}' \
  https://andrei-obol-agent.dvlabs.dev/services/canary402/audit
```

### 2. Choose an audit mode

- **Specification review** (`"spec_review": true`) fetches bounded copies of `/openapi.json`, `/.well-known/agent-registration.json`, and `/skill.md`, compares them with the requested operation and live 402 metadata, and reports integration gaps.
- **Repair generation** (`"generate_repairs": true`) adds proposed OpenAPI and Bazaar fragments. It requires specification review and never copies example values, but caller-supplied JSON property names can appear in the public report.
- **Probe-only** (`"pay": false`) validates reachability and the x402 payment challenge. It never signs or sends a payment.
- **Verified** (`"pay": true`) performs the probe, selects one supported option within the supplied budget, signs one authorization, retries the target, and judges the delivered response.

The modes compose: a request can inspect specifications, generate repairs, probe the challenge, and—only when `pay` is explicitly true—verify paid delivery. Start without downstream payment. Use verified mode only after reviewing the advertised price and funding the Canary402 wallet on the requested network.

A complete no-spend request is available in [examples/audit-spec-review.json](examples/audit-spec-review.json).

### 3. Submit the paid audit request

Use any x402 v2 client funded with Base Sepolia test USDC. The following Node.js example uses the current official x402 packages; keep the test-wallet private key in an environment variable, never in source.

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
  "https://andrei-obol-agent.dvlabs.dev/services/canary402/audit",
  {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      url: "https://seller.example.com/services/weather/current",
      method: "POST",
      body: { city: "Istanbul" },
      expectation: "The response contains a numeric current temperature for Istanbul.",
      spec_review: true,
      generate_repairs: true,
      pay: false,
    }),
  },
);

console.log(response.status, await response.json());
```

The wrapped client receives the 402 challenge, signs the advertised Base Sepolia authorization, and retries automatically. See Coinbase's [current x402 buyer quickstart](https://docs.cdp.coinbase.com/x402/quickstart-for-buyers) for wallet and language alternatives.

Request fields:

| Field | Required | Meaning |
|---|---:|---|
| `url` | Yes | Full public HTTPS endpoint to audit |
| `method` | No | `GET` by default; `POST` is also supported |
| `body` | No | JSON body for a `POST` request, up to 64 KiB |
| `expectation` | No | Plain-language condition evaluated against a successful response |
| `expected_status` | No | Exact expected status; otherwise any `2xx` is accepted |
| `spec_review` | No | Inspect public OpenAPI, ERC-8004 registration, skill.md, and live x402 metadata |
| `generate_repairs` | With `spec_review` | Publish proposed, value-free OpenAPI and Bazaar fragments; request property names may become public |
| `pay` | No | `false` by default; enables one downstream payment when `true` |
| `max_payment_atomic` | With `pay` | Maximum atomic token amount Canary402 may authorize |
| `payment_network` | Recommended with `pay` | `base` or `base-sepolia` |
| `payment_asset` | For ambiguous offers | Exact token contract to select |

Supplying an `expectation` asks Canary402 to send a bounded excerpt of the target response to the configured OpenRouter model. Do not use semantic evaluation for confidential content.

### 4. Read and share the report

The response contains an `id`, `verdict`, `score`, `coverage_percent`, and evidence-bearing checks. Specification reviews add the first two checks to the existing payment/delivery checks:

- `service_discovery`
- `request_contract`
- `reachability`
- `x402_challenge`
- `payment_budget`
- `paid_delivery`
- `task_outcome`

Possible verdicts:

| Verdict | Meaning |
|---|---|
| `PROBE_PASS` | The payment challenge is valid, but no payment was made |
| `PROBE_PASS_WITH_WARNINGS` | No payment was made; the challenge worked, but public service-contract metadata needs repair |
| `PASS` | Challenge, paid delivery, and requested outcome all passed |
| `PASS_WITH_WARNINGS` | Paid delivery worked, but part of evaluation was unavailable or uncertain |
| `FAIL` | At least one required check failed |

Retrieve the same public report later without another payment:

```bash
curl -sS https://andrei-obol-agent.dvlabs.dev/services/canary402/reports/<report-id>
```

`score` measures the checks that ran; `coverage_percent` shows how much of the full paid flow was actually exercised. A probe-only score must therefore be read together with its lower coverage.

Reports do not contain the purchased response body or fetched discovery-document bodies. They publish status, latency, content type, captured byte count, SHA-256 digests, payment terms, settlement evidence when available, and the evaluator's short reason.

When requested, `spec_review` contains document availability, the matched OpenAPI operation, concrete-schema flags, resource/Bazaar comparisons, structured findings, and proposed repair fragments. Repairs are deterministic starting points, not authoritative service semantics: sellers must review required fields, examples, descriptions, and output schemas before publishing them.

The report's `challenge_transport` shows whether the target used the current `PAYMENT-REQUIRED` header or Obol's JSON-body compatibility path.

## Live paid verification

The deployed service has completed both inbound and downstream x402 settlement tests:

- A paid call to the Canary402 Agent settled `0.001` Base Sepolia test USDC and returned [a 100/100 PASS report](https://andrei-obol-agent.dvlabs.dev/services/canary402/reports/3616014252977a40a71fabcfb460f9c7). The [settlement transaction](https://sepolia.basescan.org/tx/0x8dd8842556ea4a4f7a07fbd45d6647cd769370ed326dd921317d93de4026f91b) succeeded.
- Canary402 paid `0.02` Base mainnet USDC to audit another hackathon participant's CGT service. It returned [a 100/100 PASS report](https://andrei-obol-agent.dvlabs.dev/services/canary402/reports/b19ad52a37a23132d69ee5d4536fb135), and the [settlement transaction](https://basescan.org/tx/0x9eb237c760b8ac34dcebed5a0722c0db155e4a1d04705e3956841a0d3ff1431c) confirms the transfer.

These reports are point-in-time evidence, not a permanent guarantee about either service.

## Run locally

Prerequisites are the already-running Obol Stack, Docker, Go 1.25+, and the configured LiteLLM/OpenRouter model.

```bash
make test
make deploy
make smoke
```

`make deploy` builds `canary402:dev`, imports it into the current Obol k3d cluster, and applies [deploy/k8s.yaml](deploy/k8s.yaml). Reports persist on the `canary402-reports` PVC.

To expose the local API and the real Agent behind separate x402 gates:

```bash
make sell CANARY_HOSTNAME=andrei-obol-agent.dvlabs.dev
make sell-agent
```

The offer uses Base Sepolia, charges `0.001 USDC`, and is available at both:

```text
http://obol.stack:8080/services/canary402/audit
https://andrei-obol-agent.dvlabs.dev/services/canary402/audit
```

The permanent Cloudflare connector is active. `make sell` is idempotent for an existing ServiceOffer. Offer creation uses `--no-register` to avoid minting a duplicate identity; when an existing Base Sepolia identity is present, the script links both offers to it. x402scan listing is a separate operation and is intentionally not enabled for this deployment.

`make sell-agent` creates or updates the Hermes Agent from [agent/objective.md](agent/objective.md), applies its narrowly scoped [egress policy](deploy/agent-egress.yaml), and publishes `/services/canary402-agent` as `type=agent`. It is also idempotent.

The checked-in public route and Agent objective use the hackathon hostname. Forks should replace `andrei-obol-agent.dvlabs.dev` in [deploy/public-tunnel-routes.yaml](deploy/public-tunnel-routes.yaml) and [agent/objective.md](agent/objective.md), then pass the matching `CANARY_HOSTNAME` to `make sell`.

## Direct development requests

Port-forward the upstream to bypass Canary402's inbound payment gate while developing:

```bash
obol kubectl -n llm port-forward service/canary402 18080:8080
```

Run a probe-only audit against a real public x402 endpoint:

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

A paid downstream audit requires an explicit atomic-unit cap:

```json
{
  "url": "https://seller.example.com/services/weather/current",
  "method": "POST",
  "body": {"city": "Istanbul"},
  "expectation": "The response contains a numeric temperature for Istanbul.",
  "pay": true,
  "max_payment_atomic": "1000",
  "payment_network": "base-sepolia"
}
```

For six-decimal USDC, `1000` atomic units is `0.001 USDC`. A paid audit spends real wallet funds if settlement succeeds. Fund and verify the agent wallet deliberately before enabling it.

## Safety model

The public service fails closed:

- Only public HTTPS targets on port 443 are accepted.
- Loopback, private, link-local, metadata, benchmark, documentation, and carrier-grade NAT ranges are blocked, including IPv4-mapped IPv6 literals and the standard NAT64 translation prefixes.
- DNS is resolved and checked again at connection time to reduce DNS-rebinding risk.
- Redirects are never followed, especially after attaching a payment authorization.
- Request bodies are limited to 64 KiB and must be JSON.
- Target responses are limited to 1 MiB. Response bodies are not persisted or published; reports contain only the captured byte count and SHA-256 digest.
- Specification discovery fetches only three fixed same-origin paths, never follows redirects, limits each document to 256 KiB, and stores only metadata and digests.
- Repair generation never copies request or response example values. It can publish caller-supplied JSON property names, so do not enable it for confidential request shapes.
- Generated fragments contain explicit TODO/generic sections and remain proposals until the seller validates them.
- URLs are stored without query strings so query credentials cannot leak into reports.
- Client-supplied authentication and payment headers are not accepted or forwarded.
- Downstream payment is opt-in and bounded by the request cap and `CANARY_MAX_PAYMENT_ATOMIC`.
- The target response is labeled as untrusted data in the semantic-evaluation prompt.
- Objective protocol checks remain separate from the subjective LLM verdict.

The deployment's current hard cap is `20000` atomic USDC units (`0.02 USDC`).

## Supported payment path

The MVP supports x402 v2 `exact` payments using canonical USDC and `eip3009` on:

- Base (`eip155:8453`)
- Base Sepolia (`eip155:84532`)

It understands both current standard headers (`PAYMENT-REQUIRED`, `PAYMENT-SIGNATURE`, and `PAYMENT-RESPONSE`) and the Obol v0.13 compatibility flow (JSON 402 body, `X-PAYMENT`, and `X-PAYMENT-RESPONSE`). Paid authorizations always use the transport advertised by the target's challenge.

Unsupported or ambiguous offers are reported without signing anything. Permit2, arbitrary ERC-20 assets, asynchronous job polling, custom caller headers, and agent-style SSE endpoints are deliberately outside the first version.

Specification review currently analyzes JSON OpenAPI 3.x documents at `/openapi.json`, the standard ERC-8004 registration path, `/skill.md`, exact or templated OpenAPI paths, JSON request bodies, successful JSON responses, challenge resource URLs, and Bazaar metadata. It deliberately does not follow external `$ref` documents, parse YAML OpenAPI, scrape arbitrary HTML links, or infer business semantics/required fields.

## Verification

```bash
make test
make vet
make status
make logs
```

The unit suite includes a complete simulated flow: specification discovery, path-template matching, deterministic repair generation, challenge comparison, cap enforcement, remote-signature request, x402 envelope, paid retry, settlement evidence, semantic evaluation, and persisted reports. Generative and fuzz tests enforce deterministic, bounded parsing, equivalent IPv4/mapped-IPv6 safety classification, and the invariant that inferred schemas contain no example values. The live deployment has also completed real Base Sepolia and Base mainnet settlements, linked above.

## Runtime configuration

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | `8080` | Canary402 HTTP listen port |
| `CANARY_REPORT_DIR` | `/data/reports` | Persistent report directory |
| `CANARY_MAX_PAYMENT_ATOMIC` | `20000` | Absolute per-audit downstream spend cap |
| `CANARY_ALLOWED_NETWORKS` | `base-sepolia,base` | Allowed payment networks |
| `CANARY_MAX_CONCURRENT` | `4` | Concurrent audit limit |
| `CANARY_TARGET_TIMEOUT_SECONDS` | `20` | Per-target request timeout |
| `CANARY_ALLOW_HTTP` | `false` | Development-only HTTP target support |
| `CANARY_ALLOW_PRIVATE_TARGETS` | `false` | Test-only private target support; never enable publicly |
| `LITELLM_BASE_URL` | in-cluster LiteLLM | OpenAI-compatible semantic-evaluator URL |
| `LITELLM_MASTER_KEY` | empty | LiteLLM bearer token; when absent, semantic evaluation is disabled |
| `CANARY_MODEL` | `openrouter/auto` | LiteLLM model used for semantic judgment |
| `REMOTE_SIGNER_URL` | in-cluster remote signer | Obol typed-data signer URL |
| `REMOTE_SIGNER_TOKEN` | empty | Optional remote-signer bearer token |

## Publishing configuration

| Variable | Default | Meaning |
|---|---|---|
| `CANARY_HOSTNAME` | empty | Permanent shared hostname passed to `make sell` |
| `CANARY_SELL_NETWORK` | `base-sepolia` | Inbound sale network used when creating the offer |
| `CANARY_SELL_PRICE` | `0.001` | Inbound per-request USDC price used when creating the offer |
| `CANARY_AGENT_MODEL` | `openrouter/auto` | Model pinned to the Hermes Agent wrapper |
| `CANARY_AGENT_NETWORK` | `base-sepolia` | Agent offer's inbound payment network |
| `CANARY_AGENT_PRICE` | `0.001` | Agent offer's per-request USDC price |

LiteLLM authentication comes from the existing `llm/litellm-secrets` Kubernetes secret. If that token is absent, Canary402 remains available for deterministic audits and reports `semantic_evaluation: false` from `/health`; expectations then produce a warning instead of an invented judgment. A semantic result marked passed with a score below 50 is likewise downgraded to a warning rather than producing a clean PASS.

Payment signing uses the existing remote signer service; private keys are never mounted into Canary402. The signer address is intentionally pinned for the process lifetime because it is the funded, registered agent identity. After a coordinated remote-signer key rotation, restart Canary402 so it loads the new address.

## Secrets and source control

Local Obol/OpenRouter and tunnel credentials belong only in `.env`, which is ignored by both Git and Docker. Do not commit wallet private keys, remote-signer credentials, `LLM_API_KEY`, or the secret-bearing tunnel run command. The manifests contain only secret references and public service addresses. Verify ignored files before every release with:

```bash
git status --short --ignored
git check-ignore -v .env
```

## Deployment status

Verified on 16 July 2026:

- Permanent Cloudflare tunnel active with four connected connectors.
- The dashboard catalog lists both `llm/canary402` and `agent-canary402/canary402` as Ready offers.
- Both offers share Base Sepolia ERC-8004 Agent ID `8104`.
- A direct authenticated Agent run used `openrouter/auto`, invoked the Canary402 API, and returned a real public probe report.
- Landing page, storefront OpenAPI discovery, and the Agent registration document return `200` publicly.
- Free `/services/canary402/health` and `/services/canary402/reports/{id}` routes return `200` publicly.
- Unpaid requests to both paid endpoints return x402 v2 `402` challenges.
- The deployed `0.2.1-dev` service reports `semantic_evaluation: true`, `spec_review: true`, and `repair_generation: true`; `openrouter/auto` has answered a live evaluation request.
- The live API rejects an IPv4-mapped CGNAT target with HTTP 400 before connecting or authorizing payment; mapped-address equivalence and standard NAT64 blocks are covered by regression and fuzz tests.
- A live SpecSmith review returned `PROBE_PASS`, score 100, and `spec_review.status: READY` after matching the public Agent's OpenAPI operation, ERC-8004 registration, skill document, x402 resource, and Bazaar schemas. It generated deterministic repair proposals with `payment.attempted: false`; see [report 433d427e0fba2283a5166af695b7dea7](https://andrei-obol-agent.dvlabs.dev/services/canary402/reports/433d427e0fba2283a5166af695b7dea7).
- A public probe report is available at [report 436e24422caa55b08f50b049a41c15fe](https://andrei-obol-agent.dvlabs.dev/services/canary402/reports/436e24422caa55b08f50b049a41c15fe).
- Real inbound and downstream paid settlements have succeeded, as documented in [Live paid verification](#live-paid-verification).

x402scan listing is intentionally not active. Do not register the localhost origin or a temporary quick-tunnel URL.

## Obol v0.13 publishing notes

The publish script deliberately uses only `--rps`. In v0.13.0, combining `--rps` with `--max-in-flight` creates a single multi-type Traefik Middleware that Traefik 3.6 rejects, even while the ServiceOffer incorrectly reports Ready. Canary402 still enforces its own concurrency cap.

`obol tunnel setup` created a root storefront route. Dedicating the hostname to the HTTP offer later caused URL rewriting and an x402 challenge whose resource did not match the URL requested by the buyer. The publish script therefore keeps the permanent hostname on the shared storefront, removes a stale dedicated hostname from the offer, and uses canonical `/services/...` routes.

Cloudflare terminates TLS before the in-cluster HTTP gateway, so Obol v0.13 otherwise builds an `http://` challenge resource for an external HTTPS request. [deploy/public-tunnel-routes.yaml](deploy/public-tunnel-routes.yaml) restores the external scheme and host before the request reaches the verifier.

The API and Agent ServiceOffers share the name `canary402` in different namespaces. v0.13.0 creates its x402 `ReferenceGrant` using only that name, so publishing the Agent overwrote the API's grant and caused public HTTP 500 responses while both offers still reported Ready. [deploy/x402-llm-reference-grant.yaml](deploy/x402-llm-reference-grant.yaml) grants both offer namespaces access to `x402-verifier` and `obol-skill-md`. Both publish scripts apply it idempotently.

## ERC-8004 registration

Being on sale and being registered are separate. Canary402 is on sale through x402 and registered as [Base Sepolia ERC-8004 Agent ID 8104](https://sepolia.basescan.org/nft/0x8004A818BFB912233c491871b3d84c89A494BD9e/8104). The [mint transaction](https://sepolia.basescan.org/tx/0xdf47245cdceda5a7849485cb7ab86f00dec6b681636064fe39e73e7ff1c2c17e) succeeded. Do not rerun identity registration for the same deployment because it could mint a duplicate Agent ID.

The remote signer controls the agent wallet. Its private key is intentionally unavailable to this service and must never be copied into the repository.

`obol sell register x402scan` is a different external discovery submission. It was attempted, but the crawler rejected the origin despite live 402 resources; this deployment is intentionally not listed there.
