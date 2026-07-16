# Micro-Plan FI1 — Fault-injection specification & safety contract (+ ADR)

**Master:** `context-driven-fault-injection.md` §11 (FI1), §5, §9, §10, §14, Decisions 2 & 9.
**Branch:** `dev`. **Owner:** engineer (Go + docs). **Sequence:** after FI0, before FI2. **Depends on:** FI0 (the `AgentControl` seam exists).
**Status:** Ready after FI0. **Spec + ADR — minimal code.**

## Goal

Freeze the contract FI2/FI4/FI5 build against under the **settled mechanism** (no
fault code in the production binary — Go test-harness for internal faults + Chaos
Mesh, on-demand and staging-scoped, for deployed faults). Record **ADR-041**: the
mechanism *and* the accepted decision to run a **privileged chaos-daemon on the
shared production worker** under a hard guardrail contract.

## Deliverables

1. **Spec doc** — `docs/Fault-Injection.md` (new), the SSOT for FI2/FI4/FI5:
   - **Fault surfaces (master §6):**
     - *Harness (in-process, `_test.go`):* `session.repository` /
       `device.repository` (`ServerConfig` ports), `api.before-handler` (the chi
       middleware chain — faulted via test middleware, **not** a port),
       `relay.registry` (the `relay.SessionRegistry` interface via
       `relay.WithRegistry`, **not** a `*relay.Relay` decorator), the FI0
       `agent.control-write` seam (four `Send*` + two `Request*Sync` + `Meta()`),
       and `notifications.dispatch` / `amt.operator` (**candidate, non-gating** — no
       scenario drives them yet). Gating core = `session.repository`,
       `device.repository`, `api.before-handler` (the two repos are `ServerConfig`
       interfaces, [`api.go:71-109`](../../../server/internal/api/api.go#L71-L109)).
     - *Chaos Mesh (deployed, on-demand):* `PodChaos` (pod-delete, C1),
       `NetworkChaos` (DB/relay latency + QUIC/UDP + **D1** loss/corrupt/partition),
       `StressChaos`; bad-rollout via Helm.
     - *Ingress (FI4):* edge 502/504/timeout.
   - **Harness action set (§5a):** `delay`, `timeout`, `error`, `panic`,
     `blocked` (ctx-cancel — replaces the literal deadlock), `connection-close`.
   - **Chaos Mesh experiment surface (§5b):** the enumerated experiment kinds +
     the **mandatory guardrails** — on-demand install/uninstall, `clusterScoped=
     false` + `opengate-staging` namespace filter, **required `duration`**, staging
     label/namespace selectors, **production-pod-exclusion guard**, **pinned arm64
     digest**, dashboard off.
   - **Per-scenario expected outcomes** — exact HTTP status / typed error class /
     recovery assertion per row of master §7.
   - **Safety invariants:** no fault code in the shipped binary; prod and staging
     run the identical image; Chaos Mesh present only during an on-demand drill;
     zero-residue after every drill.
   - **Tenancy/RLS safety:** every harness fault decorator threads the request
     `context.Context` unchanged (tenant GUC via `dbtx`); a fault never drops or
     crosses org context.
2. **ADR** — `docs/adr/ADR-041-fault-injection-mechanism.md` + index row in
   [`.claude/decisions.md`](../../decisions.md): records (a) **no fault code in the
   production binary** (test-harness + Chaos Mesh; compiled-in injector, build-tag
   binary, and toxiproxy rejected); (b) the **accepted risk** of a privileged
   chaos-daemon on the shared production worker, mitigated by the Decision-9
   guardrails; (c) the **separate-cluster infeasibility** (200 GB block cap); (d)
   **D1 in scope**. Links only **archived** plans / ADRs / code / external URLs
   (adr-plan-link guard).

*(No disabled-overhead benchmark — there is no compiled-in injector to measure;
the production binary contains no fault code, asserted by the FI2 `go-arch-lint`
no-import rule instead.)*

## File inventory

**Create**
- `docs/Fault-Injection.md`
- `docs/adr/ADR-041-fault-injection-mechanism.md`

**Modify**
- `.claude/decisions.md` — ADR-041 index row.
- `docs/Home.md` — link the new Fault-Injection doc.

## Steps

1. After FI0 merges, branch is current.
2. Write `docs/Fault-Injection.md` (spec) and ADR-041 (mechanism + privileged-
   daemon safety contract + separate-cluster finding + D1 in scope).
3. Confirm every fault surface maps to a real `ServerConfig` port / the FI0 seam /
   a Chaos Mesh experiment kind; no entry references deleted code (`relay.peer-dial`,
   Redis) or a compiled-in injector.
4. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## Settled inputs (do not re-litigate — master §14)

- **Mechanism = no fault code in the production binary** (Go harness + Chaos Mesh).
- **Pod-recreation SLO = 120 s**; per-request in-deployment isolation parked.
- **D1 network chaos in scope** via Chaos Mesh `NetworkChaos`.
- Gating = the Go fault-suite in normal CI **and** the Chaos Mesh infra/network
  drills + ingress edge tests as a required post-E2E staging gate — **no
  clean-run-history wait**.

## Open for FI1 to settle (master §14)

1. Recovery SLOs for relay/agent reconnect and rollout rollback — **declare an
   initial budget now** so the drills gate from day one; tighten from observed p95
   as runs accumulate.
2. Whether ingress 502 uses a reviewed snippet or only an upstream failure (prefer
   the latter; defer until the security contract is tightened — FI4).
3. The exact Chaos Mesh guardrail implementation (namespace-scoped Helm values, the
   production-pod-exclusion guard, the pinned arm64 digest) — spec here, build in FI5.

## Acceptance criteria

1. `docs/Fault-Injection.md` enumerates every fault surface, harness action, and
   Chaos Mesh experiment with guardrails + per-scenario expected outcomes.
2. ADR-041 records the no-in-binary mechanism, the privileged-daemon safety
   contract, the separate-cluster finding, and D1 in scope; index row added.
3. No spec entry references deleted fault points or a compiled-in injector.
4. Gauntlet green; doc-links pass.

## Reviewer checklist

- [ ] Every harness surface maps to a real `ServerConfig` port or the FI0 seam;
      every deployed surface maps to a Chaos Mesh experiment kind or ingress.
- [ ] Tenancy/RLS invariant stated: harness decorators preserve the request ctx.
- [ ] Chaos Mesh guardrails stated + testable: on-demand, namespace-scope,
      required `duration`, prod-pod exclusion, pinned digest, zero-residue.
- [ ] ADR-041 records the accepted privileged-daemon risk + the separate-cluster
      infeasibility; links only archived plans / ADRs / code / URLs.
- [ ] No compiled-in injector, no toxiproxy, no Model C header path anywhere.
- [ ] `docs/Fault-Injection.md` linked from Home; no paraphrased numbers.
