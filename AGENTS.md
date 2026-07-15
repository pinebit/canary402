# Canary402 Agent Guide

This file is the operational context for coding agents working in the Canary402 repository.

## Goal

Build and publish **Canary402**, the mystery shopper for paid agents:

> Before your agent pays a new service, Canary402 pays it once and tells you whether it actually worked.

The concept and product rationale are in `idea.md`. The current implementation is a deterministic Go audit API with a narrowly scoped LLM evaluator. It is intentionally not a free-form chat wrapper.

## Repository state

- This project is prepared as a public Git repository; keep all project files and new documentation under the repository root.
- Preserve the user's `.env`; never rewrite it as part of normal development.
- The public hostname is intentionally documented; the connector token and command are never checked into project files.
- `.gitignore` must keep `.env`, `.env.*`, build output, coverage output, and logs out of commits while retaining `cmd/canary402/` source.

## Secret-handling rules

The root `.env` contains secrets. Never print it, dump it, commit it, interpolate its values into logs, or copy its values into source/manifests.

Known variable names:

- `LLM_API_KEY`: OpenRouter key used during model setup.
- `TUNNEL_USERNAME`: the public hackathon hostname (`Andrei-obol-agent.dvlabs.dev`; Obol normalizes it to lowercase).
- `TUNNEL_RUN`: a Cloudflare connector run command containing a secret token.

Verify presence with `rg -q '^NAME=.+$' .env`; do not use commands that print the matching line. When tunnel setup is authorized, source `.env` without tracing and pass the connector command directly to the supported Obol CLI. Never echo it or manually print the extracted token.

The cluster already has a namespaced `llm/litellm-secrets` secret. Canary402 references its `LITELLM_MASTER_KEY` key using `secretKeyRef`; it does not copy a token into the manifest. Do not inspect Kubernetes secret `.data`, `stringData`, or YAML output. Capture `obol model token` silently if it is ever required.

The remote signer owns the wallet key. Canary402 only calls its typed-data signing API. Never mount, copy, display, or back up the private key during ordinary development.

## Local Obol Stack

- Obol release: `v0.13.0`
- Obol commit: `2b17616`
- Stack ID: `peaceful-starling`
- k3d cluster: `obol-stack-peaceful-starling`
- kubectl context: `k3d-obol-stack-peaceful-starling`
- Base dashboard: `http://obol.stack` and `http://obol.stack:8080`
- `/etc/hosts` already contains `127.0.0.1 obol.stack`.
- Docker and all base Stack workloads are running.
- LiteLLM is configured for `openrouter/auto` through OpenRouter and has been verified end-to-end.
- The primary Hermes instance is `hermes/obol-agent` in namespace `hermes-obol-agent`.
- The public wallet address may be obtained with:

  ```bash
  obol agent wallet address obol-agent --runtime hermes --output json
  ```

  A wallet address is public; wallet keys and signer passwords are not.

## Current Canary402 state

