# OpenGate — Modular-Monolith Evaluation

**Status:** Resolved — ready for ADR-020 drafting (decisions captured 2026-05-19)
**Date:** 2026-05-18 (decisions resolved 2026-05-19)
**Author:** Ivan Volchanskyi (with Claude)
**Tempo:** **Plan only.** No source changes follow directly from this document. Decisions land via a sequence of ADRs (Section 9); execution is **opportunistic** — every architectural change rides on a functional change, not a greenfield refactor PR.

**Paired plan:** [`pmat-adoption-evaluation.md`](pmat-adoption-evaluation.md) — PMAT thresholds (TDG B+, repo-score ≥3-pt drop) measure the post-decomposition shape. PMAT baseline lands **before** ADR-020 ships (per PMAT plan §5.3).

---

## File-location note

The plan-mode harness originally pinned this file at `/home/ivan/.claude/plans/evaluate-the-whole-project-rustling-nova.md`. Per [`.claude/rules/plans-and-adrs.md`](../rules/plans-and-adrs.md), project plans must live under `/home/ivan/opengate/.claude/plans/`. The 2026-05-19 resolution edit also originated from the global path — the hook rejected the write; this in-repo file is the authoritative copy.

---

## 1. Context

OpenGate is a high-velocity remote device management platform (~144k LOC across Rust agent, Go server, React web; ~1,064 commits in the last 3 months; mutation-test floor 85%+; zero Critical/High coupling tech debt). The codebase is already partially modular at the language/process layer: Rust is a 5-crate workspace, Go has 17 internal packages, the web has 11 feature folders.

This plan asks: **should we make modularity an explicit, enforced first-class concern, and if so in what style?**

Three forces motivate the question now, in this order of importance:

1. **Phase 13b (Multiserver & Scaling) is the deadline.** [`.claude/phases.md`](../phases.md) lists it as High-priority and pending. Cross-server routing, relay pool, and Kubernetes require stable internal seams (`relay` extractable as a process boundary, session state with shard-key affinity). Refactoring *after* Phase 13b means refactoring across server boundaries, not packages. [ADR-014](../../docs/adr/ADR-014-postgres-migration.md) explicitly brackets Phase 13a scope and defers multi-server.
2. **Refactoring cost will never be lower than today.** Zero Critical/High coupling debt, 85%+ mutation coverage, single-server scope, well-documented seams. Acting now is cheap; acting later is expensive.
3. **Service-extraction optionality is cheap to preserve, expensive to retrofit.** We are *not* committing to microservices. We are keeping the door open at near-zero ongoing cost.

This document originally produced five proposed ADRs. After resolution it now codifies five ADRs at the next available numbers (020–024 — the originally-proposed 018–022 slots are taken by [ADR-018 OCI Bastion](../../docs/adr/ADR-018-oci-bastion-operator-access.md) and [ADR-019 PMAT](../../docs/adr/ADR-019-pmat-quality-overlay.md)). It does **not** produce code, timelines, or person-hour estimates.

---

## 2. Current state — corrected against the codebase (2026-05-19)

Three Explore-agent verification passes on 2026-05-19 corrected several numbers from the 2026-05-18 draft. **Cite reality, not the original draft.**

### 2.1 Go server

