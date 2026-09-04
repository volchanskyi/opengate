# Fault Injection and Kubernetes Resilience Testing

This chapter is the single source of truth for OpenGate's fault-injection
harness. It freezes the contract that the Go fault suite, the ingress fault
profiles, the Kubernetes scenario runners and the nightly network drill build
against. The mechanism decision — no fault code in the shipped binary — is
recorded in [ADR-055](../adr/ADR-055-fault-injection-mechanism.md).

## Mechanism

Faults come from two disjoint places, never from code inside the server:

- **In-process app faults** run in the Go test harness (`_test.go` only). A test
  starts the real server in-process and substitutes a **fault-decorating port**
  for one of the consumer interfaces the server already depends on. This is the
  [`store_failure_test.go`](../../server/internal/api/store_failure_test.go)
  port-substitution idiom, extended to the other seams.
- **Deployed faults** come from three staging-scoped places outside the server:
  runner scripts driving `kubectl` and `helm` directly, ingress-nginx annotations
  at the edge, and a **link shaper** the drill's own machines send their traffic
  through. None of them runs code in the server, and none of them holds any
  privilege: the shaper is an ordinary unprivileged pod with two sockets.

Edge 5xx/timeout is injected at ingress-nginx (staging host annotations).

The shipped server binary therefore contains **zero fault-injection code**;
production and staging run the identical image.

## Fault surfaces

### Harness surfaces (in-process, `_test.go`)

Each harness surface is a real seam the server already exposes — a `ServerConfig`
consumer interface, the [FI0 `AgentControl`](../../server/internal/api/api.go) port,
the relay registry option, or the chi middleware chain. The harness wraps it with
a decorator that injects an action and asserts server-side behavior.

| Surface | Seam | Faulting technique |
|---|---|---|
| `session.repository` | `ServerConfig.Sessions` ([`session.Repository`](../../server/internal/api/api.go)) | Substitute a fault-decorating `session.Repository`. |
| `device.repository` | `ServerConfig.Devices` ([`device.Repository`](../../server/internal/api/api.go)) | Substitute a fault-decorating `device.Repository`. |
| `api.before-handler` | chi middleware chain — `Recoverer` / `RequestTimeout(30s)` / `RateLimiter` in [`api.go`](../../server/internal/api/api.go) and [`middleware.go`](../../server/internal/api/middleware.go) | Test-only middleware or a handler-level fault. **Not a port** — do not substitute one. |
| `agent.control-write` | [`AgentControl`](../../server/internal/api/api.go) (four `Send*`, two `Request*Sync`, `Meta()`) | Substitute a fault-decorating `AgentControl`. Connection-close is done by the harness on the concrete conn it owns — there is no `Close()` on the seam. |
| `relay.registry` | [`relay.SessionRegistry`](../../server/internal/relay/relay.go) via [`relay.WithRegistry`](../../server/internal/relay/relay.go) | Inject a fault-decorating registry through the constructor option (precedent: `degradedRegistry` in [`handlers_health_test.go`](../../server/internal/api/handlers_health_test.go)). `ServerConfig.Relay` is a concrete `*relay.Relay` and cannot be wrapped by an interface decorator — the registry option is the seam. |
| `notifications.dispatch` / `amt.operator` | `ServerConfig.Notifier` / `ServerConfig.AMT` | **Candidate, non-gating.** No scenario drives them yet; add a harness case only when one does. |

The **gating core** in normal CI is `session.repository`, `device.repository`,
and the `api.before-handler` middleware. The two repositories are `ServerConfig`
interface ports; the Edge-Sentinel ports (`TelemetryReader`, `Inventory`,
`Purger`/`PurgeJobs`) and the notifier/AMT ports are candidate, non-gating.

### Link-shaper surface (deployed, nightly)

The machine-facing QUIC path is faulted by putting a forwarder in it. The drill's
machines dial the name on the server's certificate; a `hostAliases` entry points
that name at the shaper instead of at the server pod, and the shaper forwards
every datagram on to the server with whatever impairment the scenario has
commanded. Enrolment goes to the Service by its fully-qualified name, which an
`/etc/hosts` entry for the short name does not intercept, so the shaper carries
UDP and nothing else.

| Surface | Impairment |
|---|---|
| machine↔server QUIC path | total outage, one-way packet loss, fixed delay each way, a shared bit-rate link, and re-addressing mid-connection |

