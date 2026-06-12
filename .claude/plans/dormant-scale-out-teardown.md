# Master Plan: Tear Out Unused Large-Scale (Multi-Replica) Capabilities

**Type:** Master plan. It carries the full inventory and decisions so the micro-plans
are ready to work on without re-investigation.
**Status:** Broken into micro-plans (verified against the live tree). Execution order
**TD3 → TD2 → TD1 → TD4 → TD5 → TD6**:

- [`dormant-scale-out-td3-proxy-and-wiring.md`](dormant-scale-out-td3-proxy-and-wiring.md) — 1st (main.go↔backend.go interlock ⇒ before TD2)
- [`dormant-scale-out-td2-redis-backend.md`](dormant-scale-out-td2-redis-backend.md) — 2nd
- [`dormant-scale-out-td1-relay-core.md`](dormant-scale-out-td1-relay-core.md) — 3rd (highest risk: live pairing path)
- [`dormant-scale-out-td4-helm-policy.md`](dormant-scale-out-td4-helm-policy.md) — 4th
- [`dormant-scale-out-td5-ci-build-harness.md`](dormant-scale-out-td5-ci-build-harness.md) — 5th
- [`dormant-scale-out-td6-docs-adrs.md`](dormant-scale-out-td6-docs-adrs.md) — 6th (**BLOCKED** on the ADR-mutability governance flip — the write-guard hook hard-blocks in-place ADR edits)

**Empirical corrections to the §2 inventory found during breakdown:** (1) TD6's
in-place ADR amendment is **hook-blocked** until [`current-state-docs-doctrine-and-adr-mutability.md`](current-state-docs-doctrine-and-adr-mutability.md)
modifies `pretooluse-write-guard.sh`; (2) `secrets.example.yaml` currently has **no
`REDIS_*` entries**, so the TD4 removal there is a verify-only no-op.

## 0. Decision & rationale

Production is a **single free-tier node, one server replica, one connected agent**
(verified 2026-06-11; see [`docs/Multiscale-Readiness.md`](../../docs/Multiscale-Readiness.md) §1).
The multi-replica/distributed scale-out machinery (Redis registry + Sentinel HA,
cross-server relay proxy, KEDA autoscaling, PDB, multi-node L4) is **dormant, gated
off, never run on a live cluster, and a poor fit for the free tier** (4 OCPU / 24 GB,
200 GB block cap — already at the cap). We are **removing it** rather than carrying
unproven code that obscures what is real.

**The durable design is preserved without the code:** the ADRs + git history +
`docs/Multiscale-Readiness.md` (which becomes the *rebuild spec*) record the design.
When real demand approaches the Medium→Large boundary, rebuild against the kept seam.

**Kept (in use — not in scope to remove):** the `SessionRegistry` seam +
`InProcessRegistry` (live single-server path), `sharedKeys` (ON in prod, also gives
PVC-free redeploys), PostgreSQL. The **QUIC fast-path / client-first handshake** work
is **orthogonal** (agent wire protocol, not multi-replica infra) — see
[`fast-path-reconnect-fix.md`](fast-path-reconnect-fix.md); do not entangle.

> **ADR / doc immutability:** the user has directed that ADRs and docs be updated in
> place regardless of the current immutability rule — the
> [`current-state-docs-doctrine-and-adr-mutability.md`](current-state-docs-doctrine-and-adr-mutability.md)
> plan owns that governance flip. TD6 may therefore amend accepted ADRs directly;
> it does not need to supersede-via-new-file.

## 1. Cleanup completeness rule (MANDATORY — applies to every workstream)

Every removal **must also remove its tests, configs, fixtures, docs, CI references,
Makefile targets, dependencies, and policy rules** — not just the source file. A
workstream is **not done** until:

- `go build ./... && go vet ./...` and `make lint` are clean (no unused imports/deps).
- `go mod tidy` drops the now-unused modules; `go.sum` shrinks accordingly.
- `make lint-k8s` renders + validates with the removed templates/policy gone.
- A **dangling-reference sweep** returns zero in scope:
  `grep -rIE 'go-redis|miniredis|REGISTRY_BACKEND|RedisRegistry|PeerDialer|ScaledObject|multiserver|OPENGATE_PROXY|internal-listen|cross-server' server/ deploy/ .github/ policy/ scripts/ Makefile docs/`
  (excluding the rebuild-spec doc, ADRs being intentionally annotated, and this plan).
- The full `/precommit` gauntlet passes.

## 2. Verified teardown inventory

