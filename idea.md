# Canary402

## The mystery shopper for paid agents

> Before your agent pays a new service, Canary402 pays it once and tells you whether it actually worked.

## Problem

x402 discovery can tell an agent that a paid service exists, but not whether the complete purchasing experience currently works or whether the delivered result is useful. Buyers must otherwise risk their own money to learn whether an endpoint has a valid payment challenge, accepts payment, responds reliably, and fulfills its advertised task.

## Concept

Canary402 is an agent that performs a real, paid, end-to-end test of another x402 service. A requester supplies an endpoint and a simple expectation, such as:

> Test this weather agent with Istanbul and verify that it returns a temperature.

Canary402:

1. Discovers the endpoint's API and payment specification.
2. Sends an unpaid request and validates the `402 Payment Required` response.
3. Checks the requested price against a strict spending limit.
4. Makes one real paid request using its Obol wallet.
5. Validates the response schema and requested outcome.
6. Produces a timestamped, machine-readable and human-readable report.

An example report might contain:

```text
Canary402 verdict: PASS — 87/100

Payment:       $0.01 USDC settled
Latency:       1.8 seconds
Schema:        Valid
Task outcome:  Correct
Reliability:   2/3 attempts
Warning:       Discovery document omits an example response
```

## Core value proposition

Canary402 does not merely check whether a URL is online. It spends real money, exercises the protected service end-to-end, evaluates the delivered result, and produces evidence.

The report should distinguish between:

- Endpoint and network failures
- Invalid or incomplete x402 challenges
- Payment or settlement failures
- Response-schema failures
- Incorrect or low-quality task results
- Documentation and discovery-specification gaps

## Public product surface

Potential endpoints:

```text
POST /audit             Paid: run one real endpoint test
POST /compare           Paid: compare several agents on the same task
GET  /reports/{domain}  Free: retrieve a public report
GET  /leaderboard       Free: browse tested agent services
```

Every audited service receives a shareable report and badge:

```text
✓ Tested by Canary402
87/100 · checked 14 minutes ago
```

The public report is the distribution mechanism; the paid audit is the x402 product.

## Hackathon fit

### Best Concept

Canary402 is trust and reliability infrastructure for the agent economy rather than another general-purpose chatbot. It uses an agent to assess other agents through real economic interactions.

### Best Execution

A narrow, polished audit workflow is realistic for the hackathon. It can demonstrate an Obol-hosted agent, an agent wallet, x402 buying and selling, LLM evaluation, public deployment, and x402scan listing in one coherent flow.

### Most Non-Obol Users

x402 developers have a reason to test their services and share the resulting report. A useful badge and public leaderboard can create a provider-driven distribution loop.

### Highest Volume

Each paid audit can create both an inbound payment to Canary402 and one or more outbound payments to tested services. Recurring monitoring and comparison audits could create continued legitimate activity.

## Hackathon community loop

Canary402 can audit every participant's submission:

1. A participant submits an endpoint.
2. Canary402 buys and evaluates the service.
3. The participant receives actionable failure and documentation feedback.
4. The participant fixes the issue and requests another audit.
5. The successful report is shared publicly.

This directly supports the hackathon goals of testing other submissions and gathering Stack, x402, and documentation feedback.

## Differentiation

Canary402 should not be positioned as another x402 directory, static access checker, reputation score, or generic failure classifier. x402scan already handles service discovery and activity reporting, and existing services offer several static checks.

The defining feature is a real post-payment mystery-shopping test with task-specific validation and an evidence-backed report.

## Initial scope

The first version should:

- Support a single public HTTPS JSON `GET` or `POST` endpoint.
- Require the requester to state the expected result.
- Validate the unpaid challenge before authorizing payment.
- Enforce a maximum downstream price and per-audit budget.
- Use deterministic checks for HTTP, payment, latency, and schema.
- Use the LLM only for semantic quality evaluation.
- Produce one clean, shareable report page.
- Publish a machine-readable verdict for use by other agents.
- Begin with known x402 services and other hackathon submissions.

## Safety and integrity requirements

- Block localhost, private networks, link-local addresses, redirects to private hosts, and other SSRF targets.
- Never pay above the disclosed budget without explicit approval.
- Record timestamps, attempt counts, prices, and relevant transaction evidence.
- Separate objective protocol checks from subjective LLM judgments.
- Label scores as point-in-time observations rather than permanent guarantees.
- Prevent providers from supplying hidden evaluation instructions through their responses.
- Avoid exposing secrets, wallet credentials, or sensitive response contents in public reports.

## Possible business model

An audit requester pays a fixed amount. Part of that payment funds the downstream probe, and the remainder is the Canary402 service fee. Failed audits should clearly state whether downstream payment settled and which costs were incurred.

Possible later products include recurring uptime monitoring, batch audits, competitive agent bake-offs, buyer-side routing, signed reliability attestations, and automatic pre-purchase checks for autonomous agents.

## One-line demo

> Give Canary402 a paid agent URL and an expectation; it makes a real x402 purchase and returns a public, evidence-backed verdict.

## Status

Implemented and live for the Obol Stack/Agents Hackathon.

The current MVP includes:

- A deterministic Go audit API for public HTTPS JSON `GET` and `POST` targets.
- Probe-only and explicitly authorized paid modes with a `0.02 USDC` operator cap.
- x402 v2 `exact`/EIP-3009 support for canonical USDC on Base and Base Sepolia.
- Remote-signer integration, SSRF controls, bounded response capture, and OpenRouter semantic evaluation.
- Free public evidence reports that retain response digests rather than purchased response bodies.
- A Hermes/OpenRouter Agent wrapper and a direct HTTP API, each sold for `0.001` Base Sepolia USDC.
- A permanent deployment at [andrei-obol-agent.dvlabs.dev](https://andrei-obol-agent.dvlabs.dev) using ERC-8004 Agent ID 8104.
- Successful paid settlement tests on Base Sepolia and Base mainnet.

Comparison audits, recurring monitoring, badges, and the leaderboard remain future work.

## Reference links

- [Obol Stack documentation](https://docs.obol.org/obol-stack/obol-stack)
- [Obol overview of agent-to-agent x402 payments](https://blog.obol.org/obol-is-money/)
- [x402scan service discovery](https://www.x402scan.com/discovery)