The forwarder is [`server/tests/netfault/`](../../server/tests/netfault), built
for the node's architecture and copied into the pod the drill creates from
[`netfault-shaper-pod.sh`](../../deploy/scripts/netfault-shaper-pod.sh). Every
impairment draws from a seed recorded in the run's evidence, so two nights with
the same seed make the same decisions. A `go list -deps` assertion holds the
shaper out of the shipped server binary, in the pattern
[`noship_test.go`](../../server/internal/faulttest/noship_test.go) sets.

### Kubernetes scenario runner (C1/C2, deployed)

Single-pod deletion and bad-rollout are driven by idempotent, staging-only runner
scripts, which drive the cluster's own API directly:

- **Pod deletion (C1)** — [`scripts/fault/pod-delete.sh`](../../scripts/fault/pod-delete.sh)
  deletes the staging server pod by the exact selector
  `app.kubernetes.io/instance=<release>,app.kubernetes.io/component=server` and
  asserts the Deployment returns a Ready replacement within the pod-recreation SLO.
- **Bad rollout (C2)** — [`scripts/fault/bad-rollout.sh`](../../scripts/fault/bad-rollout.sh)
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
nginx configuration snippet; `504` is produced by shortening the ingress
proxy-read and proxy-send timeouts below what the backend takes to answer. A reviewed-snippet 502 path is
deferred until the ingress security contract is tightened.

The templates and save/apply/restore tooling live in
[`deploy/fault/ingress/`](../../deploy/fault/ingress) — driven by
[`ingress-apply.sh`](../../scripts/fault/ingress-apply.sh) and
[`ingress-restore.sh`](../../scripts/fault/ingress-restore.sh), which refuse any
namespace but `opengate-staging` and restore the Ingress byte-identical (safe to
re-run from a cleanup `trap`). The chart can never ship a fault annotation:
[`policy/k8s/fault_injection.rego`](../../policy/k8s/fault_injection.rego) denies any
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
| `connection-close` | The harness closes the concrete connection it owns and asserts **server-side cleanup**: sends surface an error, the device transitions to offline, and no goroutine leaks. Agent-side reconnect is proven by the nightly network drill, not here. |

## The nightly network drill

The link between a customer's machine and the server is the failure a remote
management product meets most often in the field, and it is the one the reconnect
backoff, the flap governor and the backfill engine exist to survive.
[`network-drill.yml`](../../.github/workflows/network-drill.yml) runs four
scenarios against it every night, driven by
[`network-drill.sh`](../../scripts/fault/network-drill.sh).

Two machines are behind the shaper at once. The **real machine** is the shipped
agent, taken as an artifact from the image build that produced the running
staging image, so the scenarios exercise the real backoff schedule and the real
backfill engine. The **simulated fleet** is twenty agents from
[`tests/loadtest`](../../server/tests/loadtest) — enough, in one tenant, to
queue against the concurrent drains the server admits per customer.

Every scenario is three phases: baseline, fault, recovery.

| Scenario | The fault | What is measured |
|---|---|---|
| S1 | the site goes dark, and comes back on a healthy link | how long the machine took to come back on its own, and how much of the hole its absence left in the customer's charts filled in |
| S2 | the same outage, recovered over a 2 Mbit/s uplink shared by every machine | the worst staleness of the live readings while the site catches up, and whether any machine lost its connection doing it |
| S3 | the connection stays up and a fifth of what the machine sends is lost | whether the machine holds its connection or churns |
| S4 | a third of a second each way, then the machine returns on a new address | whether the session survives the new address, and — recorded separately — whether the machine reconnected instead |

S4's two numbers are recorded together on purpose. A migration that does not
happen and a link that breaks look identical from the outside: both end at the
idle timeout. Only the pair tells them apart.

### What the drill is held to

- **A scenario that could not observe the system emits nothing.** A dead or
  unreachable shaper, a refused impairment, or a drop count that disagrees with
  the impairment commanded all end the scenario with no row at all. Rows of
  zeroes pull a window median down, and one bad night would quietly cost two.
- **No privilege of any kind.** No node agent, no runtime socket, no added
  capability, no root.
- **Staging only.** The runner refuses any namespace but `opengate-staging`, and
  it takes the namespace lease before touching anything.
- **It never gates a deploy.** The drill reports and trends. A regression turns
  the nightly red and raises a Telegram alert; nothing it finds blocks a release.
