# Phase 13b PR-D — `make e2e-multiserver` + baseline load test

**Created:** 2026-06-05 · **Parent:** [phase-13b-multiserver-scaling.md](phase-13b-multiserver-scaling.md) §4 PR-D · **Status:** Completed (D1–D5 landed). Archived 2026-06-05.

> **Outcome:** all five slices landed. The e2e driver (`server/tests/e2e-multiserver/`) passes all three scenarios against the live 2-replica + Redis + Postgres stack; load baseline recorded (proxied vs direct: p50 +70µs / p99 +633µs on loopback) → 30s affinity TTL confirmed adequate. Notable design correction vs the original plan: the owner-death scenario must **SIGKILL** the owner (not `stop`) so its Redis affinity claim survives, and must **re-seed** the session row each reclaim attempt because the non-owner's proxy teardown fires `OnSessionEnd` (deletes the row) while leaving the affinity key — see `scenarios.go`.

## Context

PR-C landed the `RedisRegistry` + cross-server WebSocket proxy + degraded-mode posture. All of it is proven at the **unit** level (miniredis, fake `PeerDialer`, `httptest` internal endpoint) but never against **two real server processes sharing a real Redis + Postgres**. PR-D closes that gap per ADR-023 §"Phase 13b integration test": a multi-container harness + three end-to-end scenarios, plus a load baseline against the 2-server cluster to settle the affinity-TTL "revisit after first load test" flag (ADR-023, plan §7).

The relay data path for **both** sides is WebSocket: `GET /ws/relay/{token}?side={agent|browser}` ([handlers_relay.go](../../server/internal/api/handlers_relay.go)). The token must exist in the shared `sessions` table; browser side needs a JWT (`?auth=`), agent side is authed by the token in the URL. Affinity owner is whichever server's `ClaimAffinity` wins first; the non-owner dials the owner's internal `:9091` listener and splices. This makes a **wire-level Go driver** the natural e2e shape — it reuses `internal/protocol` for frame encoding and drives raw WS, exactly as `tests/loadtest/main.go` drives raw QUIC.

## Design decisions

1. **Driver, not `_test.go`.** A standalone `main` package at `server/tests/e2e-multiserver/` (precedent: `tests/loadtest/main.go`). Rationale: (a) it orchestrates Docker containers (stop/start the owner, kill Redis) which is not a unit-test concern; (b) keeping it out of `*_test.go` keeps it out of the default `go test ./...` and the no-skip test-determinism guard, while still **always doing real work** when invoked — the guard's concern (false-green skips) doesn't apply because failure is `os.Exit(1)`. Mirrors how `make e2e` (Playwright) lives outside `go test`.
2. **Gating.** `make e2e-multiserver` is a dedicated target — **not** in `make test`, **not** in the precommit gauntlet (it needs a multi-minute Docker build + 4 containers). CI runs it on a schedule + `workflow_dispatch`, never on the merge-to-main critical path (mirrors `e2e-cross-browser.yml` / `mutation.yml`). The driver refuses to run unless `OPENGATE_MULTISERVER_E2E=1` so an accidental local `go run` no-ops loudly.
3. **Scenario 3 "Telegram alert fires" scope.** The alert is a Grafana rule on `opengate_registry_up == 0 for 30s` (landed in C3b/c) — it cannot literally fire without the whole monitoring stack. The e2e asserts the **two observable signals the alert keys on**: the `/metrics` `opengate_registry_up` gauge transitions 1→0 on both servers, and a brand-new session is refused with WS close **1013** (Try Again Later) once past the degraded threshold. The plan documents this as the honest boundary; the alert-rule wiring itself is already covered by the C3b/c unit/rule tests.
4. **Compose topology.** New `deploy/docker-compose.multiserver.yml`: `postgres` (shared, tmpfs), `redis` (redis:7-alpine, single node — Sentinel HA is a cluster-node concern per §6.4, out of scope for a single-host e2e), `server-a` + `server-b` (same image, `REGISTRY_BACKEND=redis`, `REDIS_ADDR=redis:6379`, `OPENGATE_SERVER_ID=server-a|server-b` so the peer dialer reaches the other by its compose DNS name on `:9091`). Public `:8080` of each server is published on distinct host ports (18081 / 18082). Degraded threshold shrunk via env for scenario 3 so the test doesn't wait 30s real-time — add an `OPENGATE_DEGRADED_THRESHOLD` env read in `main.go` wiring `relay.WithDegradedThreshold` (a tiny production-config addition, unit-tested).

## Slices (each TDD where it touches Go source; gauntlet-gated)

