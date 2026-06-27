# Context-Driven Fault Injection and Kubernetes Resilience Testing

**Type:** Master plan, **FI-lite scope**. To be broken into micro-plans
(FI0–FI6) per the master→micro-plan flow.
**Status:** **Ready to break into micro-plans.** The hard prerequisite —
[`dormant-scale-out-teardown.md`](archive/dormant-scale-out-teardown.md) — is
**complete** (phases.md, "Dormant Multi-Replica Teardown"): one server replica,
local relay pairing, the slim in-process `SessionRegistry`, no
Redis/Sentinel/peer-proxy/KEDA/PDB/multi-node L4. This plan is written against
that post-teardown tree.

## Review resolution

A post-teardown review cross-checked every fault point and finding against the
source tree. All six findings were confirmed and are folded in here (the
standalone review document has been removed now that its feedback is captured):

1. **Stale gate** — fixed: header de-staled to the completed teardown.
2. **`agent.control-write` is not a wrap-ready port** — confirmed
   ([api.go](../../server/internal/api/api.go) returns concrete
   `*agentapi.AgentConn`; handlers call concrete `Send*`). An **`AgentControl`
   interface seam is now in scope** as workstream **FI0**, sequenced before FI2.
3. **Smoke gate too disruptive** — resolved: promotion gates **only**
   deterministic bounded app faults; pod-delete and bad-rollout produce evidence
   on a **schedule** and gate promotion only after clean-run history.
4. **Security contract** — resolved via **Model C activation** (Decision 5):
   header-driven selection reachable **only on a cluster-internal path**
   (kubectl port-forward), never the public staging Ingress; the token is
   defence-in-depth, not the sole barrier.
5. **Observability prerequisite** — the High-severity in-cluster-scrape techdebt
   is now cleared (techdebt.md, "Severity: High — _None currently._"); still
   **verify `up`/node-exporter/`/metrics`/ingress-log queries live** before any
   infra scenario relies on them as acceptance evidence.
6. **Vague SLOs** — FI1 turns every scenario into a falsifiable hypothesis with
   an exact threshold; the pod-replacement SLO is fixed at **120 s** (Decision
   8 / §14).

## 0. Decisions that shaped this plan

Settled with the user before drafting (do not re-litigate without the user):

1. **Teardown first.** This plan is **sequenced after**
   [`dormant-scale-out-teardown.md`](archive/dormant-scale-out-teardown.md). The
   multi-replica machinery (Redis/Sentinel registry, cross-server proxy, KEDA,
   PDB, multi-node L4) is being **deleted** as free-tier YAGNI. Therefore every
   scenario that only existed to chaos-test that machinery is **out of scope
   here** — you cannot test code that is being removed. This plan describes the
   **post-teardown** topology: one node, one server replica, in-process
   registry, local relay pairing, PostgreSQL.
2. **Full compiled-in injector.** The application-level mechanism is a typed,
   context-driven injector compiled into the server and gated off by default
   (Option B), **not** only adapter-substitution inside Go tests. Rationale:
   it exercises the **real wired composition** + ingress + pod path black-box,
   reaches faults unit tests can't (panic recovery in the live process,
   WebSocket/relay mid-stream drop), and is the centerpiece of the
   capability-demonstration goal. Adapter-substitution integration tests remain
   the cheaper workhorse for pure port-error mapping and run in `make test`.
3. **Dual driver — reliability *and* capability demonstration.** Scenarios are
   chosen for the **actual live single-node failure modes** (reconnect storms,
   panic recovery, repository timeout, pod-kill recovery, bad-rollout rollback),
   **and** the methodology is built to be clean, documented, deterministic, and
   evidence-producing (a portfolio-grade resilience-engineering harness).
4. **Gate-weighted dual purpose.** A reliability **promotion gate** is the
   primary driver; the capability demonstration is the secondary benefit.
   Promotion therefore gates only deterministic, bounded app-level faults;
   disruptive infra drills run scheduled/manual until proven.
5. **Activation = Model C (cluster-internal header path).** Faults are selected
   by a named profile + scenario ID in controlled headers, but the
   fault-selecting listener is bound to a **cluster-internal path only** —
   reachable via the CI `kubectl port-forward` already used for staging smoke
   tests ([cd.yml](../../.github/workflows/cd.yml)), never routed from the
   public staging Ingress. Keeps per-request isolation (a faulted request spares
   a concurrent unselected one) without exposing a public chaos control plane.
   The bearer token stays as defence-in-depth. Rejected: Model B (public
   header+token — needless public attack surface for a gate) and pure Model A
   (kubectl-only — loses per-request isolation).