- **The link is handed back clear**, on every path out of a scenario including
  the ones that end badly.

Measurements reach VictoriaMetrics as `netdrill_*` series labelled by scenario
and victim, and render on the **Network Drill Trends** dashboard
([`network-drill-trend.json`](../../deploy/grafana/provisioning/dashboards/network-drill-trend.json)).
[`network-drill-regression-check.sh`](../../scripts/network-drill-regression-check.sh)
compares each night against a fourteen-day window and against absolute floors,
and says in its output which of the two it applied.

## Scenario catalog and expected outcomes

Executor legend: **H** = Go harness (in-process) · **IG** = ingress annotations ·
**RUN** = scenario runner script ([`scripts/fault/`](../../scripts/fault)) ·
**ND** = the nightly network drill through the link shaper.

| Scenario | Executor | Expected outcome | Recovery budget |
|---|---|---|---|
| Slow API handler | H | Client-observed latency bounded by the API-group `RequestTimeout(30s)`; server stays healthy. | n/a |
| Repository timeout | H | Boundary maps the timeout to `503`/`504`-class per handler; no leaked transaction or goroutine. | n/a |
| Handler panic | H | `500` response; process survives and the next request returns `2xx`. | next request |
| Hung dependency | H | Request context cancels; the blocked goroutine exits. | request deadline |
| Agent control-write fault | H | Send surfaces a typed error; device → offline; no goroutine leak. Agent reconnect is proven by ND, not here. | n/a |
| Relay connection drop | H | Both sides close cleanly; server-side cleanup completes and the reconnect path activates. | n/a |
| WebSocket handshake failure | H / IG | Client gets a bounded failure and reconnects. | ≤ 30 s reconnect |
| Edge 502 | IG | Public client gets the configured status; cleanup restores `2xx`. | on restore |
| Edge 504 | IG | The proxy read timeout is shorter than the backend takes; public client times out; cleanup restores `2xx`. | on restore |
| Pod deletion | RUN | Replacement pod ready within the **120 s** SLO; clients reconnect. | **≤ 120 s** |
| Bad rollout | RUN | Rollout fails readiness; Helm rollback restores the prior image healthy. | ≤ 180 s rollback |
| Machine outage, healthy recovery (S1) | ND | The machine comes back unaided and the hole in its charts fills to at least 95 %. | **≤ 120 s** to reconnect |
| Machine outage, thin-uplink recovery (S2) | ND | Live readings stay fresh while the site catches up; no machine loses its connection. | ≤ 90 s staleness |
| One-way packet loss (S3) | ND | The machine holds its connection; no offline transition, no flap. | n/a — held throughout |
| Satellite delay and re-addressing (S4) | ND | The connection stays open at 300 ms each way, and the session survives the machine returning on a new address. | ≤ 90 s after the change |

### Recovery SLO budgets

The pod-recreation SLO is fixed at **120 s** (single-node `Recreate` + image pull
+ readiness on the shared free-tier ARM worker). The remaining budgets above are
**initial, deliberately generous** values so the drills gate from their first run
without flaky-gate false negatives; each is tightened toward the observed p95 as
runs accumulate. No clean-run-history waiting period applies — a drill gates
promotion from its first run.

## Safety invariants

- No fault code in the shipped binary; production and staging run the identical
  image. The Go fault suite lives only in `_test.go`, and the link shaper is a
  separate binary a `go list -deps` assertion keeps out of the server's
  dependency graph.
- Every deployed fault is scoped to `opengate-staging`, and every runner refuses
  any other namespace.
- Every drill is bounded and removes what it created, verified on every path
  (`trap` + workflow `always()`); the network drill additionally asserts that no
  pod it created remains.
- There is no chaos endpoint or fault flag in the server — the absence of a
  compiled-in injector is asserted structurally by the fault suite's
  no-import rule, not measured as disabled overhead.

## Tenancy / RLS safety

Tenancy is cross-cutting: every repository call runs in a tenant-scoped
transaction whose tenant comes from the request context (`dbtx`, JWT `tenant` claim,
per-tx `SET LOCAL app.current_tenant` — see [Database](../architecture/Database.md) and
[ADR-041](../adr/ADR-041-postgres-rls-multitenancy.md)). Every harness fault
decorator **threads the request `context.Context` through unchanged**, so the
tenant GUC still propagates and a fault can never drop or cross a tenant
context. The fault suite proves this with a cross-tenant-leak assertion around a
substituted decorator.