### D1 — degraded-threshold env knob (tiny enabling change, real TDD)
- `main.go`: read `OPENGATE_DEGRADED_THRESHOLD` (Go duration); when set, pass `relay.WithDegradedThreshold(d)`. Default unchanged (30s).
- Test first: a `main`-package-adjacent helper `parseDegradedThreshold(string) (time.Duration, bool)` in a small testable unit (e.g. `server/cmd/meshserver/config.go`) with `config_test.go` covering valid/empty/invalid. Keeps `main.go` thin and the parse logic ≥ B+.

### D2 — multiserver compose topology
- `deploy/docker-compose.multiserver.yml` (4 services above). Healthchecks gate ordering (`postgres` healthy → servers; `redis` healthy → servers). Both servers `depends_on` postgres+redis healthy.
- `make e2e-multiserver` target: `up -d --build --wait` → `go run ./tests/e2e-multiserver` → `down -v` (teardown always, even on failure — `trap`-style via `set -e; ...; rc=$?; down; exit rc` in a small `scripts/e2e-multiserver.sh` so teardown is guaranteed).

### D3 — e2e driver: three scenarios
`server/tests/e2e-multiserver/main.go` (+ small helper files), refusing to run unless `OPENGATE_MULTISERVER_E2E=1`. Reuses `internal/protocol` for frame encode/decode and a JWT minted via the public auth API (register admin → login → create a device + session row, or seed directly). Helpers: `mintSession(baseURL)` → token+jwt; `dialRelayWS(baseURL, token, side, jwt)`; `scrapeGauge(baseURL, name)`; `composeStop(svc)` / `composeStart(svc)` via `os/exec`.
- **S1 frame flow:** agent→`server-a`, browser→`server-b` (forces cross-server splice). Write N msgpack control frames each direction; assert every frame arrives intact and in order on the peer. Assert at least one server logged/served a proxied splice (observable via the `/metrics` proxied-session counter if present, else by construction: different `OPENGATE_SERVER_ID` per side).
- **S2 owner death → TTL reclaim:** establish a session, identify the affinity owner, `composeStop` it; assert both sides observe disconnect and a **fresh** session (new token) re-establishes within the affinity TTL window against the surviving server. (No live-session migration — ADR-023 non-goal; reconnect-with-fresh-token is the contract.)
- **S3 Redis death → degraded refuse:** with `OPENGATE_DEGRADED_THRESHOLD` set low (e.g. 3s), `composeStop redis`; poll `/metrics` until `opengate_registry_up == 0` on both servers; assert a new browser WS is closed with **1013**; assert an already-in-flight session (opened before the kill) keeps relaying until its own teardown (drains, not dropped). `composeStart redis` → assert recovery (gauge back to 1, new sessions accepted).

### D4 — load baseline against the 2-server cluster
- Extend the k6 relay scenario (or add `load/k6/scenarios/relay-multiserver.js`) to drive both `:18081` and `:18082`, mixing same-server (zero-hop) and cross-server (proxied) sessions. Capture p50/p99 for proxied vs direct and write the delta into the plan + a short `docs/` note. Re-run `tests/loadtest` (QUIC) against one server unchanged as the connection baseline.
- **TTL tuning result:** record whether 30s affinity TTL holds under the load run; if the proxied p99 + reclaim window argues for a different value, note the recommendation (do **not** silently change the default — that's a decision to surface).

### D5 — CI workflow + docs
- `.github/workflows/e2e-multiserver.yml`: schedule (nightly, distinct hour from the 03:00 cluster) + `workflow_dispatch`; sets `OPENGATE_MULTISERVER_E2E=1`; `make e2e-multiserver`; uploads driver logs on failure; **not** in `merge-to-main.needs[]`.
- `/docs`: new section in [docs/Kubernetes.md](../../docs/Kubernetes.md) or a `docs/Multiserver-Testing.md` linking the harness, the three scenarios, and the load baseline numbers. Update [phases.md](../phases.md) PR-D → landed; close the ADR-023 TTL-revisit item in [decisions.md](../decisions.md) / techdebt as appropriate.

## Out of scope (PR-E)
HPA, PodDisruptionBudget, per-replica session-distribution Grafana panel, relay-pool sizing.

## Verification
- `make e2e-multiserver` green locally (all three scenarios) + teardown leaves no containers.
- D1 unit tests via `go test`; full `/precommit` per commit; coverage ≥ thresholds.
- Load run produces a recorded proxied-vs-direct p50/p99 table and a TTL verdict.
</content>
</invoke>