### Server (Go)
| Path | Action |
|---|---|
| `server/internal/multiserver/multiserver.go` | **Delete** the package (+ its tests). |
| `server/internal/relay/redis_registry.go` + `redis_registry_test.go`, `redis_registry_pubsub_test.go`, `redis_registry_semantics_test.go` | **Delete** all four. |
| `server/internal/relay/backend.go` + `backend_test.go` | Collapse `SessionRegistryFromConfig` to **inprocess-only**; remove `RedisConfig`/`RedisUniversalOptions` + their tests. |
| [`server/internal/relay/relay.go`](../../server/internal/relay/relay.go) | Remove `PeerDialer`, `WithPeerDialer`, `ErrSessionProxied`, the `proxied` state, and the foreign-owner branch in `Register`; collapse to **local pairing**. |
| `server/internal/relay/relay_proxy_test.go` | **Delete** (tests the proxy path). `relay_test.go` — drop proxy cases. |
| [`server/internal/relay/registry.go`](../../server/internal/relay/registry.go) + `inprocess_registry.go` | **Registry-depth decision — see §3.** |
| [`server/internal/relay/health.go`](../../server/internal/relay/health.go) | Re-scope: the Redis-outage *degraded-mode* posture (ADR-023 C3) is moot single-server; keep only what single-server readiness needs. |
| `server/internal/api/internal_relay.go` + `internal_relay_test.go` | **Delete** (cross-server proxy HTTP route + `HTTPPeerDialer`). |
| [`server/cmd/meshserver/main.go`](../../server/cmd/meshserver/main.go) + `main_test.go` | Remove `-internal-listen` flag, `internalAddr`, `peerDialer`/`NewHTTPPeerDialer`, `OPENGATE_PROXY_SECRET`, `OPENGATE_SERVER_ID`, the internal listener server, `REGISTRY_BACKEND` selection; `buildRelayOptions` loses the dialer. |
| [`server/go.mod`](../../server/go.mod) / `go.sum` | Drop `github.com/redis/go-redis/v9` (line 20) and `github.com/alicebob/miniredis/v2` (line 9) via `go mod tidy`. |
| [`server/.gremlins.yaml`](../../server/.gremlins.yaml) | Remove multiserver/redis carve-outs. |

### Helm chart
| Path | Action |
|---|---|
| `templates/redis-statefulset.yaml`, `redis-sentinel-statefulset.yaml`, `redis-service.yaml`, `redis-sentinel-service.yaml`, `redis-config.yaml` | **Delete.** |
| `templates/server-scaledobject.yaml`, `server-pdb.yaml`, `l4-tcp-udp-configmap.yaml` | **Delete.** |
| `templates/server-deployment.yaml` | Remove the `redis.enabled`-gated env (`REGISTRY_BACKEND`, `REDIS_*`, `OPENGATE_SERVER_ID`, proxy secret) + the `:9091` internal containerPort. |
| `values.yaml` | Remove the `redis:`, `autoscaling:`, `podDisruptionBudget:`, `l4:` blocks + the internal/proxy knobs. |
| `ci/test-values.yaml` | Remove the `redis:` + `autoscaling:` enablement (lines ~11/23). |
| `secrets.example.yaml` | Remove `REDIS_*` example entries. |
| `policy/k8s/security.rego` + `security_test.rego` | Remove **Rule 5 (KEDA ScaledObject)** and **Rule 6 (PodDisruptionBudget)** + their tests (header lines ~13/15, rules ~111–131). |

### CI / scripts / build / compose
| Path | Action |
|---|---|
| `.github/workflows/e2e-multiserver.yml` | **Delete.** |
| `scripts/e2e-multiserver.sh` | **Delete.** |
| `deploy/docker-compose.multiserver.yml` | **Delete.** |
| `server/tests/e2e-multiserver/` (`harness.go`, `load.go`, `main.go`, `scenario_reclaim.go`, `scenarios.go`) | **Delete** the directory. |
| [`Makefile`](../../Makefile) | Remove `e2e-multiserver` (target ~257), `load-test-multiserver` (~269) + their `.PHONY` entries (line 1). |

### Docs / ADRs (TD6 — amend in place per the mutability-flip plan)
- **ADR-023 Amendment 1** (Redis Sentinel registry) and **Amendment 2**
  (cross-server proxy) — now folded into ADR-023 (ADR-036 consolidation): mark
  both **reverted/removed** with rationale (free-tier YAGNI; design retained in
  the readiness doc).
- **ADR-034** (KEDA + shared keys): **split** — KEDA/PDB reverted; **shared keys
  stays** (live in prod).
- **ADR-023** (relay extraction / Redis registry) spine: annotate that the
  Redis-backed adapter + cross-server proxy (its two amendments) were removed;
  the port seam disposition follows §3.
- **ADR-030** (OKE/Helm): drop the "Redis/cross-server deferred to PR-C" +
  multi-node-L4 references that no longer have a target.
- [`docs/Multiscale-Readiness.md`](../../docs/Multiscale-Readiness.md): **reframe**
  §3 from "dormant capabilities (built)" to "removed — design retained here as the
  rebuild spec"; it remains the SSOT.