- Go module: `canary402`
- Image: `canary402:dev`
- Kubernetes namespace: `llm`
- Deployment and Service: `canary402`
- Reports PVC: `canary402-reports`
- The deployment is live and Ready in the local cluster.
- Live `/health` confirmed `semantic_evaluation: true`.
- A live audit of `https://example.com/` confirmed the request path and an actual response from `openrouter/auto`. The report correctly failed the x402 challenge check and passed the semantic expectation.
- A live probe of a current-header x402 endpoint produced report `436e24422caa55b08f50b049a41c15fe`: `PROBE_PASS`, score 100, coverage 40%, without payment.
- The permanent hostname is `https://andrei-obol-agent.dvlabs.dev`; the Cloudflare-managed tunnel is active with four connectors.
- The `llm/canary402` ServiceOffer is Ready on Base Sepolia at `0.001 USDC` per request, with canonical public route `/services/canary402/audit`.
- The real Agent CR `agent-canary402/canary402` is Ready, uses Hermes with pinned model `openrouter/auto`, and follows `agent/objective.md`.
- The Agent's isolation policy is extended only to `llm` pods labeled `app.kubernetes.io/name=canary402` on TCP 8080 by `deploy/agent-egress.yaml`.
- The `agent-canary402/canary402` ServiceOffer is Ready as `type=agent` at `/services/canary402-agent`, priced at `0.001 USDC` on Base Sepolia.
- A direct authenticated Agent run invoked the Canary402 API successfully and returned `PROBE_PASS`, score 100, coverage 40%, report `76b56cd0a830d0779ecb7cdd86259856`.
- Public `/`, `/openapi.json`, `/.well-known/agent-registration.json`, shared health/report routes, and both paid endpoints have been verified. Unpaid calls to both paid endpoints return x402 v2 `402` challenges. `/.well-known/x402` is not exposed on the shared v0.13 storefront and currently returns `404`.
- Public unpaid Agent requests at `/services/canary402-agent/v1/chat/completions` return a valid x402 v2 `402` challenge whose metadata advertises Hermes and `openrouter/auto`.
- Both offers share ERC-8004 Agent ID `8104` on Base Sepolia. The mint transaction is `0xdf47245cdceda5a7849485cb7ab86f00dec6b681636064fe39e73e7ff1c2c17e`.
- A real paid call to the public Agent succeeded for `0.001` Base Sepolia test USDC and produced report `3616014252977a40a71fabcfb460f9c7`; settlement transaction: `0x8dd8842556ea4a4f7a07fbd45d6647cd769370ed326dd921317d93de4026f91b`.
- A real downstream audit paid another participant's CGT service `0.02` Base mainnet USDC and produced report `b19ad52a37a23132d69ee5d4536fb135`, `PASS`, score/coverage 100; settlement transaction: `0x9eb237c760b8ac34dcebed5a0722c0db155e4a1d04705e3956841a0d3ff1431c`.
- The operator hard cap is now `20000` atomic USDC (`0.02 USDC`). Every paid downstream audit still requires explicit target, network, and caller cap authorization.
- x402scan registration was attempted after the endpoint worked but returned a generic “no valid paid resources” result. The user chose to stop pursuing that listing; do not retry unless they explicitly reopen it.

## Architecture

Important files:

- `cmd/canary402/main.go`: process wiring and graceful shutdown.
- `internal/canary/auditor.go`: audit state machine and score calculation.
- `internal/canary/safeclient.go`: URL validation, SSRF controls, DNS validation, and no-redirect HTTP transport.
- `internal/canary/payment.go`: x402 v2 parsing, payment selection, EIP-3009 typed data, remote-signer calls, and payment envelope encoding.
- `internal/canary/semantic.go`: LiteLLM/OpenRouter outcome evaluation with untrusted-response boundaries.
- `internal/canary/store.go`: atomic JSON report persistence.
- `internal/canary/httpapi.go`: HTTP API and local OpenAPI document.
- `deploy/k8s.yaml`: hardened Kubernetes Deployment, Service, and reports PVC.
- `deploy/agent-egress.yaml`: narrowly scoped egress from the Agent namespace to the Canary402 API only.
- `deploy/x402-llm-reference-grant.yaml`: additive workaround for the same-named cross-namespace ServiceOffer grant collision.
- `agent/objective.md`: Hermes Agent objective, tool contract, and downstream-payment safety rules.
- `scripts/deploy-local.sh`: image build/import and cluster deployment.
- `scripts/publish-local.sh`: idempotent Obol ServiceOffer publication on the shared permanent hostname; avoids duplicate ERC-8004 minting, reuses an existing identity, and applies v0.13 routing workarounds.
- `scripts/publish-agent.sh`: idempotently creates/updates the Agent, applies egress, publishes the `type=agent` offer, and reuses an existing shared identity.
- `scripts/smoke-local.sh`: temporary port-forward health and offer smoke test.
- `README.md`: operator and developer instructions.

Runtime flow:

