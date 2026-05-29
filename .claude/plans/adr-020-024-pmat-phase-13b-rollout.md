# ADR-020 / ADR-024 / PMAT / Phase 13b — sequenced rollout

**Created:** 2026-05-29
**Owner:** Ivan (work directive captured from session 2026-05-29)
**Status:** In progress — ADR-024 MouseHandler pilot pushed in commit pending after `c4ad7ad`

## Priority ordering

User-directed sequencing (verbatim 2026-05-29):

1. **Complete ADR-020 right now** — scope chosen: `cert + OpenAPI handler decomposition`
2. **Finish ADR-024** — all 5 remaining `ControlMessageHandler` carve-outs
3. **Fully complete PMAT** — all 4 integration points
4. **Re-evaluate Phase 13b** — fresh status against current state (k8s direct, no k3s; OCI Always-Free budget already mapped)

## Task breakdown

### 1. ADR-020 §9 completion

- [ ] **cert go-arch-lint pilot.** Add `cert` component to [`server/.go-arch-lint.yml`](../../server/.go-arch-lint.yml) with deny-all internal imports, `deepScan: off`, `_test.go` excluded. Same shape as `audit`, `auth`, `device` entries. Smallest fan-in in the codebase → safest pilot for stricter Go arch boundaries.
- [ ] **OpenAPI per-domain handler decomposition.** Per [modular-monolith-evaluation.md §4.1](modular-monolith-evaluation.md). Decompose the `StrictServerInterface` receiver:
  - Move handler methods off `*api.Server` into per-domain `*Handlers` structs (e.g. `device.Handlers`, `auth.Handlers`, `amt.Handlers`, `session.Handlers`, `updater.Handlers`, `notifications.Handlers`, …).
  - `api.NewServer` composes them via an interface-union struct that satisfies the full generated `StrictServerInterface`.
  - oapi-codegen output stays single; only the receiver structure shifts.
  - ~12 `handlers_*.go` files, ~50 method signatures, ~1000-1500 LOC reshuffle, **zero behavior change**.
  - High-risk file: anything touching `*Server` private state (`s.store`, `s.jwt`, `s.audit`, etc.) needs per-domain field threading or shared context.

### 2. ADR-024 §9 — remaining 5 handlers

Pattern established by `MouseHandler` (commit pending push): each handler is a unit struct in `agent/crates/mesh-agent-core/src/session/handlers/<name>.rs`, implementing the `ControlMessageHandler` marker trait, with associated functions taking `&Permissions` + the required dependencies explicitly. Each ships direct unit tests + an integration test under `tests/`.

- [ ] **`TerminalControlHandler`** — `TerminalResize`. ~20 LOC, trivial.
- [ ] **`SwitchHandler`** — `SwitchAck`. ~30 LOC, may merge with WebRTCHandler.
- [ ] **`KeyboardHandler`** — `KeyPress`. ~60 LOC, needs `&Option<&TerminalHandle>` threaded.
- [ ] **`FileHandler`** — `FileListRequest`, `FileDownloadRequest`, `FileUploadRequest`. ~150 LOC, async.
- [ ] **`WebRTCHandler`** — `SwitchToWebRTC`, `IceCandidate`. ~250 LOC, async + `Arc<Mutex<Option<…>>>` state. Highest mutation-coverage risk.
- [ ] **Verify mutation baseline.** After all 5 land, run `cargo mutants -p mesh-agent-core` and confirm score stays ≥89.5% (per [mutation.yml](../../.github/workflows/mutation.yml) trend store).

### 3. PMAT — 4 integration points (ADR-019)

Per [pmat-adoption-evaluation.md §4](pmat-adoption-evaluation.md).

- [ ] **Baseline run.** `pmat repo-score` + `pmat tdg` on current `dev` HEAD. Record as first datapoint in trend store + as a "Pre-decomposition baseline" entry in [phases.md](../phases.md). Satisfies ADR-020's "PMAT baseline must precede first opportunistic trigger" prerequisite (retroactively — first triggers already fired).
- [ ] **MCP server registration.** Add `pmat mcp serve` to `.claude/settings.json` with the 7-tool allow-list (TDG, repo-score, churn, entropy, duplicates, faults, Git-history RAG). 7-tool allow-list scopes the MCP surface to read-only quality data only.
- [ ] **Precommit step.** `pmat tdg --since-commit HEAD~1 --threshold B+` on changed files added to [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh). Exact pin `pmat@3.17.0` (per ADR-019 §5.5). Fails the commit if any changed file's TDG drops below B+.
- [ ] **Nightly workflow.** New `.github/workflows/pmat-trend.yml` modelled on [mutation.yml](../../.github/workflows/mutation.yml). Full-repo nightly at off-peak. Push per-language scores to Loki via SSH+docker (`scripts/pmat-loki-push.sh` mirrors `scripts/mutation-loki-push.sh`). Grafana dashboard `opengate-pmat-trend` with stat + time-series panels. Telegram alerts on:
  - **≥3-pt repo-score drop** day-over-day, OR
  - **Any single file** TDG drop below B+.
- [ ] **Kaizen disabled.** `--dry-run` only; never auto-commit (per ADR-019 §5.1 — conflicts with no-bypass commit guards).

### 4. Re-evaluate Phase 13b

Fresh look at the plan against current state. Constraints already known:

- **k8s direct** (no k3s intermediate step) — per user 2026-05-29.
- **OCI Always-Free A1.Flex budget**: 4 OCPU / 24 GB total. Currently using 2 OCPU / 12 GB. Phase 13b consumes the rest if a second VM joins.
- **ADR-023 `InProcessRegistry`** already landed (commit `1fb8713`).

To produce:

- Refreshed plan doc at [`.claude/plans/phase-13b-multiserver-scaling.md`](phase-13b-multiserver-scaling.md) (new).
- Risk re-check: Redis as critical-path dep, degraded-mode posture, latency budget.
- Sequencing: when does k8s adoption itself happen? (precondition for the rest).

## Out of scope (deferred)

- ADR-024 carving beyond the 5 handlers listed (no inner-handler trait variations).
- ADR-023 `memberlist` gossip layer (deferred until server count > 20 OR Redis Pub/Sub becomes hot path — per ADR-023).
- PMAT Kaizen `--commit --push` mode (incompatible with no-bypass commit guards — per ADR-019 §5.1).
- Phase 15 features (MFA/TOTP, API keys, Prometheus metrics surface expansion, session recordings, group permissions CRUD) — separate planning round.
