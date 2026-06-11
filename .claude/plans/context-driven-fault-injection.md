# Context-Driven Fault Injection and Kubernetes Resilience Testing

**Status:** Draft for review

## Problem

OpenGate needs repeatable integration and fault-tolerance tests at two distinct
boundaries:

1. **Application boundaries** — HTTP requests enter the API and cross
   hexagonal ports into repositories, relay coordination, notifications, AMT,
   and agent connections.
2. **Infrastructure boundaries** — traffic crosses ingress-nginx, Services,
   pods, Redis/Sentinel, rollouts, and worker nodes.

The mechanism must be enabled and configured only by CI/CD, remain inert in
normal deployments, produce deterministic evidence, and fit the OCI Always Free
budget where possible.

## Expected Output

- A typed, context-driven application fault-injection layer compiled into the
  existing Go server.
- Staging-only Helm controls for enabling named fault profiles.
- Declarative ingress, Kubernetes, Redis, and network-chaos scenarios.
- A CI workflow that applies one fault, runs assertions, always cleans up, and
  proves recovery.
- Metrics, logs, Kubernetes events, and test results that distinguish expected
  injected failures from product regressions.
- No persistent chaos infrastructure, additional public endpoint, or manual
  cluster mutation.

## Scope

### Included

- HTTP latency, selected HTTP errors, request timeout, bounded blocking, and
  panic recovery.
- Port-level faults around current module interfaces.
- WebSocket handshake and relay-path interruption.
- Pod deletion, readiness loss, failed rollout, Redis failover, and Redis
  network partition.
- Packet delay, loss, corruption, bandwidth restriction, and partition inside
  the staging namespace.
- A budgeted path for genuine worker-node failure.

### Excluded

- Production fault injection.
- An unauthenticated or user-facing chaos API.
- A literal unbounded deadlock that can leak the server process.
- Permanent privileged chaos workloads.
- A second ingress controller or second Flexible Load Balancer.
- A single-node "node outage" exercise that deliberately takes production down.

## Architectural Constraints

- The server uses chi middleware and keeps WebSocket routes outside the normal
  request timeout wrapper; see [`api.go`](../../server/internal/api/api.go) and
  [`middleware.go`](../../server/internal/api/middleware.go).
- Module dependencies are already expressed as ports in `ServerConfig`; fault
  behavior should wrap those ports rather than enter domain logic.
- Staging deploys through the OKE job in
  [`cd.yml`](../../.github/workflows/cd.yml).
- Staging server and Postgres storage are ephemeral in
  [`values-staging.yaml`](../../deploy/helm/opengate/values-staging.yaml).
- Production and staging share one worker and the production server currently
  binds the QUIC and MPS host ports.
- The Redis chart is dormant but already models three Redis nodes and three
  Sentinels in [`values.yaml`](../../deploy/helm/opengate/values.yaml).
- The cluster runs CRI-O, so any Chaos Mesh daemon must use the CRI-O runtime
  and socket rather than the chart's Docker default.
- All source changes follow the repository's TDD and deterministic-test rules.

## Quality Metrics

| Concern | Required result |
|---|---|
| Disabled overhead | No goroutines or timers created; middleware adds only a disabled-state branch |
| Determinism | Every scenario has a named profile, bounded duration, fixed assertion, and cleanup |
| Isolation | Fault selection is limited to the staging namespace and explicitly selected requests or pods |
| Security | No public chaos endpoint; no secret values logged; least-privilege CI and controller RBAC |
| Recovery | Baseline health and the affected operation pass after cleanup |
| Observability | Scenario ID appears in application logs, metrics, and CI output |
| Runtime | Smoke profile fits the staging deployment gate; deeper profiles run scheduled or manually |
| Cost | Default implementation remains inside Always Free allocations |
| Maintainability | Fault points and profiles are enumerated and schema-validated rather than assembled as arbitrary shell |

## Verified OCI Budget Baseline

Live inspection established:

- One `VM.Standard.A1.Flex` worker using 2 OCPUs and 12 GB RAM.
- The Always Free compute allowance leaves 2 OCPUs and 12 GB RAM unallocated.
- Kubernetes requests consume 1,180m of the node's 1,830m allocatable CPU,
  leaving 650m request headroom.
- The node currently has substantial memory headroom.
- One 50 GB boot volume plus three 50 GB block volumes consume the complete
  200 GB Always Free block-storage allowance.
