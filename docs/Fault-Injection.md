# Fault Injection and Kubernetes Resilience Testing

This chapter is the single source of truth for OpenGate's fault-injection
harness. It freezes the contract that the Go fault suite, the ingress fault
profiles, and the Chaos Mesh scenario runner build against. The mechanism
decision — no fault code in the shipped binary — is recorded in
[ADR-055](./adr/ADR-055-fault-injection-mechanism.md).

## Mechanism

Faults come from two disjoint places, never from code inside the server:

- **In-process app faults** run in the Go test harness (`_test.go` only). A test
  starts the real server in-process and substitutes a **fault-decorating port**
  for one of the consumer interfaces the server already depends on. This is the
  [`store_failure_test.go`](../server/internal/api/store_failure_test.go)
  port-substitution idiom, extended to the other seams.
- **Deployed faults** run in [Chaos Mesh](https://chaos-mesh.org/), installed
  **on-demand** for a drill against the `opengate-staging` namespace and
  uninstalled after. Chaos Mesh runs no code in the server.

Edge 5xx/timeout is injected at ingress-nginx (staging host annotations).

The shipped server binary therefore contains **zero fault-injection code**;
production and staging run the identical image.

## Fault surfaces

### Harness surfaces (in-process, `_test.go`)

Each harness surface is a real seam the server already exposes — a `ServerConfig`
consumer interface, the [FI0 `AgentControl`](../server/internal/api/api.go) port,
the relay registry option, or the chi middleware chain. The harness wraps it with
a decorator that injects an action and asserts server-side behavior.

| Surface | Seam | Faulting technique |
|---|---|---|
| `session.repository` | `ServerConfig.Sessions` ([`session.Repository`](../server/internal/api/api.go)) | Substitute a fault-decorating `session.Repository`. |
| `device.repository` | `ServerConfig.Devices` ([`device.Repository`](../server/internal/api/api.go)) | Substitute a fault-decorating `device.Repository`. |
| `api.before-handler` | chi middleware chain — `Recoverer` / `RequestTimeout(30s)` / `RateLimiter` in [`api.go`](../server/internal/api/api.go) and [`middleware.go`](../server/internal/api/middleware.go) | Test-only middleware or a handler-level fault. **Not a port** — do not substitute one. |
| `agent.control-write` | [`AgentControl`](../server/internal/api/api.go) (four `Send*`, two `Request*Sync`, `Meta()`) | Substitute a fault-decorating `AgentControl`. Connection-close is done by the harness on the concrete conn it owns — there is no `Close()` on the seam. |
| `relay.registry` | [`relay.SessionRegistry`](../server/internal/relay/relay.go) via [`relay.WithRegistry`](../server/internal/relay/relay.go) | Inject a fault-decorating registry through the constructor option (precedent: `degradedRegistry` in [`handlers_health_test.go`](../server/internal/api/handlers_health_test.go)). `ServerConfig.Relay` is a concrete `*relay.Relay` and cannot be wrapped by an interface decorator — the registry option is the seam. |
| `notifications.dispatch` / `amt.operator` | `ServerConfig.Notifier` / `ServerConfig.AMT` | **Candidate, non-gating.** No scenario drives them yet; add a harness case only when one does. |

The **gating core** in normal CI is `session.repository`, `device.repository`,
and the `api.before-handler` middleware. The two repositories are `ServerConfig`
interface ports; the Edge-Sentinel ports (`Correlate`, `TelemetryReader`,
`Inventory`, `Purger`/`PurgeJobs`) and the notifier/AMT ports are candidate,
non-gating.

### Chaos Mesh surfaces (deployed, on-demand)

| Surface | Experiment kind |
|---|---|
| server↔Postgres / server↔relay latency and abort | `NetworkChaos` |
| QUIC/UDP agent path; packet loss / corrupt / reorder / partition (D1) | `NetworkChaos` |
| CPU / memory pressure | `StressChaos` |

### Kubernetes scenario runner (C1/C2, deployed)

Single-pod deletion and bad-rollout are driven by idempotent, staging-only runner
scripts — a direct `kubectl`/`helm` fault needs no Chaos Mesh controller:

- **Pod deletion (C1)** — [`scripts/fault/pod-delete.sh`](../scripts/fault/pod-delete.sh)
  deletes the staging server pod by the exact selector
  `app.kubernetes.io/instance=<release>,app.kubernetes.io/component=server` and
  asserts the Deployment returns a Ready replacement within the pod-recreation SLO.
- **Bad rollout (C2)** — [`scripts/fault/bad-rollout.sh`](../scripts/fault/bad-rollout.sh)
  deploys a deliberately-failing revision (a nonexistent `image.tag`), asserts the
  rollout fails readiness, then `helm rollback`s and asserts the prior image is
  healthy within the rollback SLO. A `trap` safety net rolls back even on
  interruption, so staging never lingers on the bad revision.

Both refuse any namespace but `opengate-staging` and capture evidence
(`kubectl get events`, rollout status, pod state) to `EVIDENCE_DIR` for the drill
artifacts.

### Ingress surface (edge)

Edge 502/504/timeout is injected with version-controlled, staging-only
ingress-nginx annotation templates applied to the public staging host, then
restored. `502` is produced by making the **upstream unavailable** (backend
scaled to zero / pointed at a dead service), not by a reviewed critical-risk
nginx configuration snippet; `504` is produced by a backend delay (Chaos Mesh)
that exceeds the ingress proxy-read timeout. A reviewed-snippet 502 path is
deferred until the ingress security contract is tightened.

The templates and save/apply/restore tooling live in
[`deploy/fault/ingress/`](../deploy/fault/ingress/) — driven by
[`ingress-apply.sh`](../scripts/fault/ingress-apply.sh) and
[`ingress-restore.sh`](../scripts/fault/ingress-restore.sh), which refuse any
namespace but `opengate-staging` and restore the Ingress byte-identical (safe to
re-run from a cleanup `trap`). The chart can never ship a fault annotation:
[`policy/k8s/fault_injection.rego`](../policy/k8s/fault_injection.rego) denies any
rendered manifest carrying a `fault.opengate.dev/…` key, checked against the
production render in `make lint-k8s`.

## Harness action set

| Action | Behavior asserted |
|---|---|
| `delay` | Context-aware timer that exits on cancellation; client-observed latency stays bounded and the server stays healthy. |
| `timeout` | Waits for context expiry or returns a timeout-class error; the boundary maps it to the correct HTTP status with no leaked transaction or goroutine. |
| `error` | Returns a typed boundary error; the handler maps it to the mapped HTTP status. |
| `panic` | `middleware.Recoverer` turns the panic into a 500, telemetry records it, and the **next request succeeds**. |
| `blocked` | Waits on context cancellation — models a hung dependency (replaces a literal deadlock); the request context cancels and the goroutine exits. |
| `connection-close` | The harness closes the concrete connection it owns and asserts **server-side cleanup**: sends surface an error, the device transitions to offline, and no goroutine leaks. Agent-side reconnect is proven by the Chaos Mesh drills, not here. |

## Chaos Mesh experiment surface and guardrails

Because a separate Always-Free cluster is infeasible (the 200 GB block-volume cap
is already consumed — see [ADR-035](./adr/ADR-035-oke-free-tier-block-volume-remediation.md)),
Chaos Mesh runs on the one shared worker under a **mandatory** safety contract.
Every item below is a required deliverable of the scenario runner, not a
convention:

- **On-demand install/uninstall.** Chaos Mesh is `helm install`ed for a drill and
  uninstalled after; no standing chaos control plane sits next to production.
- **Namespace-scoped.** Installed with `clusterScoped=false` and the controller
  bound to `opengate-staging`; the dashboard is disabled.
- **Required `duration`.** Every experiment carries a `duration` — Chaos Mesh
  runs indefinitely by default, so an unbounded experiment is rejected.
- **Staging selectors + production-pod-exclusion guard.** Selectors pin the
  staging namespace and the staging release label; a guard forbids any selector
  that could resolve a production pod, and the install refuses any namespace but
  `opengate-staging`.
- **Pinned arm64 digest.** The daemon image is pinned by an `arm64` digest
  verified to schedule on the A1 worker (multi-arch manifests are not guaranteed).
- **Zero residue.** Every drill uninstalls the daemon and verifies zero Chaos
  Mesh residue, even on failure (`trap` + workflow `always()`).

The exact Helm values, the exclusion admission/selector guard, and the pinned
digest are implemented by the scenario runner.

## Scenario catalog and expected outcomes

Executor legend: **H** = Go harness (in-process) · **CM** = Chaos Mesh
(on-demand, staging) · **IG** = ingress annotations · **RUN** = scenario runner
script ([`scripts/fault/`](../scripts/fault/)).

| Scenario | Executor | Expected outcome | Recovery budget |
|---|---|---|---|
| Slow API handler | H | Client-observed latency bounded by the API-group `RequestTimeout(30s)`; server stays healthy. | n/a |
| Repository timeout | H | Boundary maps the timeout to `503`/`504`-class per handler; no leaked transaction or goroutine. | n/a |
| Handler panic | H | `500` response; process survives and the next request returns `2xx`. | next request |
| Hung dependency | H | Request context cancels; the blocked goroutine exits. | request deadline |
| Agent control-write fault | H | Send surfaces a typed error; device → offline; no goroutine leak. Agent reconnect is proven by CM, not here. | n/a |
| Relay connection drop | H | Both sides close cleanly; server-side cleanup completes and the reconnect path activates. | n/a |
| WebSocket handshake failure | H / IG | Client gets a bounded failure and reconnects. | ≤ 30 s reconnect |
| Deployed DB/relay latency | CM `NetworkChaos` | Real-pod dependency delay bounded; the pool/driver recovers after the fault clears. | ≤ 60 s after clear |
| Edge 502 | IG | Public client gets the configured status; cleanup restores `2xx`. | on restore |
| Edge 504 | IG + CM | Backend delay exceeds the ingress timeout; public client times out; cleanup restores `2xx`. | on restore |
| Pod deletion | RUN | Replacement pod ready within the **120 s** SLO; clients reconnect. | **≤ 120 s** |
| Bad rollout | RUN | Rollout fails readiness; Helm rollback restores the prior image healthy. | ≤ 180 s rollback |
| Packet loss / partition + QUIC (D1) | CM `NetworkChaos` | Reconnect/backoff within budget; no data loss; recovers after the fault clears. | ≤ 60 s after clear |

### Recovery SLO budgets

The pod-recreation SLO is fixed at **120 s** (single-node `Recreate` + image pull
+ readiness on the shared free-tier ARM worker). The remaining budgets above are
**initial, deliberately generous** values so the drills gate from their first run
without flaky-gate false negatives; each is tightened toward the observed p95 as
runs accumulate. No clean-run-history waiting period applies — a drill gates
promotion from its first run.

## Safety invariants

- No fault code in the shipped binary; production and staging run the identical
  image. The Go fault suite lives only in `_test.go`; Chaos Mesh runs entirely
  outside the process.
- Chaos Mesh is present only during an on-demand drill and is scoped to
  `opengate-staging`.
- Every experiment is duration-bounded; every drill verifies zero residue after
  cleanup.
- There is no chaos endpoint or fault flag in the server — the absence of a
  compiled-in injector is asserted structurally by the fault suite's
  no-import rule, not measured as disabled overhead.

## Tenancy / RLS safety

Tenancy is cross-cutting: every repository call runs in a tenant-scoped
transaction whose org comes from the request context (`dbtx`, JWT `org` claim,
per-tx `SET LOCAL app.current_org` — see [Database](./Database.md) and
[ADR-041](./adr/ADR-041-postgres-rls-multitenancy.md)). Every harness fault
decorator **threads the request `context.Context` through unchanged**, so the
tenant GUC still propagates and a fault can never drop or cross an organization
context. The fault suite proves this with a cross-tenant-leak assertion around a
substituted decorator.

## CI/CD gating

Two disjoint CI surfaces both gate promotion:

- The **Go fault suite** runs in normal CI / `make test` as an ordinary
  deterministic job.
- The **infra/network drills** (Chaos Mesh) and **ingress edge tests** run as a
  required stage after staging E2E, installing Chaos Mesh on-demand and asserting
  against the SLO budgets above.

The full suite gates at once — there is no phased gate activation and no
clean-run-history wait. Because the deployed drills gate promotion, their
determinism is mandatory: a flaky drill blocks deploys. See
[CI Pipeline](./CI-Pipeline.md) and [Continuous Deployment](./Continuous-Deployment.md).
