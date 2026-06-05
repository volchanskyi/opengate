# Acceptance-Test Tier — Adoption Proposal (PARKED)

**Created:** 2026-06-04 · **Status:** Parked (research distilled; revisit later) · **Owner:** Ivan

Distilled from a discussion of Matteo Vaccari's
[*Acceptance Tests for AI-Assisted Development*](https://matteo.vaccari.name/posts/acceptance-tests-for-ai-assisted-development/).
This is **not** scheduled work — it captures the idea and the OpenGate-specific
assessment so we can pick it up without re-deriving the analysis.

## The article's thesis (condensed)

- Acceptance tests (ATs) are the key guardrail for AI-assisted dev: AI multiplies
  existing practice, so good guardrails compound.
- ATs are written from the business POV ("user does X in context Y ⇒ Z"), readable
  by non-technical stakeholders; distinct from implementation-detail unit tests.
- **Double-loop TDD:** outer loop = AT from the story; inner loop = unit TDD.
- **Ports-and-adapters is the enabler:** ATs are fast/reliable only if they bypass
  the UI, use **in-memory repository implementations** (not in-memory DBs), and
  replace external systems with local doubles. Plain end-to-end ATs are slow/flaky.
- **Let AI draft the ATs** (human reviews for business correctness), seeded with
  examples of existing ATs in the codebase's house style.
- Stable port signatures (`Create(cfg)`-style) let the agent work without churning
  test infrastructure.

## OpenGate assessment

**Already in place (the hard part):**
- Ports-and-adapters: `api.NewServer(cfg ServerConfig)` with injected ports
  (`SessionRegistry`, `PeerDialer`, `Notifier`, repositories, `AgentLookup`).
- In-memory adapters as doubles: `InProcessRegistry`, the `Null*` impls, the fake
  `PeerDialer` used in C2 tests.
- UI-bypassing component tests already exist but are unlabeled:
  [`relay_handler_test.go`](../../server/internal/api/relay_handler_test.go),
  [`internal_relay_test.go`](../../server/internal/api/internal_relay_test.go) drive
  `httptest.NewServer(srv)` + real WS and assert outcomes.
- Strong deterministic guardrails already: hook-enforced TDD, the gauntlet
  (coverage/sonar/mutation/pentest/e2e), immutable ADRs. So ATs are an
  **incremental** gain here, not the primary guardrail.

**Gap the pattern would close:** coverage is barbell-shaped — fast unit tests +
slow full-stack Playwright ([`web/e2e`](../../web/e2e)), with a thin/unlabeled
API-level middle. Missing the author's *outer loop*: a fast, business-outcome tier.

**Where the article does NOT fit OpenGate (deliberate divergences):**
1. **Keep `testpg` (real Postgres via testcontainers, ADR-029); do not introduce
   in-memory repository fakes.** A hand-written repo double drifts from real
   semantics (`ON CONFLICT`, ordering, tx isolation, exact error values) — a
   fidelity regression for a security-sensitive product. Swap only the *external*
   edges (relay peer, notifier, agent transport) for in-memory doubles.
2. **Skip a Gherkin/YAML business-DSL layer.** The domain is technical (relay /
   device mgmt / AMT); stakeholders are ops/security engineers, so the
   "non-technical readability" payoff is weak. Prefer table-driven Go ATs with
   **domain-language assertions/error messages** (the article's 2nd example shape).

## Proposed adoption (when revisited)

1. **Double-loop discipline at the phase-slice level.** Each slice in
   [`phases.md`](../phases.md) already has a defined observable outcome; formalize
   "write the API-level outcome test first, then unit-TDD the internals." Small
   extension of the existing TDD mandate — no new framework.
2. **Name + grow the HTTP/component AT tier** between unit and Playwright: drive the
   API directly, reuse `ServerConfig` injection, keep `testpg`, double only external
   edges, assert in domain language. Stop growing Playwright; push new
   acceptance coverage into this faster tier (matches the article's anti-slow-E2E
   caution).
3. **One canonical AT exemplar per subsystem** committed as the pattern the agent
   mirrors (this repo already steers via "mirror the existing test style").

## First concrete candidate (when unparked)

C2 cross-server proxy AT: *"browser on relay-instance-B + agent on
relay-instance-A ⇒ bytes flow end-to-end."* Today it's covered only in pieces
(splice test + endpoint test), not as one cross-instance scenario.
- **Fast outer-loop version (now-able, no Docker):** two `relay.NewRelay` instances
  + the real `api.HTTPPeerDialer` over `httptest`, asserting end-to-end byte flow.
- **Top-of-pyramid version:** PR-D's `make e2e-multiserver` (Docker) as confirmation.

## Open questions for the revisit

- Is the fast HTTP AT tier a new package/build tag, or just a labeling +
  convention over the existing `internal/api` tests?
- Do we want a thin DSL/helper (Go, not Gherkin) for relay scenarios, or keep
  raw table-driven tests?
- Which subsystems get a canonical exemplar first (relay, auth, device CRUD)?