## CI/CD gating

Two disjoint CI surfaces gate promotion:

- The **Go fault suite** runs in normal CI / `make test` as an ordinary
  deterministic job — the in-process app-fault gate, needing no staging cluster.
- The **deployed drills** run after staging E2E through the reusable
  [`fault-tolerance.yml`](../../.github/workflows/fault-tolerance.yml) workflow,
  invoked from the staging deploy job in
  [`cd.yml`](../../.github/workflows/cd.yml). Each drill runs one
  [`scripts/fault/`](../../scripts/fault) runner against `opengate-staging`,
  uploads its evidence directory as a run artifact, reverts every fault under
  `always()`, and verifies the staging Ingress is left free of any
  `fault.opengate.dev/…` annotation.

Activation is **config-only**, by repository variable:

- `STAGING_FAULT_TESTS` — when `true`, the deploy pipeline runs the drill stage
  after E2E and **gates production promotion** on it (a failed drill blocks the
  deploy). Unset or `false` skips the stage and production proceeds.
- `STAGING_FAULT_PROFILE` — selects the enumerated scenario (`pod-delete`,
  `bad-rollout`, `ingress-504`, `ingress-502`); defaults to `pod-delete`.

The deploy pipeline is the workflow's only entry point, so every drill runs
against the staging deploy it gates. The scenario is always one of that
enumerated allow-list: a runtime guard re-validates the `workflow_call` string so
a repository variable can never inject a free-form or out-of-band scenario.
On-demand network drills (packet loss/corrupt/partition on the QUIC path) are run
from their own tooling and are never wired into this gating path.

Because the deployed drills gate promotion when activated, their determinism is
mandatory: a flaky drill blocks deploys. Before an infra scenario is trusted for
CPU/mem/disk evidence, verify the live node scrape (`up`, node-exporter,
`/metrics`, ingress logs) in VictoriaMetrics first. See
[CI Pipeline](./CI-Pipeline.md) and [Continuous Deployment](./Continuous-Deployment.md).

## Operating the drills

### Ownership

- **In-process app faults** — the Go fault suite
  ([`integration_fault_suite_test.go`](../../server/internal/api/integration_fault_suite_test.go),
  [`noship_test.go`](../../server/internal/faulttest/noship_test.go))
  over the [`faulttest`](../../server/internal/faulttest/ports.go) decorators. Owned
  by the server; changes ride normal TDD and `make test`.
- **Deployed drills** — the [`scripts/fault/`](../../scripts/fault) runners and the
  [`fault-tolerance.yml`](../../.github/workflows/fault-tolerance.yml) workflow. Each
  runner refuses any namespace but `opengate-staging`.
- **Ingress templates** — [`deploy/fault/ingress/`](../../deploy/fault/ingress).

### Cleanup

Every drill is self-cleaning, and cleanup is idempotent (safe to re-run):

- `pod-delete` — no residue; Kubernetes recreates the deleted pod.
- `bad-rollout` — a shell `trap` always `helm rollback`s to the captured good
  revision, even on interruption, so staging never lingers on the bad revision.
- `ingress-504` / `ingress-502` —
  [`ingress-restore.sh`](../../scripts/fault/ingress-restore.sh) returns the Ingress
  byte-identical / the Deployment to its saved replica count; with no saved state
  it is a no-op. The workflow runs it under `always()` and then asserts no
  `fault.opengate.dev/…` annotation remains.

### Emergency removal

To clear a lingering staging fault by hand, from a checkout with cluster access:

```bash
# Revert both ingress faults (a no-op if not applied):
NAMESPACE=opengate-staging scripts/fault/ingress-restore.sh edge-504
NAMESPACE=opengate-staging scripts/fault/ingress-restore.sh edge-502

# Roll a stuck bad rollout back to the last deployed revision:
helm rollback opengate-staging "$(helm history opengate-staging -n opengate-staging \
  -o json | jq -r '[.[] | select(.status == "deployed")] | last | .revision')" \
  -n opengate-staging --wait
```

To disable the gate entirely, set the `STAGING_FAULT_TESTS` repository variable
to `false` (or unset it): the deploy pipeline then skips the drill stage and
production promotion proceeds without it.