6. **`AgentControl` seam is in scope (FI0).** Decoupling the API handlers from
   the concrete `*agentapi.AgentConn` is the prerequisite for the
   `agent.control-write` fault point and a net-positive decoupling regardless.
   It ships as its own TDD micro-plan before FI2.
7. **Mechanism chosen in FI1, not assumed.** The compiled-in injector vs
   adapter-substitution call is made in FI1 with a disabled-overhead benchmark
   in hand. §5 describes the compiled-in injector as the leading candidate; FI1
   confirms or substitutes it.
8. **Pod-replacement SLO = 120 s.** Realistic for single-node `Recreate` +
   image pull + readiness on the shared free-tier ARM worker. Relay/agent
   reconnect and Helm-rollback SLOs are measured first (FI1 §14).

## 1. Problem

Post-teardown, OpenGate needs repeatable integration and fault-tolerance tests at
two boundaries:

1. **Application boundaries** — HTTP/WebSocket requests cross hexagonal ports
   into repositories, **local** relay pairing, notifications, AMT, and agent
   connections.
2. **Infrastructure boundaries** — traffic crosses ingress-nginx, the Service,
   the single server pod, and rollouts.

The mechanism must be enabled and configured only by CI/CD, remain inert in
normal deployments, produce deterministic evidence, and fit the OCI Always Free
budget.

## 2. Goals & non-goals

**Goals.** (a) Prove the **live** failure modes recover within declared SLOs:
slow/erroring dependency, handler panic, hung dependency, pod loss, bad rollout,
agent reconnect after a relay/WS drop. (b) Produce a **clean, documented,
repeatable** harness whose every run yields metrics + logs + k8s events tagged
with a scenario ID. (c) Zero overhead and zero attack surface when disabled.

**Non-goals.** Production fault injection; an unauthenticated/user-facing chaos
API; a literal unbounded deadlock that can leak the process; permanent
privileged chaos workloads; a second ingress controller or billable Load
Balancer; multi-replica / Redis / cross-server scenarios (**that code no longer
exists after teardown**); a single-node "node outage" that deliberately takes
production down.

## 3. Scope

### In scope
- **App-level (Option B):** HTTP latency, selected HTTP errors, request timeout,
  bounded blocking, panic recovery, and a selected relay/agent connection close.
- **Port-level faults** at the post-teardown module boundaries (see §6).
- **WebSocket handshake** failure and **relay-path** interruption (local pairing).
- **Edge (Option A):** ingress 502/504/timeout via reviewed, version-controlled,
  staging-only annotation templates.
- **Kubernetes (Option C, reduced):** single-pod deletion (C1) and bad-rollout +
  Helm rollback (C2, single-replica `Recreate`).

### Out of scope (was in the pre-teardown draft; removed by Decision 1)
- Redis/Sentinel multiserver, Redis master deletion, Redis partition (**deleted**).
- Cross-server proxy partition; the `relay.peer-dial` fault point (**deleted**).
- The multi-replica "overlap/capacity" rollout case (single replica only).
- The Redis `emptyDir` staging storage mode (no Redis to host).

### Deferred (documented, not built now)
- **Chaos Mesh network faults (D1)** — packet delay/loss/corruption on the single
  server pod's QUIC/MPS path. Real value for storm/lossy-network testing, but a
  privileged CRI-O daemon for one pod is disproportionate today. Kept as an
  on-demand extension behind an explicit manual profile; **not** in the core
  suite. Pod-to-pod *partition* is permanently irrelevant (one pod).
- **Genuine worker-node outage (C4)** — needs ≥2 workers; the free tier's 200 GB
  block cap and shared single worker physically forbid it (readiness §3). Run
  only in a funded/ephemeral isolated cluster.

## 4. Architectural constraints (verified)