- The existing ingress uses the tenancy's one free 10 Mbps Flexible Load
  Balancer.
- The separate Always Free Network Load Balancer entitlement is unused.
- The node root filesystem has limited free space, so every new `emptyDir` must
  have a `sizeLimit` and an ephemeral-storage request/limit.

Re-run the following before enabling an infrastructure profile because these
values can change:

```bash
kubectl describe node "$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')"
kubectl get pvc,pv -A
kubectl get svc -A
oci search resource structured-search --query-text 'query instance resources where lifecycleState != "TERMINATED"'
oci search resource structured-search --query-text 'query loadbalancer resources'
oci search resource structured-search --query-text 'query networkloadbalancer resources'
```

Oracle documents the relevant allowances in
[Always Free Resources](https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm):
4 A1 OCPUs, 24 GB memory, 200 GB combined boot/block storage, one 10 Mbps
Flexible Load Balancer, and one Network Load Balancer. OKE Basic Cluster
control planes are free according to the
[OCI price list](https://www.oracle.com/cloud/price-list/#container-engine-kubernetes).

## Option A — Existing ingress-nginx and OCI Load Balancer

### Design

Reuse the existing ingress controller, ingress class, and Flexible Load
Balancer. CI temporarily applies a staging-only ingress fault profile, executes
black-box requests through the public staging hostname, then restores the
baseline manifest.

Possible HTTP-layer controls include:

- proxy connect, send, and read timeouts;
- connection and request-rate limits;
- custom error interception;
- a staging-only configuration snippet that returns a selected HTTP status;
- interaction with Option B, where ingress times out while the application
  deliberately delays the selected request.

The supported annotation set is documented by
[ingress-nginx](https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/).

### Budget

- Additional CPU/memory request: none.
- Additional storage: none.
- Additional Load Balancer: none.
- Always Free result: **fits**.

### Strengths

- Exercises the real public edge, TLS termination, ingress routing, Service,
  and server path.
- Produces black-box behavior visible to Playwright, curl, and external probes.
- Requires no permanent workload.
- Can deterministically produce edge-facing timeout and error behavior when
  paired with a controlled slow backend.

### Limitations

- Standard ingress annotations do not implement arbitrary packet loss,
  corruption, or network partition.
- A timeout annotation only changes timeout policy; it does not make a healthy
  upstream slow by itself.
- Snippet annotations are classified as high or critical risk. The live
  controller currently permits them, but the plan must not expand that
  privilege or expose arbitrary snippet content to workflow inputs.
- Ingress cannot inject faults after a WebSocket upgrade with request-level
  precision.

### Security Posture

- Store approved snippets as version-controlled templates; never interpolate
  arbitrary workflow input into NGINX directives.
- Restrict the fault ingress to the staging host and namespace.
- Capture and restore the original annotations in an `always()` cleanup step.
- Add a policy test that rejects fault annotations on production Ingress
  objects.

### Decision

**Adopt for HTTP-edge scenarios only.** Do not present it as a substitute for
real network chaos.

### Rejected Variant

A separate chaos ingress controller or `LoadBalancer` Service is rejected. It
duplicates the edge, risks consuming a billable Load Balancer, and does not
increase fault fidelity enough to justify the operational surface. The unused
free NLB should remain available for future multi-node QUIC/MPS exposure.

## Option B — Context-Driven Go Fault Injection

### Design

Compile the injector into the existing server and keep it inert unless Helm
sets an explicit staging-only enable flag.

The request flow is:

1. Middleware validates whether fault injection is enabled.
2. A selected request supplies a named profile and scenario ID using controlled
   headers.
3. Middleware resolves the profile from startup configuration and stores a
   typed immutable specification in `context.Context`.
4. Port decorators inspect the context at named boundaries and execute the
   configured action.
5. Metrics and structured logs record the scenario, fault point, action, and
   result.

The context carries only a profile reference. Arbitrary duration, status, or
module names are not accepted from the request.

### Fault Points

Initial points should cover high-value boundaries without scattering checks
through business logic:

- `api.before-handler`
- `session.repository`
- `device.repository`
- `relay.registry`
- `relay.peer-dial`
- `notifications.dispatch`
- `amt.operator`
- `agent.control-write`
- `websocket.before-upgrade`

Decorators implement the existing port interfaces and are wired at the
composition root. Domain packages remain unaware of fault injection.

### Supported Actions

- **Delay:** context-aware timer that exits immediately on cancellation.
- **Timeout:** wait until the request context expires or return a timeout-class
  error expected by the selected boundary.
- **Error:** return a typed boundary error or a configured HTTP status at the
  API point.
- **Panic:** panic at a bounded point to verify `middleware.Recoverer`,
  telemetry, and process survival.
- **Blocked dependency:** wait on context cancellation to emulate a hung module.
  This replaces a literal deadlock, which is intentionally excluded.
- **Connection close:** close a selected relay or agent connection through its
  adapter to exercise reconnect behavior.

### Enablement

Proposed Helm values:

```yaml
faultInjection:
  enabled: false
  environment: staging
  secretKey: FAULT_INJECTION_TOKEN
  profiles: {}
```

The server must fail closed when:

- enabled outside the staging namespace/environment;
- the profile is unknown;
- the authentication token is absent or invalid;
- the requested fault point is not allowed by that profile.

### Budget

- Additional reserved infrastructure: none.
- Transient cost: delayed requests retain their goroutine and connection until
  the bounded context finishes.
- Always Free result: **fits**.

### Strengths

- Best match for the existing hexagonal architecture.
- Deterministic and request-scoped.
- Can target internal modules that ingress and pod-level tools cannot see.
- Fully testable without Kubernetes.
- Zero operational dependency when disabled.

### Limitations

- Does not reproduce kernel-level packet behavior.
- HTTP middleware affects WebSocket setup but not the entire upgraded stream;
  relay adapters need explicit fault points.
- Poorly bounded delays could exhaust connections, so every profile needs a
  maximum duration and concurrency cap.
- Panics test recovery but should not be used where a goroutine has no recovery
  boundary.

### Decision

**Adopt as the primary control plane for application-level scenarios.**

## Option C — Kubernetes-Native Failure Scenarios

Option C contains several materially different scenarios and budget outcomes.

### C1 — Pod Deletion

Delete only the selected staging server pod and assert:

- Kubernetes recreates it;
- liveness and readiness recover;
- the public health endpoint recovers inside the SLO;
- expected clients reconnect;
- no production object changes.

There is no steady-state resource cost.

**Decision:** adopt.

### C2 — Failed or Atomic Rollout

The current server Deployment uses `Recreate` unless shared keys and non-hostPort
L4 are enabled; see
[`server-deployment.yaml`](../../deploy/helm/opengate/templates/server-deployment.yaml).
Therefore a normal staging rollout does not create a surge replica.

Test two explicit cases:

1. **Bad image/readiness:** apply an invalid image or readiness behavior, assert
   rollout failure, invoke Helm rollback, and prove the previous revision is
   healthy.
2. **Overlap/capacity path:** render a dedicated test configuration that permits
   a second staging server and requests the same resources defined in
   [`values-staging.yaml`](../../deploy/helm/opengate/values-staging.yaml).

The temporary second server requests 50m CPU and 96Mi memory.

**Budget:** fits Always Free.

**Decision:** adopt, but describe it as an explicit test topology rather than
the behavior of the current `Recreate` rollout.

### C3 — Redis/Sentinel Multiserver

Enable two staging server replicas, Redis, and Sentinel to test:

- server affinity and peer proxying;
- Redis master deletion and Sentinel promotion;
- readiness drain while the registry is unavailable;
- degraded-mode recovery;
- session continuity and bounded failure.

The application already has local multiserver coverage through
[`e2e-multiserver.yml`](../../.github/workflows/e2e-multiserver.yml). The OKE
scenario extends that coverage to real scheduling, Services, readiness, and
Sentinel behavior.

#### Compute Budget

- Three Redis pods: 150m CPU / 192Mi.
- Three Sentinel pods: 75m CPU / 96Mi.
- Redis/Sentinel total: 225m CPU / 288Mi.
- Temporary second staging server: 50m CPU / 96Mi.
- Full C3 addition: 275m CPU / 384Mi.

This fits the available node request headroom.

#### Storage Blocker

[`redis-statefulset.yaml`](../../deploy/helm/opengate/templates/redis-statefulset.yaml)
always emits three PVCs. OCI rounds each requested volume up to its minimum,
which would consume another 150 GB while the tenancy already uses the full
Always Free storage allowance.

Add a staging-only storage mode:

```yaml
redis:
  data:
    persistent: false
    sizeLimit: 1Gi
```

When persistence is false:

- render one bounded `emptyDir` per Redis pod;
- set ephemeral-storage requests and limits;
- keep production persistence behavior unchanged;
- document that Redis data loss is expected and useful in this test topology.

**Decision:** adopt only after the `emptyDir` mode exists.

### C4 — Genuine Worker-Node Outage

A valid node-outage test needs at least two workers so workloads can move and
the test can distinguish failover from total cluster loss.

A second worker matching the existing worker consumes:

- 2 additional A1 OCPUs;
- 12 GB additional memory;
- a 50 GB boot volume.

The compute allocation exactly reaches the Always Free 4 OCPU / 24 GB ceiling,
but storage reaches 250 GB and exceeds the 200 GB allowance.

Testing by stopping the only current worker is rejected because production and
staging share that worker and no in-cluster recovery path remains.

**Decision:** defer from the free default. Run only in an isolated temporary
environment after one of these gates is satisfied:

- free at least 50 GB of OCI boot/block storage;
- accept paid block storage for the test window;
- use a separate funded tenancy or ephemeral external cluster.

## Option D — Chaos Mesh

### D1 — Slim Installation

Install:

- one controller manager;
- one chaos daemon on the single worker;
- no dashboard;
- no DNS server;
- no bundled Prometheus;
- no persistence.

Use namespace-scoped targeting for `opengate-staging`, a CRI-O runtime/socket,
and a fixed allowlist of Chaos CRDs rendered from the repository.

The current
[Chaos Mesh chart defaults](https://raw.githubusercontent.com/chaos-mesh/chaos-mesh/master/helm/chaos-mesh/values.yaml)
request 25m CPU / 256Mi for each controller and the light daemon profile
requests 100m CPU / 256Mi.

#### Budget

- 125m CPU / 512Mi memory.
- No storage when persistence is disabled.
- No Load Balancer when the dashboard is disabled.
- Always Free result: **fits**.

#### Capabilities

Use Chaos Mesh only where it adds kernel/network fidelity:

- delay and jitter;
- packet loss;
- corruption and duplication;
- bandwidth restriction;
- partition between selected staging pods;
- process or pod fault where Kubernetes-native deletion is insufficient.

Chaos Mesh documents these actions in
[NetworkChaos](https://chaos-mesh.org/docs/simulate-network-chaos-on-kubernetes/).

#### Security

- The daemon is privileged and receives host networking capabilities; install it
  only for the scenario window.
- Pin the chart and image versions.
- Verify the CRI-O socket path before installation.
- Limit controller scope and selectors to the staging namespace.
- Reject external targets and host-network targets.
- Delete CRDs and privileged workloads in `always()` cleanup.

**Decision:** adopt for scheduled/manual high-fidelity network scenarios.

### D2 — Default Installation

The default chart creates three controllers, one daemon per node, dashboard,
and DNS server. On the current one-node cluster this requests approximately:

- 300m CPU;
- 1.3 GB memory.

Dashboard persistence is disabled by default and bundled Prometheus is disabled,
so storage can remain zero.

#### Strengths

- Full UI and DNSChaos feature set.
- Controller redundancy.

#### Weaknesses

- Redundant controller replicas on one worker do not provide node-level
  availability.
- Dashboard and DNS expand the attack surface without serving the selected
  scenarios.
- Combined with C3, only about 75m of CPU request headroom remains.

**Decision:** reject for this cluster.

## Comparative Decision Matrix

| Option | Layer | Fidelity | Added request | Storage/LB | Always Free | Decision |
|---|---|---|---:|---|---|---|
| A | Public HTTP edge | HTTP errors, throttling, timeout policy | 0 | Reuses LB | Yes | Adopt narrowly |
| B | API and module ports | Deterministic internal delay/error/panic/block | 0 reserved | None | Yes | Primary mechanism |
| C1 | Kubernetes pod | Real pod loss/restart | 0 steady | None | Yes | Adopt |
| C2 | Kubernetes rollout | Real rollout failure and rollback | 50m / 96Mi transient | None | Yes | Adopt |
| C3 | Multiserver/Redis | Real Sentinel, readiness, peer routing | 275m / 384Mi | Needs bounded `emptyDir` | Yes after fix | Adopt conditionally |
| C4 | Worker node | Real node failure/rescheduling | 2 OCPU / 12 GB | Adds 50 GB boot | No | Isolated paid test |
| D1 | Pod network/kernel | Real delay/loss/partition/corruption | 125m / 512Mi | None | Yes | Adopt on demand |
| D2 | Full Chaos Mesh | D1 plus dashboard/DNS/redundancy | 300m / ~1.3Gi | None if ephemeral | Yes but tight | Reject |

## Recommended Composite

No single option covers the required fault model. Use:

1. **Option B** as the request-scoped selector and application-boundary
   injector.
2. **Option A** for black-box ingress behavior and edge timeout/error
   assertions.
3. **Option C1/C2** for routine Kubernetes recovery tests.
4. **Option C3** after Redis receives a staging `emptyDir` mode.
5. **Option D1** for packet-level faults that A and B cannot reproduce.
6. **Option C4** only in an isolated environment with an explicit storage
   budget.

The maximum recommended in-cluster addition is C3 plus D1:

- 400m CPU requests;
- 896Mi memory requests;
- about 250m CPU request headroom remaining.

Do not combine C3 with the default D2 topology; it leaves insufficient CPU
request margin for comfortable scheduling and system workload variance.

## Scenario Catalog

| Scenario | Executor | Expected assertion |
|---|---|---|
| Slow API handler | B | Client timeout/error is bounded; server remains healthy |
| Repository timeout | B | Typed error mapping; no leaked transaction/goroutine |
| Handler panic | B | 500 response; process and subsequent request survive |
| Hung module | B | Request context cancels and goroutine exits |
| Edge 502 | A or A+B | Public client receives configured status; cleanup restores 2xx |
| Edge 504 | A+B | Backend delay exceeds ingress timeout; public client receives timeout |
| WebSocket handshake failure | A/B | Client receives bounded failure and reconnects |
| Relay connection drop | B | Both sides close cleanly and reconnect path activates |
| Packet latency/loss | D1 | Error/latency budget changes match profile and recover |
| Pod deletion | C1 | Replacement pod becomes ready within the SLO |
| Bad rollout | C2 | Rollout fails, rollback succeeds, prior image is healthy |
| Redis master deletion | C3 | Sentinel promotes a replica and readiness recovers |
| Redis partition | C3+D1 | Server drains or degrades according to registry policy |
| Worker loss | C4 | Pods reschedule and externally observed service recovers |

## CI/CD Control Model

### Workflow Entry

Add a dedicated workflow, for example
`.github/workflows/fault-tolerance.yml`, with:

- `workflow_dispatch` inputs restricted to an enumerated profile;
- an optional schedule for the deeper profile;
- a reusable workflow entry callable after staging E2E;
- the existing OCI/kubeconfig action;
- concurrency that prevents two staging fault runs from overlapping;
- a hard timeout longer than the longest bounded scenario.

Repository configuration controls activation:

- `STAGING_FAULT_TESTS=false` disables the staging CD invocation.
- `STAGING_FAULT_PROFILE=smoke` selects the bounded post-deploy subset.
- Scheduled/manual runs select `network` or `multiserver`.

No application setting is changed manually.

### Execution Order

1. Verify namespace, cluster, resource budget, scrape health, and baseline
   staging health.
2. Install or enable only the resources required by the selected profile.
3. Start observation and record the scenario ID.
4. Apply one fault.
5. Run black-box and internal assertions.
6. Remove the fault in a shell `trap` and workflow `always()` step.
7. Uninstall temporary privileged resources.
8. Verify baseline health, rollout state, pod count, and absence of Chaos CRs.
9. Upload test output and relevant logs/events.

### Profiles

- **smoke:** B delay/panic, C1 pod deletion, C2 failed rollout; suitable for
  staging CD after normal E2E.
- **multiserver:** C3 Redis/Sentinel scenarios; manual or scheduled.
- **network:** D1 delay/loss/partition plus B/A assertions; manual or scheduled.
- **node:** C4 only, guarded by an explicit isolated-cluster input and budget
  check.

## Observability Requirements

Before infrastructure chaos becomes a gate:

- VictoriaMetrics `up` must report the node and selected application targets as
  healthy.
- The live node scrape currently fails that prerequisite, so restore node
  metrics before relying on CPU, memory, or disk assertions.
- Use existing HTTP request, latency, DB, relay, and connected-agent metrics.
- Query ingress logs for edge 5xx and application logs for the scenario ID.
- Capture `kubectl get events`, rollout status, pod restarts, and readiness
  transitions.
- Keep an external health assertion for scenarios that can remove all in-cluster
  observers.

## Implementation Workstreams

### 1. Specification and Safety Contract

- Define fault points, actions, profile schema, maximum duration, and
  concurrency limits.
- Define staging-only and production-deny invariants.
- Define expected HTTP and typed module errors per scenario.
- Record an ADR because this introduces a privileged testing system and a
  cross-cutting application mechanism.

### 2. Application Injector — TDD First

- Add tests for disabled behavior, token validation, unknown profiles, context
  cancellation, bounded blocking, panic recovery, and concurrent request
  isolation.
- Add `server/internal/faultinject/` with typed configuration and context
  helpers.
- Add API fault-selection middleware inside the API group, after
  `RequestTimeout`, so every injected delay or blocked dependency remains
  bounded by the existing request deadline.
- Add a separate selector at the WebSocket route because upgraded connections
  intentionally remain outside `RequestTimeout`.
- Add port decorators at the composition root.
- Add WebSocket/relay-specific injection points without putting the normal
  timeout middleware around upgrades.
- Add metrics and structured logs.
- Benchmark enabled and disabled middleware paths.

### 3. Helm Configuration

- Add `faultInjection` values and server environment wiring.
- Reject enablement unless the release namespace is staging.
- Add Redis `persistent`/`emptyDir` branching with size and ephemeral-storage
  limits.
- Add chart tests and policy tests for production denial, storage mode, and
  resource limits.

### 4. Ingress Profiles

- Store approved staging-only annotation patches or templates.
- Implement save/apply/restore tooling.
- Add tests that production Ingress cannot receive fault annotations.
- Verify 502/504 behavior through the public staging hostname.

### 5. Kubernetes Scenario Runner

- Add scripts for pod deletion, failed rollout, rollback, and Redis failover.
- Use exact selectors and namespace guards.
- Make every script idempotent and cleanup-safe.
- Reuse the existing multiserver assertions where behavior overlaps.

### 6. Slim Chaos Mesh

- Pin the chart and images.
- Render one controller, one light daemon, CRI-O configuration, no
  dashboard/DNS/Prometheus/persistence.
- Validate OKE CRI-O socket compatibility in a non-destructive probe.
- Restrict scenario manifests to approved selectors and actions.
- Add install, experiment, cleanup, and residue-verification scripts.

### 7. CI Integration

- Add the dedicated workflow and reusable entry.
- Add the smoke profile after staging E2E behind the repository variable.
- Keep deeper profiles outside the normal production promotion path.
- Upload assertions, logs, metrics snapshots, and Kubernetes events.
- Block production promotion only on the enabled smoke profile.

### 8. Documentation and Operations

- Document profile ownership, enablement, cleanup, and emergency removal.
- Add the workflow and Helm controls to deployment documentation.
- Add dashboards or alert annotations only where existing telemetry cannot
  identify an injected fault.
- Update project state and archive this plan after implementation.

## Acceptance Criteria

1. Normal server and Helm deployments have fault injection disabled.
2. Production rendering fails if fault injection or fault ingress annotations
   are enabled.
3. A selected request can trigger a bounded internal fault without affecting an
   unselected concurrent request.
4. Panic injection returns an error while the next request succeeds.
5. A blocked dependency exits when the request context is canceled.
6. Public staging tests can produce and then recover from a controlled edge
   timeout.
7. Pod deletion and failed rollout scenarios recover within their declared
   SLOs.
8. Redis/Sentinel staging topology uses no OCI block volume.
9. Redis master deletion and partition scenarios prove readiness/degraded-mode
   behavior and recovery.
10. Slim Chaos Mesh creates no dashboard, DNS server, Prometheus, PVC, or public
    Service.
11. Network delay/loss affects only selected staging pods and disappears after
    cleanup.
12. Every workflow failure path removes fault resources and verifies no residue.
13. The recommended combined topology stays inside the measured node request
    headroom.
14. Node-outage testing cannot target the shared single-worker cluster.
15. The full precommit gauntlet and relevant staging scenarios pass.

## Open Decisions

1. Whether the smoke profile runs after every staging deployment or only when a
   repository variable is enabled.
2. Which two or three module ports provide the highest-value initial Option B
   coverage.
3. Whether ingress 502 injection should use a reviewed critical-risk snippet or
   only a controlled application/upstream failure.
4. Whether Redis/Sentinel is installed per scenario or kept dormant but
   continuously deployed in staging.
5. The recovery SLOs for pod recreation, Redis promotion, relay reconnect, and
   rollout rollback.
6. The funded environment to use for the genuine two-worker node-outage test.
