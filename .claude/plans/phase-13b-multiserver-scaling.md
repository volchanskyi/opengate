# Phase 13b — Multiserver & Scaling (re-evaluation)

**Created:** 2026-05-31 (re-evaluation of the long-standing High-priority Phase 13b entry)
**Status:** Planning — not started. Supersedes the stale one-line phases.md entry ("Cross-server routing, relay pool, Kubernetes").
**Drivers:** ADR-023 (Redis session registry) is the architectural spine; this plan is the execution sequencing against *current* state and constraints.

## 1. What changed since Phase 13b was first parked

- **ADR-023 landed the seam, dormant.** The [`SessionRegistry`](../../server/internal/relay/registry.go) outbound port + [`InProcessRegistry`](../../server/internal/relay/inprocess_registry.go) adapter exist and are unit-tested — but they are **not wired into the live relay path**. [`relay.go`](../../server/internal/relay/relay.go) still uses its in-memory `sync.Map` directly; `cmd/meshserver/main.go` and `api.go` never construct the registry. The port is a seam waiting to be put on the hot path.
- **`multiserver` / `clientapi` are empty stub packages** ([`multiserver.go`](../../server/internal/multiserver/multiserver.go), [`clientapi.go`](../../server/internal/clientapi/clientapi.go)) — package docs only, no code.
- **k3s is off the table.** Decision (2026-05-29): go **directly to k8s**, no intermediate single-node distros. The microservice pilot targets real k8s.
- **Observability path is proven.** CD Phase D (VictoriaMetrics/Grafana/Loki/Promtail/Telegram) plus the mutation-trend and now PMAT-trend (ADR-019/028) workflows demonstrate the SSH+docker→Loki pattern and Grafana dashboards a multiserver rollout will lean on.
- **Deployment is still single-VM docker-compose + Caddy** (CD Phase A–C). No k8s manifests exist in `deploy/` yet.

## 2. Constraints (hard)

- **OCI Always-Free A1.Flex budget: 4 OCPU / 24 GB total across the tenancy.** Currently consumed: **~2 OCPU / 12 GB** by the single-VM stack (server + Postgres + monitoring + Caddy). **Remaining headroom: ~2 OCPU / 12 GB.** Phase 13b (k8s + ≥2 server replicas + Redis + the existing Postgres/monitoring) does **not** fit in the *remaining* headroom alongside the current VM — it requires re-allocating the budget (see §5 sequencing).
- **k8s direct** (no k3s/microk8s).
- **No-bypass commit/push guards, TDD, SonarCloud merge gate** all still apply — every PR below runs the full gauntlet.
- **Single region** at launch (ADR-023 §"Out of scope").

## 3. Goals / non-goals

**Goals.** Cross-server session routing (agent on server A ↔ browser on server B); a relay pool that survives a single server's death (TTL reclaim); horizontal scale on k8s within the Always-Free budget; `make e2e-multiserver` proving it.

**Non-goals (deferred — ADR-023 §"Out of scope").** Redis HA/sentinel/cluster; memberlist gossip (until >20 nodes or Pub/Sub becomes hot path); cross-server *live agent migration*; multi-region; any auth scheme beyond the existing mTLS + Caddy/ACME.

## 4. Work breakdown (sequenced PRs)

> Ordering rationale: prove each seam on the *live path* with the cheap adapter before adding the expensive dependency. Wire the registry (InProcess) → containerize for k8s → swap to Redis → scale.

### PR-A — Put `InProcessRegistry` on the live relay path (no behavior change)
ADR-023 deferred *wiring* the adapter. Do it now, single-server: `cmd/meshserver/main.go` constructs `NewInProcessRegistry`, injects it into `Relay`; `relay.go` routes `Register`/lookup/teardown through the `SessionRegistry` port instead of touching `sync.Map` directly. This is the de-risking step — it forces the relay's session lifecycle to express itself in `ClaimAffinity`/`LookupOwner`/`SaveSession`/`DeleteSession`/`Publish`/`Subscribe` terms while the implementation is still trivially in-process. **Behavior identical; a single new integration test asserts parity.** Gate: existing relay + relay-faults integration suites stay green.