- chi middleware stack confirmed at
  [`api.go:233-270`](../../server/internal/api/api.go#L233-L270): `Recoverer` at
  the top; `RequestTimeout(30s)` + `RateLimiter` apply **only** inside the API
  group. WebSocket routes sit **outside** `RequestTimeout`
  ([`middleware.go:17-24`](../../server/internal/api/middleware.go#L17-L24),
  which documents that `http.TimeoutHandler` is not a `Hijacker`). The injector
  slots in **after** `RequestTimeout` in the API group, with a **separate**
  selector at the WebSocket route.
- Module dependencies are ports in `ServerConfig`; fault behavior **wraps ports**
  at the composition root — domain packages stay unaware of fault injection.
- Staging deploys through the OKE job in
  [`cd.yml`](../../.github/workflows/cd.yml); staging server + Postgres are
  ephemeral in
  [`values-staging.yaml`](../../deploy/helm/opengate/values-staging.yaml).
- Production and staging share **one** worker; the production server binds the
  QUIC and MPS host ports — so infra faults must target only the **staging**
  namespace and never the shared node.
- Server Deployment is `Recreate` single-replica post-teardown — C2 tests
  rollback of a bad revision, not surge behavior.
- All source changes follow the repo TDD + deterministic-test rules.

## 5. Mechanism — Option B injector (primary)

Compiled into the server, **inert** unless Helm sets an explicit staging-only
enable flag. Request flow:

1. Middleware checks whether injection is enabled (fail-closed otherwise).
2. A selected request supplies a **named profile + scenario ID** via controlled
   headers and an auth token — never arbitrary duration/status/module names.
   Per **Model C** (Decision 5), the fault-selecting listener accepts these
   headers **only on the cluster-internal path** (kubectl port-forward); the
   public staging Ingress is never routed to it.
3. Middleware resolves the profile from startup config and stores a **typed,
   immutable** spec in `context.Context`.
4. **Port decorators** inspect the context at named boundaries and execute the
   configured action.
5. Metrics + structured logs record scenario, fault point, action, result.

### Supported actions
- **Delay** — context-aware timer, exits immediately on cancellation.
- **Timeout** — wait for context expiry or return a timeout-class error.
- **Error** — typed boundary error, or a configured HTTP status at the API point.
- **Panic** — at a bounded point, to verify `middleware.Recoverer` + telemetry +
  process survival.
- **Blocked dependency** — wait on context cancellation (emulates a hung module);
  replaces the excluded literal deadlock.
- **Connection close** — close a selected relay/agent connection through its
  adapter to exercise reconnect.

### Fail-closed when
- enabled outside the staging namespace/environment;
- the fault headers arrive on the public Ingress path rather than the
  cluster-internal listener (Model C);
- profile unknown; auth token absent/invalid; fault point not allowed by profile.

### Helm enablement
```yaml
faultInjection:
  enabled: false
  environment: staging
  secretKey: FAULT_INJECTION_TOKEN
  profiles: {}
```

## 6. Fault points (post-teardown)

High-value boundaries that survive teardown, wired as port decorators at the
composition root:

- `api.before-handler`
- `session.repository`
- `device.repository`
- `relay.registry` (now the **slimmed single-server** `InProcessRegistry`)
- `relay.session-drop` (close a live local-pairing relay connection)
- `notifications.dispatch`
- `amt.operator`
- `agent.control-write` — **blocked on FI0**: today the API holds a concrete
  `*agentapi.AgentConn` ([api.go](../../server/internal/api/api.go),
  handlers call concrete `Send*`), so this point is only wrappable once FI0
  lands the `AgentControl` interface seam.
- `websocket.before-upgrade`

(Removed vs. the pre-teardown draft: `relay.peer-dial` — the `PeerDialer` is
deleted.) `relay.session-drop` stays **deferred** until its exact close
semantics and scenario-token selection are reviewed; the gating core is
`session.repository`, `device.repository`, and `api.before-handler`, all of
which are already interfaces in `ServerConfig`.

## 7. Scenario catalog (reliability-focused)

| Scenario | Executor | Expected assertion |
|---|---|---|
| Slow API handler | B | Client timeout/error bounded; server stays healthy |
| Repository timeout | B | Typed error mapping; no leaked txn/goroutine |
| Handler panic | B | 500 response; process + next request survive |
| Hung dependency | B | Request context cancels; goroutine exits |
| Agent control-write fault | B | Agent reconnects; session re-establishes |
| Relay connection drop | B | Both sides close cleanly; reconnect path activates |
| WebSocket handshake failure | A/B | Client gets bounded failure and reconnects |
| Edge 502 | A | Public client gets configured status; cleanup restores 2xx |
| Edge 504 | A+B | Backend delay exceeds ingress timeout; public client times out |
| Pod deletion | C1 | Replacement pod ready within **120 s** SLO; clients reconnect |
| Bad rollout | C2 | Rollout fails; Helm rollback succeeds; prior image healthy |
| *(deferred)* Packet latency/loss | D1 | Error/latency budget changes per profile and recovers |

## 8. CI/CD control model

Add `.github/workflows/fault-tolerance.yml`:

- `workflow_dispatch` inputs restricted to an **enumerated** profile;
- a reusable entry callable **after** staging E2E;
- the existing OCI/kubeconfig action;
- concurrency guard (no two overlapping staging fault runs);
- a hard timeout longer than the longest bounded scenario.

Activation by repository variable only (no manual app mutation). Per **Model C**,
the workflow reaches the injector over the existing `kubectl port-forward`, not
the public Ingress:
- `STAGING_FAULT_TESTS=false` disables the staging CD invocation;
- `STAGING_FAULT_PROFILE=smoke` selects the bounded post-deploy subset;
- the deferred `network` profile (D1) is manual/scheduled only.

**Execution order:** verify baseline + budget → enable only what the profile
needs → start observation + record scenario ID → apply one fault → run black-box
+ internal assertions → remove fault in a shell `trap` **and** workflow
`always()` → verify baseline health, rollout state, pod count, zero residue →
upload assertions, logs, metrics, events.

**Profiles:**
- **smoke (gating):** B app faults only (delay/panic/timeout/error on
  `session.repository`, `device.repository`, `api.before-handler`) — runs in
  staging CD after E2E and **gates production promotion**. Deterministic,
  bounded, fast.
- **infra (scheduled evidence):** C1 pod deletion, C2 bad rollout — runs
  scheduled/manual, uploads evidence, and **does not gate promotion** until it
  has a clean scheduled-run history (Decision 4).
- **network (deferred):** D1 packet faults — manual/scheduled, never gates
  promotion.

## 9. Observability & evidence (capability-demonstration goal)

- Scenario ID appears in application logs, a dedicated metric label, and CI output.
- Reuse existing HTTP/latency/DB/relay/connected-agent metrics; query ingress
  logs for edge 5xx.
- Capture `kubectl get events`, rollout status, pod restarts, readiness
  transitions per run as uploaded artifacts.
- Keep an **external** health assertion for scenarios that can remove all
  in-cluster observers.
- **Prerequisite:** the live node scrape currently fails in VictoriaMetrics —
  restore node metrics before any infra scenario relies on CPU/mem/disk
  assertions (readiness doc, Observability).

## 10. Quality metrics / NFRs

| Concern | Required result |
|---|---|
| Disabled overhead | No goroutines/timers created; only a disabled-state branch. Benchmark enabled vs disabled paths. |
| Determinism | Every scenario: named profile, bounded duration, fixed assertion, cleanup. |
| Isolation | Fault selection limited to staging namespace + explicitly selected requests/pods. |
| Security | No public chaos endpoint; no secret logged; least-privilege CI/RBAC; token required; production-deny enforced by policy test. |
| Recovery | Baseline health + affected operation pass after cleanup. |
| Runtime | Smoke profile fits the staging deploy gate; deeper profiles manual/scheduled. |
| Cost | Stays inside Always Free (no added reserved infra; delayed requests hold only their own goroutine/connection until the bounded context ends). |
| Maintainability | Fault points + profiles enumerated and schema-validated, not assembled as arbitrary shell. |

## 11. Workstreams → micro-plans (FI0–FI6)

**Broken out into self-contained micro-plan files** (each with file inventory,
TDD steps, acceptance, reviewer checklist), all in `.claude/plans/`. Implement in
order; only FI0 has no dependency (active-plan files are referenced by name, not
linked — they move on archive):

- FI0 — `fi0-agentcontrol-seam.md`
- FI1 — `fi1-fault-injection-spec.md`
- FI2 — `fi2-application-injector.md`
- FI3 — `fi3-helm-fault-config.md`
- FI4 — `fi4-ingress-fault-profiles.md`
- FI5 — `fi5-k8s-scenario-runner.md`
- FI6 — `fi6-fault-ci-integration-docs.md`

The summaries below are the master-level description of each workstream:


- **FI0 — `AgentControl` interface seam (TDD first).** Introduce an
  `AgentControl` interface (the `Send*` surface the API uses) so handlers depend
  on a port, not the concrete `*agentapi.AgentConn`. Net-positive decoupling on
  its own; unblocks the `agent.control-write` fault point. Failing tests first,
  then extract the interface, then re-point `AgentGetter`/handlers. No
  fault-injection code yet.
- **FI1 — Specification & safety contract.** Fault points, actions, profile
  schema, max duration, concurrency caps, staging-only/production-deny
  invariants, expected HTTP + typed errors per scenario. Record an ADR (this
  introduces a privileged testing system + a cross-cutting app mechanism).
- **FI2 — Application injector (TDD first).** `server/internal/faultinject/`
  typed config + context helpers; API selection middleware **after**
  `RequestTimeout`; separate WS-route selector; port decorators at the
  composition root; metrics + logs; benchmarks. Tests for disabled behavior,
  token validation, unknown profiles, context cancellation, bounded blocking,
  panic recovery, concurrent-request isolation.
- **FI3 — Helm configuration.** `faultInjection` values + env wiring; reject
  enablement unless namespace is staging; chart + policy tests for production
  denial and resource limits.
- **FI4 — Ingress profiles.** Version-controlled staging-only annotation
  templates; save/apply/restore tooling; a policy test that production Ingress
  cannot receive fault annotations; verify 502/504 through the public staging
  host.
- **FI5 — Kubernetes scenario runner.** Idempotent, cleanup-safe scripts for pod
  deletion and bad-rollout+rollback, with exact selectors + namespace guards.
- **FI6 — CI integration & docs.** `fault-tolerance.yml` + reusable entry; smoke
  profile after staging E2E behind the repo variable; artifact upload; deployment
  docs; profile ownership/cleanup/emergency-removal runbook; update project
  state; archive this plan.

*(The deferred D1 Chaos-Mesh workstream is documented in §3 but not scheduled.)*

## 12. Sequencing & risk

- **Prerequisite met:** [`dormant-scale-out-teardown.md`](archive/dormant-scale-out-teardown.md)
  is complete, so the deleted fault points (`relay.peer-dial`, Redis) are
  already out of this plan.
- Order: FI0 (`AgentControl` seam) → FI1 (spec/ADR + mechanism choice) → FI2
  (injector, the bulk) → FI3 (Helm) → FI4 (ingress) → FI5 (k8s runner) → FI6
  (CI/docs). Each micro-plan keeps the gauntlet green per commit.
- **Risk:** FI2 adds a cross-cutting mechanism to the live server — it must be
  provably inert when disabled (benchmark + a test asserting no goroutine/timer
  on the disabled path). Security-review the token + production-deny before
  enabling in any environment.

## 13. Acceptance criteria

1. Normal server + Helm deployments have fault injection disabled, zero overhead.
2. Production rendering fails if fault injection or fault ingress annotations are
   enabled.
3. A selected request triggers a bounded internal fault without affecting an
   unselected concurrent request.
4. Panic injection returns 500 while the next request succeeds.
5. A blocked dependency exits when the request context is canceled.
6. Public staging tests produce and recover from a controlled edge timeout.
7. Pod deletion and bad-rollout scenarios recover within declared SLOs.
8. Every workflow failure path removes fault resources and verifies no residue.
9. No Redis/multiserver/cross-server scenario exists (consistent with teardown).
10. The full precommit gauntlet and the staging smoke profile pass.

## 14. Decisions (settled vs. still open for FI1)

**Settled** (do not re-litigate without the user):
- **Pod-recreation SLO = 120 s** (Decision 8).
- **Activation = Model C**, cluster-internal header path only (Decision 5).
- **Gating core ports** = `session.repository`, `device.repository`,
  `api.before-handler` (all already interfaces in `ServerConfig`);
  `agent.control-write` lands after FI0; `relay.session-drop` deferred.
- **Smoke profile gates promotion with app faults only**; infra drills are
  scheduled evidence (Decision 4).

**Open for FI1:**
1. Recovery SLOs for relay/agent reconnect and rollout rollback — **measure
   first** (run several scheduled drills, set the gate threshold above observed
   p95), unlike the pre-set 120 s pod SLO.
2. Whether ingress 502 injection uses a reviewed critical-risk snippet or only a
   controlled application/upstream failure (prefer the latter; deferred until
   the security contract is tightened).
3. The mechanism choice (compiled-in injector vs adapter-substitution), made
   with the disabled-overhead benchmark in hand (Decision 7).