- **Already-clean seams (proof modularity is in-flight informally):**
  - `AgentGetter`, `AMTOperator`, `CertProvider` at [`server/internal/api/api.go:33-50`](../../server/internal/api/api.go#L33-L50). Each has live concrete implementations (`*agentapi.AgentServer`, `*amt.Manager`, `*cert.CA`) plus test stubs — they ARE real ports.
  - `notifications.Notifier` at [`server/internal/notifications/notifier.go:44-49`](../../server/internal/notifications/notifier.go#L44-L49) — two live impls (`PushNotifier`, `NoopNotifier`).
  - `relay.Conn` at [`server/internal/relay/relay.go:35-43`](../../server/internal/relay/relay.go#L35-L43) — used in prod for the WSConn adapter.
- **Pinch-point 1: god-struct `*Server`.** [`server/internal/api/api.go:74-93`](../../server/internal/api/api.go#L74-L93) — 17 fields holding every dependency. Domain handlers are auto-generated via oapi-codegen's `StrictHandlerWithOptions` ([`api.go:143`](../../server/internal/api/api.go#L143)); `*Server` implements the codegen `ServerInterface`. There is no per-domain `RegisterRoutes` today.
- **Pinch-point 2: monolithic store.** [`server/internal/db/store.go:16-98`](../../server/internal/db/store.go#L16-L98) — **56 methods** (verified count, not 90+ as the draft claimed) spanning 13 entity groups: Devices, Groups, Users, AgentSessions, WebPush, AMTDevices, EnrollmentTokens, Audit, DeviceHardware, DeviceLogs, DeviceUpdates, SecurityGroups, Health.
- **`internal/db` import sites: 51** (verified via `find ... | xargs grep -l "internal/db"`, not 39 as the draft claimed).
- **17 packages, dominated by `api` (12,260 LOC) and `mps` (3,910 LOC).** Stubs `multiserver` (3 LOC) and `clientapi` (2 LOC) telegraph Phase 13b work.

### 2.2 Rust agent

- **Already a 5-crate workspace** (`mesh-agent-core` 3,020 LOC, `mesh-agent` 1,891 LOC, `mesh-protocol` 352 LOC, `platform-linux` 245 LOC, `platform-windows` 229 LOC). Trait-based platform abstraction is mature.
- **Inter-crate edges:** `mesh-agent` → {core, protocol, platform-linux}; `mesh-agent-core` → `mesh-protocol`; platform crates → core. `mesh-protocol` depends on no internal crates (wire types only).
- **Single hotspot — corrected.** The draft pointed at `session/mod.rs::receive_loop` (the WebSocket message loop). The **actual** frame-type dispatch lives at [`agent/crates/mesh-agent-core/src/session/handler.rs:17-46`](../../agent/crates/mesh-agent-core/src/session/handler.rs#L17-L46) — 30 lines, 4 outer branches (Control / Terminal / Ping / wildcard). The complexity lives in the **inner** `handle_control` fan-out (~10 methods: `handle_mouse_move`, `handle_mouse_click`, `handle_key_press`, `handle_file_list`, `handle_file_download`, `handle_ice_candidate`, `handle_switch_ack`, `handle_webrtc_offer`, …). No per-control-message trait exists today.
- **No `cargo-deny` config yet** (no `agent/deny.toml`); no `cargo-modules` snapshot test.

### 2.3 Web frontend

- **11 feature folders** (verified count, not 12): `admin`, `agent-setup`, `auth`, `dashboard`, `devices`, `file-manager`, `messenger`, `profile`, `remote-desktop`, `session`, `terminal`. **None** have a `state/` subdirectory or an `index.ts` barrel today.
- **12 Zustand stores in `web/src/state/`** (verified count, not 26): admin, amt, auth, chat, connection, device, file, push, security-groups, session, toast, update.
- **81 cross-feature store imports** (verified, not 232+) — the original 232+ figure conflated import sites with usage sites. Top consumers:
  - `useAuthStore` — 20 imports
  - `useConnectionStore` — 14
  - `useDeviceStore` — 10
  - `useToastStore` — 9
  - `useUpdateStore` — 8
- **Bootstrap coupling — corrected.** The draft proposed keeping `auth`, `connection`, `toast` global as bootstrap exceptions. Reality: **only `useAuthStore` is hydration-gated** ([`web/src/App.tsx:4,9-10`](../../web/src/App.tsx#L4-L10) — `hydrated` flag blocks render). `useConnectionStore` calls `useToastStore.getState().addToast(...)` but neither gates render. The "global exception" set therefore shrinks to **`auth` only**.
- **DeviceDetail still imports 5 stores** as cited in the draft: confirmed at [`web/src/features/devices/DeviceDetail.tsx:3-7`](../../web/src/features/devices/DeviceDetail.tsx#L3-L7).
- **Zustand 5.0.12** (no Redux / Jotai / Context hierarchies). TypeScript strict mode fully on. No path aliases that would short-circuit boundary linting. No `eslint-plugin-boundaries` / `dependency-cruiser` installed yet — fully greenfield.

### 2.4 Cross-cutting

- [`api/openapi.yaml`](../../api/openapi.yaml) is the single source of truth for HTTP. Server hand-implements via oapi-codegen `ServerInterface`; web auto-generates types via `npm run generate:api`.
- The Rust agent does **not** consume the OpenAPI spec. It speaks QUIC + MessagePack via [`agent/crates/mesh-protocol`](../../agent/crates/mesh-protocol/) — verified to export only `codec`, `control`, `error`, `types` modules with no HTTP framework deps.
- The cross-language contract gate is the golden-file suite at [`testdata/golden/`](../../testdata/golden/) — already covered by [ADR-016](../../docs/adr/ADR-016-bidirectional-goldens-and-sidecars.md).

---

## 3. Three architectural styles

### 3.1 Sketch — Domain-Driven Design (bounded contexts)

Each domain owns its aggregate root (Device, Session, AMTDevice, Update, Tenant), value objects, repository, and use cases. `db.Store` is replaced by per-aggregate repositories. Cross-domain interactions go through domain events, not direct calls. Ubiquitous language is enforced at code-review time.

### 3.2 Sketch — Hexagonal (Ports & Adapters)

Each domain exposes **inbound ports** (use-case interfaces) and **outbound ports** (storage, notification, telemetry, transport). Adapters live at the edges: HTTP handler is an inbound adapter; PostgreSQL repository is an outbound adapter. This formalizes what `AgentGetter` / `AMTOperator` / `Notifier` / `relay.Conn` already do informally.

### 3.3 Sketch — Package-by-feature with enforced boundaries

Keep the current package layout. Codify the dependency direction in CI: `go-arch-lint` (Go), workspace + `cargo-deny` (Rust), `eslint-plugin-boundaries` + `dependency-cruiser` (web). No new architectural vocabulary; the rule is the artifact.

### 3.4 Same module, three styles — the AMT example

| Aspect | DDD | Hexagonal | Package-by-feature |
|---|---|---|---|
| **What's a module** | `amt` bounded context with `AMTDevice` aggregate | `amt` exposes `AMTUseCases` inbound port; outbound ports for WS-MAN, store, audit | `internal/amt/` (existing) + `internal/mps/` (with import direction enforced) |
| **DB access** | `AMTRepository` interface owned by `amt` | `AMTStorePort` interface owned by `amt`; PostgreSQL adapter implements it | Narrow per-method interface in `amt`, satisfied by `db.Store` |
| **Cross-domain** | Domain event `AMTPowerChanged` consumed by `device` and `audit` | `amt` depends on `audit.Logger` outbound port | Direct call to `audit.Log(...)`; `audit` import allowed by lint rule |
| **Test seam** | Repository fake | Port mocks | Existing test patterns (real DB in integration) |
| **Files touched to introduce** | ~20 (aggregate, repo, events, handlers, tests) | ~8 (ports + adapters split) | ~3 (lint config + minor moves) |

### 3.5 Trade-off matrix

| Axis | DDD | Hexagonal | Package-by-feature |
|---|---|---|---|
| Discipline cost | **High** (aggregates, events, ubiquitous language) | Medium (port discipline) | **Low** (rules in CI) |
| Refactor blast radius | **Large** (split `db.Store`, add event bus) | Medium (extract ports per domain over time) | **Small** (incremental rules) |
| Fit with existing code | Partial — `AMTOperator` / `Notifier` hint at it | **Strong** — existing seams *are* ports | **Strongest** — already mostly there |
| Fit for Phase 13b (multiserver) | Strong — aggregates align with shard keys | **Strong** — ports become network boundaries | Weak — no natural extraction line |
| Fit for future service extraction | **Strongest** | Strong | Weak unless layered later |
| Tooling support (Go) | Manual; no mainstream DDD libs | Manual interface discipline | `go-arch-lint`, `depguard` — mature |
| Tooling support (Rust) | Manual | Manual; trait-based naturally hexagonal | `cargo-deny`, workspace deps — mature |
| Tooling support (web) | N/A | Per-feature container / use-case split | `eslint-plugin-boundaries`, `dependency-cruiser` — mature |
| Test impact | Repository fakes per domain | Easy port mocks — lowest impact | Unchanged |
| Onboarding cost | High (vocabulary) | Medium | Low |
| Reversibility | Hard to back out | Medium | Trivial — delete the linter rule |
| Risk of over-engineering | **High** | Medium | **Low** |

### 3.6 Resolution: **full hexagonal across all modules** (Round 1, 2026-05-19)

The 2026-05-18 draft recommended a **layered hybrid** (package-by-feature floor + selective hexagonal at existing seams + DDD-lite only for Phase 13b). The 2026-05-19 resolution **overrode** that with **full hexagonal across all modules**.

Why the override:

- Every existing informal seam (`AgentGetter`, `AMTOperator`, `CertProvider`, `Notifier`, `relay.Conn`) is already port-shaped with at least one real implementation and at least one test stub. Promoting them to first-class ports is a documentation-and-naming exercise, not a redesign.
- Phase 13b will introduce network boundaries at exactly these seams. Doing the hexagonal naming now means Phase 13b becomes a transport swap, not a redesign.
- The plan's own §3.5 trade-off matrix scored hexagonal "Strong" or "Strongest" on every axis where DDD scored higher, at a fraction of DDD's discipline cost. The override pushes from "selective" to "uniform" — the additional cost is uniformly documenting and locating ports per module, not multiplying them.
- Port over-production risk (§6) is mitigated by the **earned-port rule**: a port is justified by (a) having a second consumer, OR (b) having a unit-test isolation need, OR (c) crossing a process boundary candidate (relay, agentapi). No prophylactic ports.

DDD-lite (domain events, aggregate enforcement, ubiquitous language) remains a non-goal. The chosen style is **hexagonal**, not "hexagonal-plus-DDD."

---

## 4. Target module boundaries (resolved)

### 4.1 Go server — proposed modules

| Module | Composed from | Style applied | Notes |
|---|---|---|---|
| `device` | parts of `db`, `api/handlers_devices.go`, `api/handlers_groups.go`, `api/handlers_users.go`, `api/handlers_security_groups.go` | Hexagonal — inbound `DeviceService` port, outbound `DeviceRepository` | Primary CRUD domain; shard-key candidate for 13b |
| `session` | parts of `db`, `api/handlers_sessions.go`, parts of `agentapi`, parts of `relay` | Hexagonal — inbound `SessionService` port, outbound `SessionRepository`, `SessionRegistry` (see §4.5) | Needs distributed semantics in 13b |
| `amt` | `internal/amt` (domain wrapper, 109 LOC) | Hexagonal — inbound `AMTService` port; outbound `AMTTransport` port satisfied by `mps` | `AMTOperator` interface (already in `api/api.go`) becomes the canonical inbound port |
| `amt/transport` | renamed from `internal/mps` (3,910 LOC) | Hexagonal — `AMTTransport` adapter implementing CIRA/APF/WS-MAN | **Round 1 Q3 resolution:** keep separate, restructure as transport layer of `amt`. No merge. |
| `update` | `internal/updater` + `api/handlers_updates.go` + `api/handlers_enrollment.go` + `api/handlers_install.go` | Hexagonal — inbound `UpdateService` port, outbound `ManifestStore` + `SigningKeys` ports | Loosest coupling today; cleanest pilot |
| `notification` | `internal/notifications` + `api/handlers_push.go` | Hexagonal (already done — `Notifier` is the port) | Reaffirm; no further work |
| `auth` | `internal/auth` + `api/handlers_auth.go` | Hexagonal — inbound `AuthService` port, outbound `UserStore` + `JWTSigner` ports | `tenant` model deferred to Phase 15; auth module stays single-tenant until then |
| `audit` | parts of `db` + `api/handlers_audit.go` | Hexagonal — inbound `AuditLogger` port (sink-only); outbound `AuditStore` | Single consumer pattern, but port still earned by test-isolation need |
| `cert` | `internal/cert` | Hexagonal — `CertProvider` interface (already in `api/api.go`) is the port | Already isolated; lowest fan-in — safe pilot for arch-lint |
| `relay` | `internal/relay` + `internal/signaling` | Hexagonal — `relay.Conn` is the inbound port; new outbound `SessionRegistry` port (§4.5) | **Phase 13b extraction target.** ADR-023 covers extraction AND registry. |
| `transport` | `internal/protocol` + `internal/agentapi` | Hexagonal — `Codec` and `AgentConnection` ports; partners with `mesh-protocol` crate | Wire-protocol owner |
| `observability` | `internal/metrics` + future tracing | Package-by-feature (no domain ownership; pure infra) | No port discipline needed |
| `platform` | `internal/osutil` + `internal/testutil` | Package-by-feature (utility layer; depended on by everyone) | Leaf — never imports other modules |

The residual `api` package becomes a **thin route-mounting layer**: per-domain modules expose `RegisterRoutes(r chi.Router, svc <DomainService>)`, and `api` only composes middleware + mounts. The god-struct `*Server` is decomposed into per-module sub-servers wired in [`cmd/meshserver/main.go`](../../cmd/meshserver/main.go).

**oapi-codegen interaction.** Domain handlers today are auto-generated via `StrictHandlerWithOptions` ([`api.go:143`](../../server/internal/api/api.go#L143)). The decomposition does **not** ask oapi-codegen to emit per-module interfaces. Instead, each domain module owns a `*Handlers` struct that implements the slice of `ServerInterface` belonging to that domain, and `api.NewServer` composes a top-level `ServerInterface` from the per-module handlers. The codegen output stays single; the receiver structure shifts.

### 4.2 Rust agent — `ControlMessageHandler` trait split (corrected target)

Reaffirm the existing 5 crates. **No new crates.** One internal split inside `mesh-agent-core`:

- [`session/handler.rs:17-46`](../../agent/crates/mesh-agent-core/src/session/handler.rs#L17-L46) (`handle_frame`, 30 lines, 4 outer branches) stays as the thin frame multiplexer.
- The inner `handle_control` fan-out (~10 methods on `SessionHandler` — `handle_mouse_move`, `handle_mouse_click`, `handle_key_press`, `handle_file_list`, `handle_file_download`, `handle_ice_candidate`, `handle_switch_ack`, `handle_webrtc_offer`, …) moves behind a **`ControlMessageHandler` trait**.
- Each control-message variant becomes either a dedicated impl or grouped impls (e.g. `MouseHandler` covering `MouseMove` + `MouseClick`; `FileHandler` covering `FileList` + `FileDownload`).
- **Earned by ~10 implementations** — passes the earned-port rule.

The outer 4-branch dispatch (Control / Terminal / Ping / wildcard) does NOT become a trait. Three of the four branches are 1–3 lines and one (`Frame::Control`) fans into the new trait. No outer trait is earned.

**Mutation-score guard:** the current 89.5% on `mesh-agent-core` (per [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml) Rust history) includes the present switch. Per-handler split needs equivalent test coverage **before** merge or it will trip the 85% floor.

### 4.3 Web — feature consolidation (corrected scope)

11 feature folders stay. **11 of 12 stores move into their feature folder.** Only `useAuthStore` stays global (the hydration-gate concern at [`web/src/App.tsx:4,9-10`](../../web/src/App.tsx#L4-L10)).

The rule:

- Stores live in `web/src/features/<x>/state/`, not in global `web/src/state/`.
- Each feature exports its public surface via `web/src/features/<x>/index.ts` (a barrel). Stores, components, hooks, and types are public ONLY if re-exported there.
- Cross-feature access goes through the feature's `index.ts`. The `eslint-plugin-boundaries` rule denies deep imports into `features/<x>/state/...` from outside `features/<x>/`.
- The 81 current cross-feature store imports are paid down opportunistically as features are touched. The boundary lint starts in **warn** mode (§5.4) so the 81 violations don't block CI on day one.

**Toast / connection redirection.** `useToastStore` and `useConnectionStore` are NOT bootstrap-coupled (verified — neither gates render). They each move into a feature:

- `useToastStore` → owned by a new minimal `feedback` feature OR re-exported from a `lib/feedback` non-feature folder. Decision deferred to ADR-022 because the toast surface is genuinely cross-cutting (no feature "owns" it). **Default for ADR-022:** keep at `web/src/lib/feedback/toast-store.ts` (utility, not a feature); boundaries lint exempts `lib/`.
- `useConnectionStore` → owned by the `session` feature (where the WebSocket/QUIC connection lifecycle belongs). It calls `useToastStore.getState().addToast(...)` directly — that import becomes `from '../../lib/feedback/toast-store'` (allowed by boundaries lint).

### 4.4 Cross-cutting ownership

- [`api/openapi.yaml`](../../api/openapi.yaml) is co-owned by web and Go server. Add a `CODEOWNERS` entry naming both areas. Schema changes require both reviews.
- `mesh-protocol` crate ↔ `server/internal/protocol` are paired modules. The golden-file suite ([`testdata/golden/`](../../testdata/golden/)) is the contract gate — already covered by [ADR-016](../../docs/adr/ADR-016-bidirectional-goldens-and-sidecars.md).
- **`mesh-protocol` does NOT speak HTTP** — verified against [`agent/crates/mesh-protocol/src/lib.rs`](../../agent/crates/mesh-protocol/src/lib.rs). Documented in ADR-020 as a hard rule.

### 4.5 Distributed session registry (new — Round 2 Q4)

The 2026-05-18 draft's Phase 13b extractability criterion ("`relay` extractable to a separate binary without code changes in `api`") was impossible against the current code — relay state is in-memory `sync.Map[SessionToken]*session` ([`server/internal/relay/relay.go:54-60`](../../server/internal/relay/relay.go#L54-L60)) with per-connection `Conn` pairs and no persistence.

**Resolution:** ADR-023 covers BOTH the port shape AND a distributed session registry.

- New outbound port `SessionRegistry` owned by the `session` and `relay` modules. Methods: `LoadSession`, `SaveSession`, `DeleteSession`, `NotifySessionEvent`, `ClaimAffinity(token, serverID)`.
- **Storage tech: Redis** (Round 2 Q4 resolution). Always-Free OCI container; lower latency than Postgres `LISTEN/NOTIFY` at the projected Phase 13b session counts.
- Affinity routing: each session token is owned by exactly one server (the one that claimed it first via `ClaimAffinity`). Both relay sides (agent + browser) must be routed to the owning server. Routing layer specified in ADR-023.
- The Redis adapter implements the port. In single-server mode (today), an in-process adapter satisfies the same port — Phase 13b is a config swap, not a code change.

**Operational surface added:** Redis container, backups (or persistence: AOF + RDB), connection pooling, sentinel/cluster decisions (deferred to Phase 13b). Recorded as a Medium tech-debt item in [`.claude/techdebt.md`](../techdebt.md) once ADR-023 lands.

### 4.6 Transaction boundaries (new — Round 2 Q2)

Splitting `db.Store` into per-aggregate repositories raises the question: when a handler deletes a Device, cascades its AgentSessions, and writes an AuditEvent atomically, who owns the transaction?

**Resolution (Round 2 Q2):** Use-case layer (per-domain `Service` inbound port) opens the transaction and threads it through `context.Context`. Each repo call extracts the tx from context if present, falling back to the bare DB handle otherwise.

- Pattern: `ctx, tx, err := svc.repo.BeginTx(ctx); defer tx.Rollback(ctx); ...; tx.Commit(ctx)`. Helper lives in a small `dbtx` package that wraps `pgx.Tx`.
- Repos remain simple — single-method signatures, no `Tx` parameter, no coordinator.
- Multi-aggregate writes live in the `Service` orchestrating them, not in any single repo.
- Read paths do NOT open a tx by default.

This is the standard Go hexagonal pattern. No event bus introduced.

---

## 5. Enforcement mechanisms

The plan adds **three CI gates**. All three start in **warn** mode. Flip criterion in §5.4.

### 5.1 Go — `go-arch-lint`

- Tool: [`go-arch-lint`](https://github.com/fe3dback/go-arch-lint) (v1.15.0, May 2026, actively maintained). (Round 3 Q2 resolution — `depguard` rejected as too narrow for module-level allowed-edge rules.)
- Config: `.go-arch-lint.yml` at repo root. Components match Section 4.1 (one component per module). Allowed-edge list. **Deny-by-default.**
- Run: inside [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh) (new step) AND in CI (`.github/workflows/ci.yml`).
- Exceptions: per-package exception in `.go-arch-lint.yml`'s `exceptions:` block with an inline ADR reference comment. Inline file-level suppression is **not** introduced — the existing no-suppression rule ([`.claude/rules/sonarcloud.md`](../rules/sonarcloud.md)) extends to architecture lint.

### 5.2 Rust — `cargo-deny` + `cargo-modules`

- New `agent/deny.toml` for dependency-direction enforcement (no `platform-*` may import `mesh-agent-core` internals beyond declared re-exports; no crate may add new external HTTP deps without ADR amendment).
- `cargo-modules` (`cargo-modules generate tree --package mesh-agent-core --types --traits`) snapshot test for the internal module graph of `mesh-agent-core`. Snapshot stored at `agent/crates/mesh-agent-core/tests/module-graph.snap`. Diffs require review (CI fails on unreviewed diff).
- Both tools added to [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh).

### 5.3 Web — `eslint-plugin-boundaries` + `dependency-cruiser`

- `eslint-plugin-boundaries` element groups: `feature`, `lib`, `app`. Rules:
  - `app` (`web/src/main.tsx`, `App.tsx`) may import from any group.
  - `feature` may import from `lib` and from its own siblings via the feature's `index.ts` ONLY.
  - `lib` may NOT import from `feature`.
- `dependency-cruiser` snapshot of cross-feature edge count, stored at `web/dependency-cruiser.snapshot.json`. Snapshot becomes the warn baseline; PRs that increase the count without a barrel-export justification fail.
- Run: pre-commit hook (existing `web/eslint.config.js` runs in gauntlet step 4) + CI gate.
- Exception policy: same as Go — file-level ESLint suppression directives are not introduced. The existing write-guard hook ([`.claude/hooks/pretooluse-write-guard.sh`](../hooks/pretooluse-write-guard.sh)) already blocks them.

### 5.4 Warn → error flip criterion (new — Round 2 Q1)

**Resolution:** Each gate flips from warn to error at the first commit where its violation count reaches **zero**. No soak period. Per-gate independent timing — whichever of the three gates clears first, flips first.

- The flip is automated via a small script in `scripts/arch-lint-flip.sh` invoked by CI: if the gate ran clean for the immediately-prior `dev` HEAD AND clean on the current diff, the gate runs in error mode.
- Risk: a regression introduced the same week the flip happens will fail the same commit that would have introduced it. That's acceptable — the regression is the signal.
- An explicit ADR amendment can defer a flip (e.g. mid-Phase-13b kickoff). The default is auto-flip.

---

## 6. Pitfalls (extended)

- **Splitting `db.Store` without an event mechanism creates query fan-out.** A handler that today calls `Store.GetDeviceWithSessions` becomes two repo calls and risks N+1. Mitigation: keep dedicated read-model / view queries in a separate `query` sub-package per domain (`device/query.go`). Do not force every read through the aggregate.
- **Over-strict linters block legitimate cross-cutting refactors.** Allow declared exceptions in the linter's own config file, not as file-level suppressions. ADR-020 must explicitly carve out the arch-lint exception mechanism.
- **Premature ports cost more than they save.** Earned-port rule (§3.6): a port is justified by ≥2 consumers OR test-isolation need OR process-boundary candidacy. Document the rule in ADR-020 so future PRs don't ladder up to one-impl-one-test-stub ports.
- **Web store consolidation can break bootstrap.** Only `auth` is hydration-coupled (verified). The earlier draft's worry about `toast`/`connection` was incorrect. Still: move them last to minimize risk.
- **Rust `handle_control` carve-up may regress mutation score.** The current 89.5% on `mesh-agent-core` includes the fan-out. Per-handler split needs equivalent test coverage **before** merge or it will trip the 85% floor.
- **ADR sprawl.** Five ADRs (020–024) landing in close succession risks contradiction. Section 9 ordering is single-file, single-review; ADR-020 is the only blocker; 021–024 can open in any order after.
- **Plan rot.** If no ADR is opened within 60 days of this plan's resolution being accepted, archive as `not-pursued`. **Review date: 2026-07-18.**
- **Redis as new operational surface.** ADR-023 introduces Redis as a Phase 13b critical-path dependency. Single point of failure for relay coordination. Document the recovery procedure (graceful failover to in-process registry for short outages? hard-fail?) in ADR-023.
- **`*Server` decomposition is the largest refactor in this plan.** Splitting the god-struct into per-module handler structs touches ~18 files. Mitigation: oapi-codegen's `ServerInterface` is composable — the top-level `*Server` becomes a struct embedding per-module handler structs. Each domain's handler PR carries 1–3 file moves.

---

## 7. Verification (revised)

How we will know the architecture got better, not just different:

| Signal | Source | Target |
|---|---|---|
| `go-arch-lint` allowed-edge count | CI artifact, tracked monthly | Non-increasing |
| `eslint-plugin-boundaries` violation count | CI artifact | Reaches zero (triggers warn→error flip per §5.4) |
| `dependency-cruiser` cross-feature edge count | CI snapshot | Non-increasing |
| `cargo-modules` snapshot diffs | CI snapshot | Reviewed in every PR that touches `mesh-agent-core` |
| Mutation score per component | Existing CI floor | ≥85% maintained throughout |
| `db.Store` method count | `grep` count in `store.go` | <30 after per-domain repos extract (from 56) |
| New imports of `api` from non-`api` packages | `go-arch-lint` | 0 |
| Phase 13b readiness — relay extractability | Manual gate | `relay` package + Redis-backed registry runnable as a separate binary; integration test (`make e2e-multiserver`) green |
| Build + test wall-clock | CI timing | No regression >10% |
| PMAT TDG grade distribution | [PMAT plan §7](pmat-adoption-evaluation.md) | A/B share non-decreasing during decomposition |

---

## 8. ADR sequence (renumbered)

Originally proposed 018–022. Reality: 018 = OCI Bastion, 019 = PMAT. Renumbered to **020–024**. Each is single-file and self-contained. Per [`.claude/rules/plans-and-adrs.md`](../rules/plans-and-adrs.md), revisions supersede; they do not edit.

| # | Title | Key questions answered |
|---|---|---|
| **ADR-020** | Modular monolith: full-hexagonal scope, enforcement, and exception policy | Which modules and ports (§4.1)? `go-arch-lint` + `cargo-deny`/`cargo-modules` + `boundaries`/`dependency-cruiser` configs (§5)? Warn→error flip = zero violations no soak (§5.4)? Exception policy = config-file only (§5.1)? Earned-port rule (§3.6)? |
| **ADR-021** | Go per-aggregate repositories + transactions via `context.Context` | Which aggregates (§4.1)? Tx ownership = use-case layer threading via context (§4.6)? Read-model `query` sub-packages (§6)? Residual `db.Store` size (§7)? |
| **ADR-022** | Web per-feature state ownership | Which stores move (§4.3 — 11 of 12)? Global exceptions = `auth` only (§4.3)? `toast` in `lib/feedback/` (§4.3)? Barrel-export convention (§4.3)? |
| **ADR-023** | Relay extraction + Redis-backed distributed session registry | `SessionRegistry` port (§4.5)? Redis tech choice (§4.5)? Affinity routing scheme (§4.5)? Recovery posture (§6)? Phase 13b integration test (§7)? |
| **ADR-024** | Rust agent `ControlMessageHandler` trait | Which fan-out level (§4.2 — inner control)? Trait shape? Mutation-score preservation gate (§4.2)? |

ADR-020 is the only blocker. ADRs 021–024 can be opened in any order after 020 lands.

---

## 9. Opportunistic implementation triggers

No greenfield refactor PRs. Every architectural change rides on a functional change:

| Trigger | Module | Governing ADR | First micro-step |
|---|---|---|---|
| Next AMT bug fix | `amt` + `amt/transport` (mps rename) | ADR-021 | Extract `AMTRepository` from `db.Store`; rename `internal/mps` → `internal/amt/transport`; promote `AMTOperator` to formal `AMTService` port |
| Next session-protocol change | Rust agent | ADR-024 | Carve `MouseHandler` first (largest control-message group); add the `ControlMessageHandler` trait around it |
| Next web feature touching >2 stores | Web state | ADR-022 | Move target stores into their feature folder; add barrel exports; enable `boundaries` rule in warn |
| Phase 13b kickoff | `relay` + registry | ADR-023 | Wire the in-process `SessionRegistry` port; verify the Redis adapter against a docker-compose stack; run `make e2e-multiserver` |
| Any cert-related change | `cert` | ADR-020 | Apply `go-arch-lint` rules to `cert` first (lowest fan-in — safe pilot) |
| Any new OpenAPI endpoint | per-domain handler | ADR-020 | Register via the domain's `RegisterRoutes`, not `api.Server.routes()` |
| PMAT baseline run (preceding ADR-020) | None — observability only | [PMAT plan §5.3](pmat-adoption-evaluation.md) | Run `pmat repo-score` + `pmat tdg` against current `dev` HEAD; record baseline before any module move |

The first time a trigger fires post-ADR, that PR pays the small architectural tax. Subsequent PRs in that module run on the new shape.

---

## 10. Explicit non-goals

- No microservices decomposition.
- No event bus, message queue, or CQRS introduction.
- No new programming languages.
- No replacement of Zustand, chi/v5, oapi-codegen, or QUIC.
- No big-bang refactor PR (incompatible with the `dev`-only branching rule).
- No timeline commitments. This is opportunistic.
- No person-hour estimates.
- No code snippets to copy-paste — those belong in the ADRs and the feature PRs that follow.
- **No DDD-lite, no ubiquitous-language enforcement, no aggregate-root constraint at the type level.** The chosen style is hexagonal, not "hexagonal + DDD."
- **No `depguard`** — `go-arch-lint` alone is the Go enforcement tool.
- **No prophylactic ports.** Earned-port rule (§3.6) is mandatory.

---

## 11. Resolved decisions (was: open questions for review)

Resolved 2026-05-19 across three question rounds. ADR-020 cites these values verbatim.

| # | Round | Reference | Resolved value | Original draft recommendation |
|---|---|---|---|---|
| 1 | R1 Q1 | §3.6 — architectural style | **Full hexagonal across all modules** | Layered hybrid — **overridden, more rigorous** |
| 2 | R1 Q2 | §4.5 — Phase 13b relay scope | **Add Redis-backed `SessionRegistry` to ADR-023 scope** | Port-shape only — **overridden, addresses real gap** |
| 3 | R1 Q3 | §4.1 — AMT + MPS | **Keep separate; rename `mps` → `amt/transport`** | Merge — **overridden, preserves protocol/domain boundary** |
| 4 | R1 Q4 | §4.3 — web stores | **11 of 12 stores move; only `useAuthStore` stays global** | Same intent; numbers corrected (was 26 stores → 3 global) |
| 5 | R2 Q1 | §5.4 — warn→error | **Zero current violations, no soak; per-gate timing** | "Per ADR-018" — **quantified now** |
| 6 | R2 Q2 | §4.6 — tx boundary | **Use-case layer owns; tx threaded via `context.Context`** | Not in draft — **decided now** |
| 7 | R2 Q3 | §4.2 — Rust split | **`ControlMessageHandler` trait around inner `handle_control` fan-out** | Wrong file in draft (`session/mod.rs::receive_loop`) — **corrected** |
| 8 | R2 Q4 | §4.5 — registry tech | **Redis** | Not in draft — **decided now** |
| 9 | R3 Q1 | (process) | **Edit this plan in-place** | N/A |
| 10 | R3 Q2 | §5.1 — Go lint tool | **`go-arch-lint` only** (no `depguard`) | Draft mentioned go-arch-lint; alternative rejected explicitly |
| 11 | review | §6 / §11 | **Review cadence: monthly until ADR-020 lands, quarterly thereafter** | Draft's recommendation — confirmed |
| 12 | review | §8 — ADR numbering | **020–024** (018/019 already taken by Bastion/PMAT) | Forced by reality |

**Stale-fact corrections made in §2:**

- Zustand stores: 26 → **12**
- Cross-feature store imports: 232+ → **81**
- Feature folders: 12 → **11**
- `db.Store` method count: 90+ → **56**
- `internal/db` import sites: 39 → **51**
- Rust hotspot location: `session/mod.rs::receive_loop` → **`session/handler.rs:17-46` (inner `handle_control` fan-out is the carve-up target)**
- Bootstrap-coupled stores: `auth`+`connection`+`toast` → **`auth` only**

**Cross-cutting consequences accepted:**

- Full-hexagonal discipline cost is higher than the original layered-hybrid path. Earned-port rule (§3.6) is the safety valve.
- Redis as new Phase 13b infra surface (§4.5) — recorded as a future tech-debt item once ADR-023 lands.
- Warn→error flip without soak (§5.4) means a same-week regression fails the introducing commit. Accepted.
- Plan-correction-in-place loses the historical "Proposed" version. The git history of this file is the audit trail.

---

## 12. Critical files referenced

- [`server/internal/api/api.go`](../../server/internal/api/api.go) — god-struct + oapi-codegen mounting (§2.1)
- [`server/internal/db/store.go`](../../server/internal/db/store.go) — 56-method interface (§2.1, §4.1)
- [`server/internal/notifications/notifier.go`](../../server/internal/notifications/notifier.go) — port pattern reference (§2.1)
- [`server/internal/relay/relay.go`](../../server/internal/relay/relay.go) — Phase 13b extraction target (§2.1, §4.5)
- [`agent/crates/mesh-agent-core/src/session/handler.rs`](../../agent/crates/mesh-agent-core/src/session/handler.rs) — corrected Rust hotspot (§2.2, §4.2)
- [`agent/crates/mesh-protocol/src/lib.rs`](../../agent/crates/mesh-protocol/src/lib.rs) — no-HTTP boundary (§2.4)
- [`web/src/state/`](../../web/src/state/) — 12 stores to consolidate (§2.3, §4.3)
- [`web/src/App.tsx`](../../web/src/App.tsx) — auth hydration gate (§2.3, §4.3)
- [`web/src/features/devices/DeviceDetail.tsx`](../../web/src/features/devices/DeviceDetail.tsx) — 5-store-import canary (§2.3)
- [`api/openapi.yaml`](../../api/openapi.yaml) — co-owned contract (§2.4, §4.4)
- [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh) — where `go-arch-lint` / `cargo-deny` / `cargo-modules` integrate (§5)
- [`testdata/golden/`](../../testdata/golden/) — cross-language contract gate (§2.4)
- [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml) — mutation-score guard reference (§4.2, §6)
- [`.claude/phases.md`](../phases.md) — Phase 13b deadline (§1, §6)
- [`.claude/techdebt.md`](../techdebt.md) — tech-debt register (Redis addition target)
- [`.claude/decisions.md`](../decisions.md) — ADR index (rows 020–024 added as each ADR lands)
- [`pmat-adoption-evaluation.md`](pmat-adoption-evaluation.md) — paired plan; coordinate PMAT baseline before ADR-020

---

## 13. Verification of this resolution

How to verify the resolutions are still valid before opening ADR-020:

1. Re-run the §2 reality checks (`db.Store` method count, store count, feature count, import sites) against current `dev` HEAD. If any number drifted >10 %, refresh §2 before drafting ADR-020.
2. Confirm PMAT baseline ([PMAT plan §5.3](pmat-adoption-evaluation.md)) has run and recorded TDG + repo-score. ADR-020 work must not start without a baseline.
3. Confirm none of the modules in §4.1 have been merged/split by intervening PRs.
4. Confirm the §11 resolution table has no contested rows. If the user revises a decision, edit the table and rebuild the affected ADR section.
5. **Review date: 2026-07-18.** If no ADR-020 PR has opened by then, archive this plan as `not-pursued`.

---

## 14. Microservice-migration readiness

Forward-looking audit (2026-05-19). This plan's stated non-goal (§10) is "No microservices decomposition." This section answers the separate question: *if and when we later choose to extract services and move to Kubernetes, how much of that work has this plan already paid for?*

**Verdict: ~70 % of the way.** The plan sets up the **boundaries** correctly for the canonical strangler-fig migration. It deliberately does NOT solve the remaining 30 % (event bus, per-service DB, per-service OpenAPI, etc.), which are decisions best made at the moment of each extraction because the answers depend on which service is being extracted and what data it owns.

### 14.1 Readiness scorecard

| Dimension | Readiness | Why |
|---|---|---|
| Module → service extraction (boundary clarity) | **High** | Full hexagonal (§3.6) + per-aggregate repos (§4.1) + named ports |
| Sync RPC migration (in-process → HTTP/gRPC) | **High** | Outbound ports become network adapters with zero use-case-layer change |
| Per-domain handler bundle | **High** | Per-module `RegisterRoutes` (§4.1); lifting a domain to its own binary = removing one mount point |
| Relay extraction (Phase 13b specific) | **Very High** | Already in scope as ADR-023 with `SessionRegistry` port + Redis adapter |
| AMT gateway extraction | **High** | `amt` (domain) + `amt/transport` (MPS) split is the seam |
| Frontend independence (BFF / micro-frontend later) | **Medium** | Per-feature stores (§4.3) enable feature-scoped frontends; no per-feature build target yet |
| CI boundary verification | **High** | `go-arch-lint` allowed-edge graph (§5.1) IS the would-be service boundary map |
| Quality signal during migration | **High** | PMAT TDG grade per file + repo-score trend ([PMAT plan](pmat-adoption-evaluation.md)) surface boundary erosion |
| Async event propagation | **Medium** | Hexagonal accommodates it cleanly via a future `EventPublisher` port; plan explicitly defers the bus (§10) |
| Database-per-service | **Low** | Single Postgres; tx-via-context (§4.6) assumes shared DB |
| Cross-service consistency (sagas / outbox) | **Low** | Not addressed; future ADR per extraction |
| API spec per service | **Low** | Single [`openapi.yaml`](../../api/openapi.yaml); needs split at first user-facing service extraction |
| Per-service CI / deployment pipeline | **Low** | [`.github/workflows/build-image.yml`](../../.github/workflows/build-image.yml) builds one image; per-service workflows needed |
| Distributed tracing | **Low** | `observability` module (§4.1) mentions future tracing; not committed |
| Service-to-service security (mTLS / service mesh) | **Low** | N/A in monolith; deferred until ≥2 services exist |
| API gateway / BFF | **Low** | Caddy terminates TLS today; can grow into a gateway when needed |

### 14.2 Deliberately deferred — handled at extraction time, not now

For each gap below: the design is NOT pre-committed in this plan because the answer depends on which service is being extracted and what data it owns. Each is added by the extraction PR's own ADR.

| Gap | When it lands | Likely shape |
|---|---|---|
| **Event bus / async messaging** | First extraction that publishes a domain event (e.g. `AMTPowerChanged` consumed by both audit + device services) | New ADR. Outbound `EventPublisher` port per service. Concrete backend (NATS / Redis Streams / Postgres LISTEN/NOTIFY) chosen at that ADR. Additive — hexagonal absorbs it cleanly. |
| **Cross-service consistency** | First multi-aggregate write spanning service boundaries | New ADR. Most likely: outbox pattern (atomic local write + publisher worker reads outbox table and emits). Sagas as the alternative for long-running flows. |
| **Database-per-service** | First service extraction | Each service owns its schema (single-instance) or its own Postgres (separate instance). Migration script per cutover. The residual `db.Store` for that service → 0 (it now owns its own repo against its own DB). |
| **Per-service OpenAPI** | First user-facing service extraction | Split `api/openapi.yaml` into `api/<service>/openapi.yaml`. The browser hits a gateway / BFF that routes; the agent still talks to ONE edge (the relay service). |
| **Per-service CI / deployment** | First service extraction | Per-service `build-image.yml` and `cd.yml`. Dependency graph in a top-level orchestrator workflow. Cosign signing ([ADR-009](../../docs/adr/)) extends naturally. |
| **Distributed tracing** | Before the second service extracts | OpenTelemetry SDK in each service; collector → Tempo or Jaeger; Grafana dashboard alongside existing Prometheus + Loki panels. |
| **API gateway / BFF** | First user-facing service extraction | Either (a) Caddy grows path-based routing rules, or (b) a thin Go BFF binary sits in front of services. (a) preferred for simplicity. |
| **Service mesh / mTLS** | When ≥2 services run in production | Kubernetes-native (Linkerd or Istio). Could defer to plaintext over a private subnet for early Phase 13b if the security review allows. |

### 14.3 Per-extraction checklist

When service N extracts from the monolith, the PR pays this checklist:

- [ ] Module has clean port surface (verified by `go-arch-lint` allowed-edge graph)
- [ ] Module's repo has its own DB schema (or its own Postgres instance — decide per service)
- [ ] Module's slice of `openapi.yaml` extracted to `api/<service>/openapi.yaml`
- [ ] New service binary in `cmd/<service>/main.go` wiring the same ports to the same adapters
- [ ] Caddy / gateway routing rule added for service's path prefix
- [ ] `build-image.yml` and `cd.yml` cloned and parameterized for the service
- [ ] OpenTelemetry tracing added if this is service ≥ 2
- [ ] Smoke test (in `make e2e-multiservice`) verifying the extracted service answers the same requests the monolith did
- [ ] Monolith's `RegisterRoutes` call for this module removed
- [ ] Residual `db.Store` shrinks by the methods that moved with the service
- [ ] New ADR documents the extraction decision and concrete tech picks

The first extraction is the most expensive (it sets up gateway routing, distributed tracing, per-service CI patterns). Subsequent extractions reuse the patterns.

### 14.4 Future-work bullet: residual `db.Store` → 0

Plan §7 sets the target "`db.Store` <30 methods after per-domain repos extract." That's correct for the monolith. For the microservice end state, the target is **zero** — each service owns its data via its own repo against its own DB. The intermediate target (<30) is a checkpoint, not the destination. ADR-021 should note this so the residual `db.Store` isn't accidentally re-grown.

### 14.5 What would CHANGE in this plan if microservices were the goal today

Concretely: nothing structural would change. The same hexagonal seams, the same per-aggregate repos, the same `SessionRegistry`, the same `RegisterRoutes` decomposition. Only the *triggers* would change — instead of "next AMT bug fix → extract `AMTRepository`," it would be "AMT extraction PR → extract `AMTRepository` + new DB schema + new `cmd/amt/main.go` + routing + tracing + ADR."

That structural stability is the point. The plan picks the modularity style (full hexagonal) that minimises rework whether the future holds a tighter monolith or full microservices. The cost of optionality is the slightly-higher discipline cost of port discipline applied uniformly — accepted in §3.6.