1. Caller pays the Obol-gated public `POST /services/canary402/audit` route, or calls upstream `POST /audit` directly during local development.
2. Canary402 validates the requested target before connecting.
3. It sends an unpaid request and expects an x402 v2 challenge in the current header format or Obol's JSON-body compatibility format.
4. If `pay` is false, it publishes a probe-only report.
5. If `pay` is true, it requires `max_payment_atomic`, checks the request cap and hard operator cap, and requires one unambiguous supported payment option.
6. It asks the existing Obol remote signer for one EIP-712 signature.
7. It retries the exact request with `PAYMENT-SIGNATURE` or compatibility `X-PAYMENT`, matching the target's challenge transport, and refuses redirects.
8. It records response and settlement evidence.
9. It asks `openrouter/auto` through LiteLLM to judge only the user-supplied expectation against the untrusted response.
10. It persists a public report and returns its ID.

Agent wrapper flow:

1. A buyer pays the `type=agent` chat-completions route.
2. Hermes interprets the natural-language request under `agent/objective.md`.
3. Its terminal tool calls `http://canary402.llm.svc.cluster.local:8080/audit` directly; this internal call does not traverse or pay the external HTTP ServiceOffer.
4. The NetworkPolicy permits only that cross-namespace destination/port in addition to the CRD controller's standard Agent egress.
5. Hermes summarizes the deterministic report and links its free public report URL.

The Agent and raw API are separate paid products. A user pays only the offer they call. Neither inbound payment authorizes a downstream target payment unless `pay: true` plus an explicit network and atomic cap reach the deterministic API.

## API

- `POST /audit`: paid when published through Obol; direct upstream access is only for local development.
- `GET /reports/{id}`: free public report.
- `GET /health`: free health endpoint.
- `GET /openapi.json`: direct local API description; the public shared origin exposes Obol's storefront description and discovery bundle.

The caller's inbound payment and Canary402's downstream payment are separate. A paid call to Canary402 does not authorize a downstream purchase unless the JSON request explicitly sets `pay: true` and supplies a cap.

## Payment support and hard limits

The MVP supports:

- x402 v2
- `exact` scheme
- EIP-3009 `TransferWithAuthorization`
- Current standard header transport: `PAYMENT-REQUIRED` → `PAYMENT-SIGNATURE` → `PAYMENT-RESPONSE`
- Obol v0.13 compatibility transport: JSON 402 body → `X-PAYMENT` → `X-PAYMENT-RESPONSE`
- canonical USDC on Base (`eip155:8453`)
- canonical USDC on Base Sepolia (`eip155:84532`)
- JSON `GET` and `POST` targets

Current operator cap: `20000` atomic USDC units, equal to `0.02 USDC` for six-decimal USDC.

The MVP deliberately rejects:

- Permit2
- arbitrary ERC-20 contracts
- ambiguous multi-currency matches
- unsupported networks
- payment above either the caller cap or operator cap
- redirects after either unpaid or paid requests
- custom caller auth/payment headers
- private or non-HTTPS target URLs

Do not perform a real paid audit without explicit user approval for the target, network, and maximum amount. Tests simulate the payment protocol but do not settle on-chain.

## Security invariants

Preserve these invariants in every change:

- Production accepts only public HTTPS targets on port 443.
- Resolve and validate DNS again at connection time.
- Do not use environment proxy settings for target calls.
- Block loopback, private, link-local, metadata, CGNAT, benchmark, multicast, documentation, and unspecified addresses.
- Never follow redirects carrying a payment authorization.
- Never forward caller-supplied `Authorization`, `Cookie`, `Host`, `X-PAYMENT`, or arbitrary headers.
- Limit request and response sizes.
- Strip query strings and credentials from public reports.
- Do not persist or publish target response bodies. Store only captured byte count and SHA-256 digest.
- Treat target content as untrusted data during semantic evaluation.
- Supplying an expectation sends a bounded response excerpt to the configured OpenRouter model; do not semantically evaluate confidential content.
- Keep deterministic protocol checks separate from LLM judgment.
- Label results as point-in-time evidence, not permanent guarantees.
- Preserve the read-only container filesystem, dropped Linux capabilities, non-root user, and disabled service-account token.

If private targets or HTTP are needed in tests, instantiate `TargetPolicy{AllowHTTP: true, AllowPrivateTargets: true}` in the test only. Never enable `CANARY_ALLOW_PRIVATE_TARGETS` in a public deployment.