### PR-B — k8s adoption (the precondition for everything multi-)
Containerize the stack onto k8s. **Key open decision (§6): OKE vs self-managed kubeadm.** Recommended: **OKE** — the OKE control plane is free; worker nodes are the Always-Free A1.Flex, so it fits the budget and avoids hand-rolling a control plane on a 4-OCPU envelope. Deliverables: `deploy/k8s/` manifests (or a Helm chart) for server Deployment + Service + Caddy/ingress + Postgres (StatefulSet or keep the managed/colocated DB) + monitoring; a CD path that deploys to the cluster. Single replica first — this PR is "same app, now on k8s," not "scaled."

### PR-C — `RedisRegistry` adapter + cross-server proxy (the ADR-023 Phase-13b PR)
Implement `RedisRegistry` satisfying `SessionRegistry`: `SETNX`-based `ClaimAffinity` with TTL, Pub/Sub for `Publish/Subscribe`, metadata in Redis hashes. Wire the **cross-server WebSocket proxy** (losing-affinity server proxies frames to the owner; `X-OpenGate-Proxy: serverID` header; same `relay.Conn` wire format). Backend selected by `REGISTRY_BACKEND=redis|inprocess` (config swap, per ADR-023 §"affinity routing"). Add a Redis pod/Service to `deploy/k8s/`. **This is the one PR ADR-023 scoped the extraction to.**

### PR-D — `make e2e-multiserver` + baseline load test
Per ADR-023 §"Phase 13b integration test": two server containers + one Redis + one shared Postgres. Three scenarios — (1) A-agent/B-browser frames flow; (2) owner killed → reclaim within TTL → both reconnect; (3) Redis killed → in-flight drains, new sessions rejected, **Telegram alert fires**. Gated behind `OPENGATE_MULTISERVER_E2E=1` so it stays out of the default precommit run. Re-run the k6 load test (Phase 12) against the 2-server cluster for a latency/throughput baseline; record the TTL tuning result.

### PR-E — scale-out policy
HPA on the server Deployment (CPU + a custom `opengate_active_sessions` metric already in the monitoring surface); PodDisruptionBudget; relay-pool sizing within budget. Grafana panel for per-replica session distribution. This is the "pool" half of "relay pool."

## 5. Sequencing & budget math (the real tension)

The current VM uses ~2/12 of 4/24; the cluster cannot run *beside* it at the target replica count. Two migration paths:

- **Path 1 — pilot-then-cutover (recommended).** Stand up OKE worker(s) on the *remaining* ~2 OCPU / 12 GB as a pilot (single replica + Redis, no monitoring duplication — point it at the existing VM's Loki/Grafana over the VCN). Prove PR-C/PR-D there. Then cut DNS over and reclaim the old VM's 2 OCPU / 12 GB to grow the cluster to the full 4/24. Brief window of two parallel stacks; neither is at full replica count, so the 4/24 ceiling holds.
- **Path 2 — in-place convert.** Convert the existing VM into the first k8s node and redeploy the stack onto it. No spare-capacity pilot, but a harder rollback story and a maintenance window on prod.

Budget at steady state (full 4/24 to the cluster): 2–3 server replicas (~0.5 OCPU / 1–2 GB each), Redis (~0.25 OCPU / 0.5–1 GB), Postgres (keep colocated or move to its own node), monitoring. **Tight but feasible for a 2–3 replica pilot.** Postgres placement (in-cluster StatefulSet vs the current colocated container vs a future managed DB) is the biggest budget swing — flag for the user.

## 6. Decisions — RESOLVED 2026-06-01

1. **k8s flavor: OKE.** Free control plane; workers on the Always-Free A1.Flex. (kubeadm rejected — control plane eats the 4-OCPU budget; non-OCI managed rejected — leaves the free tier.)
2. **Postgres placement: in-cluster StatefulSet + PVC** (OCI block volume via the `oci-bv` CSI driver). k8s-native, survives node reschedule, plays with node-pool upgrades; the in-place convert migrates data via a one-time `pg_dump`→restore onto the new PVC (Phase 13a ships that path). Node-pinned colocated pod rejected (node-local SPOF that fights node-pool upgrades); OCI Managed Postgres rejected (not Always-Free).
3. **Migration path: in-place convert** — the existing prod VM becomes k8s node 1 and the stack redeploys onto it. Note: even with in-place compute, the DB data still does a `pg_dump`→restore→cutover onto the StatefulSet PVC (a mini data-side pilot-then-cutover).
4. **Redis: Sentinel HA from the start** (1 primary + 2 replicas + 3 sentinels, pod anti-affinity), chosen over the staged ephemeral→Sentinel path. **Caveat to honor in PR-C/PR-E:** Sentinel auto-failover only yields *real* HA once the cluster spans **≥2–3 nodes** (anti-affinity must place primary/replicas/sentinels on separate nodes); on the single-node in-place-convert pilot it gives no node-failure protection. So sequence node-pool growth to ≥2 nodes *with* enabling Sentinel. This supersedes ADR-023's "HA out of scope" — record a new ADR (superseding ADR-023's Redis-HA deferral) when PR-C lands the `RedisRegistry`.

## 7. Risk re-check (vs ADR-023 §"Recovery posture")

- **Redis as critical-path dependency.** Mitigated by the degraded-mode posture already specified: <30s outage → in-flight sessions continue, new sessions fail *open* (rejected, not split-brain); >30s → degraded mode + Telegram alert; `/healthz` reports Redis status so the k8s readiness probe / ingress drains the pod. **PR-C must implement `/healthz` Redis reporting and the readiness probe wiring**, not just the happy path.
- **Latency budget.** Cross-server proxy adds one intra-VCN hop for A-agent/B-browser sessions. Affinity routing means same-server sessions take zero extra hops. PR-D's load test must record the proxied-session p50/p99 delta and confirm the 30s TTL is sane under load (ADR-023 flagged TTL as "revisit after first load test").
- **New operational surface (Redis).** Backups (AOF+RDB), connection pooling, eventual sentinel/cluster. **Add a Medium tech-debt entry** when PR-C ships (ADR-023 §"Accepted trade-offs" called for this; it was never recorded in [techdebt.md](../techdebt.md) — fix on PR-C).
- **k8s itself is new operational surface.** Manifests, ingress, secrets management (CD Phase E "secrets management / network policies" is still deprioritized — Phase 13b may force part of it). Cluster upgrades, node pool management.

## 8. Feeds into phases.md

The Planned-section Phase 13b row is repointed at this plan. Suggested refreshed note: *"Sequenced A–E: wire InProcessRegistry live → k8s (OKE) → RedisRegistry + cross-server proxy → e2e-multiserver + load test → HPA. Budget-constrained to 4 OCPU/24 GB; pilot-then-cutover. Open decisions: k8s flavor, Postgres placement, migration path, Redis durability."*

## 9. References

- [ADR-023](../../docs/adr/ADR-023-relay-extraction-redis-session-registry.md) — the registry decision + recovery posture + memberlist deferral (this plan is its execution arm)
- [ADR-020](../../docs/adr/ADR-020-modular-monolith-full-hexagonal.md) — hexagonal seams that make the relay extractable
- Current seam: [`registry.go`](../../server/internal/relay/registry.go), [`inprocess_registry.go`](../../server/internal/relay/inprocess_registry.go)
- Budget precedent: [ADR-018](../../docs/adr/ADR-018-oci-bastion-operator-access.md) (Always-Free operational simplicity)
- Load-test baseline: Phase 12 k6 harness