- [`.claude/decisions.md`](../decisions.md) rows for 023/030/034 (031/033 are now ADR-023 amendments);
  [`.claude/phases.md`](../phases.md) Phase 13b PR-C/PR-E rows.
- `docs/Architecture.md`, `docs/Kubernetes.md`,
  `docs/Testing.md`, `README.md`: remove cross-server / Redis / multiserver prose.

> **Known false positives (do NOT remove):** `scripts/tests/docker-hub-mirror.test.sh`
> and the docker-hub-mirror action mention `redis` only as an example *cached image*;
> `ADR-020`/`ADR-027` mention these in passing. Verify each grep hit before cutting.

## 3. Decision required — SessionRegistry port depth

Removing the proxy means `relay.Register` no longer resolves cross-server ownership,
so `ClaimAffinity`/`LookupOwner`/`Subscribe`/`PublishEvent` on the
[`SessionRegistry`](../../server/internal/relay/registry.go) port become unused.

- **A — Keep the full port** (smallest diff; leaves 4 unused distributed methods +
  their `InProcessRegistry` no-op impls + tests). Contradicts "rip unused."
- **B — Slim the port to single-server essentials (RECOMMENDED).** Keep the seam
  (so rebuild is cheap) but drop the distributed-only methods; `InProcessRegistry`
  shrinks to what local pairing uses. Honors "rip unused" while preserving the seam.
- **C — Remove the port entirely**, revert the relay to a direct in-memory session
  map. Maximal cut; loses the seam (rebuild from ADR-023 + git).

Recommend **B**. The TD1 micro-plan finalizes the exact method-level cut after
reading `relay.Register`'s call sites. **Hard constraint:** single-server relay
pairing behavior must be **byte-for-byte identical** before/after (it's the live
production path) — TDD: `relay_test.go` green before and after.

## 4. Workstreams (for the future micro-plan breakdown)

- **TD1 — Relay core simplification** (highest risk; live code). Local-pairing-only
  `Register`; resolve §3; `health.go` re-scope; remove `relay_proxy_test.go`.
- **TD2 — Remove Redis backend** (adapter + 3 tests + `backend.go` redis branch +
  `go.mod`/`go.sum` go-redis & miniredis).
- **TD3 — Remove cross-server proxy + multiserver pkg + `main.go` wiring**
  (`internal_relay.go` + test, `internal/multiserver/`, the flags/env/listener).
- **TD4 — Remove scale-out Helm + policy** (redis/scaledobject/pdb/l4 templates,
  values/ci/secrets blocks, deployment env, Rego Rules 5 & 6 + tests).
- **TD5 — Remove scale-out CI/test harness + build** (e2e-multiserver workflow +
  script + compose + `server/tests/e2e-multiserver/`, Makefile targets, gremlins
  carve-outs).
- **TD6 — Docs + ADRs** (amend per §2; reframe the readiness doc; decisions/phases).

## 5. Sequencing & risk

Order: **TD2 + TD3** (remove callers/backends) → **TD1** (simplify relay once nothing
depends on the proxy/registry-distributed methods) → **TD4** (chart/policy) → **TD5**
(CI/build) → **TD6** (docs/ADRs last, reflecting the final code). Each micro-plan
keeps the gauntlet green per commit.

**Risks:** (1) TD1 touches the **live relay pairing path** — behavior-preserving,
test-guarded. (2) **No wire-protocol change** and **no agent (Rust) change** — Redis/
proxy/KEDA are server+infra only, so blast radius is server + chart + CI, not the
agent fleet. (3) Removing degraded-mode/readiness (`health.go`) must not break the
existing `/healthz`/readiness probe the chart uses — keep the single-server probe.

## 6. Reviewer acceptance criteria

- [ ] Production single-server relay behavior unchanged (pairing, `/healthz`,
      smoke + E2E green minus the deleted multiserver E2E).
- [ ] `go.mod` no longer requires go-redis or miniredis; `go mod tidy` clean.
- [ ] Helm renders every overlay under `make lint-k8s` with redis/keda/pdb/l4 gone;
      Rego Rules 5 & 6 + tests removed.
- [ ] The §1 dangling-reference sweep returns zero (excluding documented false
      positives + the rebuild-spec doc).
- [ ] ADRs 023/030/031/033/034 amended; readiness doc reframed; decisions/phases
      updated.
- [ ] Full `/precommit` gauntlet green.

## 7. Execution workflow

Per [`CLAUDE.md`](../../CLAUDE.md): `dev` only; **TDD** (deleting code = delete/adjust
its tests in the same change — that satisfies the gate as a test change on the
branch); `/precommit` before every commit; `/refactor` after; author = Ivan, no
`Co-Authored-By`. `go mod tidy` + `make lint`/`lint-k8s` per workstream.