## Development commands

The sandbox may not allow the default Go build cache or loopback listeners. Prefer:

```bash
GOCACHE=/tmp/canary402-gocache go test ./...
GOCACHE=/tmp/canary402-gocache go vet ./...
```

The `httptest` integration tests require loopback socket permission. If a managed sandbox blocks them, rerun the same test command with the appropriate execution approval; do not weaken or delete the tests.

Standard workflow:

```bash
make test
make vet
docker build --build-arg VERSION=0.1.0-dev -t canary402:dev .
k3d image import canary402:dev --cluster obol-stack-peaceful-starling
obol kubectl apply -f deploy/k8s.yaml
obol kubectl -n llm rollout status deployment/canary402 --timeout=120s
./scripts/smoke-local.sh
```

Do not use destructive Kubernetes, Docker, Obol, or Git commands unless explicitly authorized. Preserve the reports PVC across normal redeployments.

## Publishing and tunnel workflow

The permanent tunnel and ServiceOffer are already active. Reproduce or verify publication with:

```bash
make sell CANARY_HOSTNAME=andrei-obol-agent.dvlabs.dev
make sell-agent
```

The first command publishes the deterministic HTTP API at the shared-origin endpoint `/services/canary402/audit`. The second creates/updates the real Agent and publishes `/services/canary402-agent/v1/chat/completions` on the same origin.

`obol sell http` automatically starts a quick public tunnel when the tunnel is dormant. If a future environment should remain local-only, do not run `make sell`; use a direct port-forward instead.

The dashboard-managed Cloudflare public hostname routes to `http://traefik.traefik.svc.cluster.local:80`. Tunnel setup used the supported connector-token path from `.env`; never reveal or retype that token into documentation.

Important v0.13 workarounds:

1. Do not combine `--max-in-flight` and `--rps`. The controller puts both limit types in one Traefik Middleware, which Traefik 3.6.6 rejects. The ServiceOffer and HTTPRoute still report Ready/Accepted, so verify Traefik logs and the actual 402 response. The script keeps `--rps 2`; the application itself enforces four concurrent audits.
2. `obol tunnel setup` created `traefik/tunnel-storefront` as a catch-all for the hostname. A dedicated offer hostname caused request rewrites and mismatched challenge resource URLs, so the scripts deliberately retain the storefront at `/`, clear `spec.hostname`, and use canonical shared paths.
3. Cloudflare terminates TLS before the in-cluster gateway. `deploy/public-tunnel-routes.yaml` restores `X-Forwarded-Proto: https` and the public host before requests reach the verifier, preventing `http://` challenge resource URLs.
4. `--description` cannot be used with `--no-register` in v0.13 even though CLI help says descriptions also feed the payment page/storefront. The scripts patch a concise registration description only after an existing Agent ID is available.

x402scan listing is intentionally inactive after its crawler rejected the live paid resources. Do not retry it unless the user explicitly changes that decision. Never register `obol.stack`, localhost, or a temporary `trycloudflare.com` URL.

## Obol v0.13 integration details

- `obol sell http --namespace llm` sets both the ServiceOffer namespace and upstream service namespace.
- Declaring any `--route` makes the route table exhaustive. Every intended route must be declared.
- More-specific routes beat wildcards.
- Use free gates for `/health` and `/reports/*`; use a paid gate for `/audit`.
- The shared public origin exposes `/openapi.json`, `/.well-known/agent-registration.json`, and canonical service paths under `/services/...`. It does not expose `/.well-known/x402`; the OpenAPI document is the working discovery surface.
- `--no-register` skips ERC-8004 registration but does not stop the CLI from activating a quick tunnel.
- Offers created with `--no-register` can later share an existing `AgentIdentity`; activation and minting are distinct operations in v0.13.
- In v0.13, use a single gateway limit type per offer; two limit flags create a Traefik-invalid multi-type Middleware.
- The Obol verifier exposes authenticated `X-Payment-Payer` to the upstream on paid routes. Do not trust a client-supplied copy when bypassing the gate locally.

## Obol documentation and product feedback already found

Preserve these findings for the final hackathon feedback:

1. The Quickstart still describes OpenClaw as the primary/default agent and `llmspy`; v0.13 defaults to Hermes and LiteLLM, with OpenClaw as an alternative.
2. The versioned Quickstart example still shows the obsolete `v0.1.0` release.
3. Interrupting or failing the installer's sudo `/etc/hosts` step aborts before the normal start prompt, and the recovery path is not documented.
4. `obol stack up` and agent setup repeatedly attempt privileged host updates. `OBOL_NONINTERACTIVE=true` avoids the prompt with a warning, but that recovery/control is not documented.
5. The Ollama prompt does not clearly explain that declining or having no model can skip the default Hermes deployment.
6. Cloudflared is installed but dormant until the first sell operation; the resulting public-exposure behavior deserves a clearer warning near `obol sell`.
7. Raw localhost requests return Traefik 404 because routing depends on the `Host` header. The docs should explain `obol.stack` and the `:8080` fallback explicitly.
8. `obol model setup` help suggests provider-specific environment variable support, but `OPENROUTER_API_KEY` was not detected in this setup. The generic `LLM_API_KEY` worked.
9. Interrupting sudo during the initial Hermes setup can create a partial deployment directory. A retry then sees the directory and skips missing generation steps, causing a missing `values-remote-signer.yaml`. The supported recovery was a force onboarding.
10. The default Hermes dashboard redirect returned HTTP 500 because its upstream `BasicAuthProvider` raised `NotImplementedError` for an OAuth-style redirect. The main Obol dashboard remained healthy.
11. Hermes logged warnings about a network-accessible API server with an unsandboxed local terminal and invalid API-key health probes even though the pods were Ready.
12. `obol agent wallet address ... --output json` emitted the plain address on stderr in this installation, so normal stdout capture returned empty. The output flag/help behavior is misleading for automation.
13. `obol sell http --no-register --description ...` fails because description is treated as registration-specific, while help explicitly says it is also used on the payment page and storefront.
14. Supplying both documented limit flags (`--max-in-flight` and `--rps`) produces one Traefik Middleware containing two middleware types. Traefik rejects every protected router with “multi-types middleware not supported,” but ServiceOffer Ready and HTTPRoute Accepted/ResolvedRefs remain true. This needs validation, separate middleware resources, and a surfaced failure condition.
15. Configuring a permanent tunnel before dedicating its only hostname creates a `tunnel-storefront` catch-all. Later binding that hostname to a ServiceOffer says the storefront is skipped but leaves the old catch-all, which shadows all non-discovery offer routes. The hostname binding flow should remove or narrow the stale route automatically.
16. `obol sell test` initially returned a large Next.js 404 because of the two routing problems above. The test became useful only after inspecting Traefik logs; status commands alone incorrectly claimed the offer was healthy.
17. `obol agent new --help` exposes `--runtime`, but the named CRD creation path rejects the flag and instructs callers to omit it. Help should separate legacy and CRD flags.
18. `obol agent auth canary402 --output json` failed for the CRD-declared Agent, even though the command accepts an instance name and does not state it is legacy-only. Testing required using the key already injected into the pod without printing it.
19. CRD Agents receive a private-range-denying `agent-isolation` NetworkPolicy. This is a good default, but neither the CLI nor current docs explain how to declare narrow cross-namespace tool dependencies; Canary402 initially failed with connection refused until a second additive NetworkPolicy allowed the API pod/port.
20. The Agent ServiceOffer reports `UpstreamHealthy=True` when the health probe returns HTTP 404. A 404 should not be labeled Healthy without an explicit documented relaxed-health policy.
21. `obol sell list` leaves the Agent MODEL column empty even though the ServiceOffer status and storefront catalog correctly contain `openrouter/auto`.
22. A `type=agent` offer defaults its storefront description to the entire Agent objective, which can expose internal tool addresses and operational rules. The CLI should support a separate public description while `--no-register` is active.
23. ReferenceGrant resource names use only the ServiceOffer name, not namespace plus name. Creating `agent-canary402/canary402` overwrote the grant for `llm/canary402`, causing HTTP 500 on the API while both offers stayed Ready. The controller must use a namespace-qualified resource name or merge allowed source namespaces.
24. Giving the HTTP offer a dedicated hostname caused the public `/audit` request to be rewritten to a shared internal path before the verifier built its challenge. The challenge resource no longer matched the buyer's requested URL, so standard clients could not validate or retry it.
25. Behind the permanent Cloudflare tunnel, the verifier generated `http://` resource URLs for externally HTTPS requests because the external scheme was not preserved. Hostname-specific routes had to set `X-Forwarded-Proto` and `X-Forwarded-Host` explicitly.
26. The generated shared storefront rendered malformed service links shaped like `http://host/https://...`. Public service navigation should use origin-relative canonical paths.
27. Visiting a paid service path directly shows a `402` challenge, not its service description. The relationship between the root storefront, discovery documents, service description, and paid operation needs clearer dashboard and docs guidance.
28. The agent wallet is remote-signer managed, but the faucet flow asks the user to connect an ordinary wallet. Documentation should explain how to fund the public remote-signer address without exposing or expecting access to its private key.
29. ERC-8004 registration minted Agent ID 8104 successfully, but a follow-up metadata transaction reverted with custom error `0x7e273289`. Registration should report partial success clearly and explain whether metadata reconciliation is safe to retry.
30. Offers created with `--no-register` remained deactivated after the shared identity was minted. They had to be patched to enable registration, even though status and dashboard language suggested registration would attach automatically.
31. `obol sell status` shows Agent ID 8104 and `Registered=True` but leaves `Registration Tx: (not set)` even though the mint transaction is known. The status object should retain or recover the transaction hash.
32. The shared registration owner/status took time and manual reconciliation to appear consistently across both namespaces. The dashboard should distinguish identity minting, offer activation, and shared registration reconciliation.
33. x402scan rejected the permanent origin with a generic “no valid paid resources” result even after the official AgentCash discovery client found both resources and direct unpaid calls returned valid live 402 challenges. The error needs endpoint-level probe diagnostics.
34. Obol has no obvious generic balance/faucet/buyer command for external paid services; `obol buy` is oriented around model inference. Funding and testing a seller wallet therefore required external chain tools and protocol-specific code.
35. Dashboard terminology—service on sale, Agent offer, ERC-8004 registration, and x402scan listing—reads like one state machine even though these are separate layers. The product should name and display them independently.
36. Another participant's discovery/OpenAPI document exposed paid endpoints but no concrete request-body schema, forcing request-shape inference from its UI. Discovery validation should require or strongly encourage usable schemas and examples.
37. That participant's live 402 challenge also advertised an `http://` resource URL behind an HTTPS tunnel, showing the forwarded-scheme issue affects other deployments, not only Canary402.

## Registration state

- Base Sepolia ERC-8004 Agent ID: `8104`.
- Registry: `0x8004A818BFB912233c491871b3d84c89A494BD9e`.
- Mint transaction: `0xdf47245cdceda5a7849485cb7ab86f00dec6b681636064fe39e73e7ff1c2c17e`.
- Both ServiceOffers report `Registered=True` and share Agent ID 8104; `obol sell status` still displays `Registration Tx: (not set)`.
- `obol sell register` is an on-chain identity operation, separate from having Ready x402 offers. Do not rerun it: that could mint a duplicate identity.
- The signing/payout wallet address is public, but its private key remains exclusively in the Obol remote signer. Never try to export it.
- x402scan is a separate external listing. Its registration attempt failed despite live paid resources, and the user chose to ignore it. Do not retry without new explicit direction.

## Current next steps

1. Keep documentation, tests, and source-control secret hygiene current as the project is published.
2. Collect feedback from other hackathon participants using the public URL.
3. Require explicit target, network, and atomic cap approval before every new real downstream payment.
4. Add Permit2, async job support, response-schema hints, or private buyer-side result capture only after the basic paid flow remains stable.
5. Leave x402scan alone unless the user explicitly reopens that work.
