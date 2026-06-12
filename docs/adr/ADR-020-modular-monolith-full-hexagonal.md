# ADR-020: Modular monolith — full hexagonal style, enforced by CI

Date: 2026-05-19
Status: Accepted

## Context

OpenGate is partially modular by language: Rust is a 5-crate workspace, the Go server has 17 internal packages, the web has 11 feature folders. Modularity is enforced informally — via code review and a handful of declared interfaces (`AgentGetter`, `AMTOperator`, `CertProvider`, `notifications.Notifier`, `relay.Conn`) — but not by CI. Two structural pinch-points have accumulated:

- **God-struct `*Server`** at [`server/internal/api/api.go:74-93`](../../server/internal/api/api.go#L74-L93) — 17 fields holding every dependency; oapi-codegen mounts all handlers through one `StrictHandlerWithOptions` call at [`api.go:143`](../../server/internal/api/api.go#L143).
- **Monolithic `db.Store`** at [`server/internal/db/store.go:16-98`](../../server/internal/db/store.go#L16-L98) — 56 methods spanning 13 entity groups; imported from 51 files.

Three forces motivate making modularity explicit and enforced now:

1. **Phase 13b (Multiserver & Scaling) is the deadline.** Cross-server routing, relay pool, and Kubernetes require stable internal seams. [ADR-014](ADR-014-postgres-migration.md) explicitly defers multi-server until Phase 13a closes.
2. **Refactoring cost will never be lower than today.** Zero Critical/High coupling tech-debt, 85%+ mutation coverage across all three languages.
3. **Service-extraction optionality is cheap to preserve, expensive to retrofit.** Microservices are not committed to, but the door should stay open at near-zero ongoing cost.

The evaluation in [`.claude/plans/modular-monolith-evaluation.md`](../../.claude/plans/archive/modular-monolith-evaluation.md) compared three styles (DDD, hexagonal, package-by-feature) against verified code reality. This ADR codifies the resolved decisions.

## Decision

Adopt **full hexagonal architecture across all modules** of OpenGate. Every cross-module call goes through a named port (inbound use-case ports + outbound storage / transport / notifier ports). Enforced by three CI gates — `go-arch-lint`, `eslint-plugin-boundaries` + `dependency-cruiser`, `cargo-deny` + `cargo-modules` — all starting in **warn** mode, flipping to **error** automatically when their violation count reaches zero.

### Style: full hexagonal (overrides "layered hybrid" from the plan)

Plan §3.6 originally recommended a layered hybrid (package-by-feature floor + selective hexagonal at existing seams + DDD-lite only for Phase 13b). The 2026-05-19 resolution overrode that with **uniform hexagonal** because:

- Every existing informal seam (`AgentGetter`, `AMTOperator`, `CertProvider`, `Notifier`, `relay.Conn`) is already port-shaped with at least one real implementation. Promoting them to first-class ports is a naming-and-documentation exercise, not a redesign.
- Phase 13b will introduce network boundaries at exactly these seams. Doing the hexagonal naming now means Phase 13b becomes a transport swap, not a redesign.
- The plan's own §3.5 trade-off matrix scored hexagonal "Strong" or "Strongest" on every axis where DDD scored higher, at a fraction of DDD's discipline cost.

DDD-lite (domain events, aggregate enforcement, ubiquitous language) remains a **non-goal**. The chosen style is hexagonal, not "hexagonal + DDD."

### Earned-port rule

To prevent port over-production, a new port is justified by at least one of:

1. **Two or more consumers** of the same operation, OR
2. **Unit-test isolation need** that a concrete dependency would block, OR
3. **Process-boundary candidacy** (the operation is a Phase 13b extraction target).

No prophylactic ports. A single-consumer interface with no isolation need and no future network plans is dead weight.

### Module list

The Go server decomposes into twelve modules, each with named inbound and outbound ports:

| Module | Composed from | Inbound port | Outbound ports |
|---|---|---|---|
| `device` | parts of `db`, `handlers_devices.go`, `handlers_groups.go`, `handlers_users.go`, `handlers_security_groups.go` | `DeviceService` | `DeviceRepository`, `AuditLogger` |
| `session` | parts of `db`, `handlers_sessions.go`, parts of `agentapi`, parts of `relay` | `SessionService` | `SessionRepository`, `SessionRegistry` |
| `amt` | `internal/amt` (109 LOC, domain wrapper) | `AMTService` (promoted from existing `AMTOperator`) | `AMTTransport`, `AMTRepository` |
| `amt/transport` | renamed from `internal/mps` (3,910 LOC) | (adapter, no port) | — |
| `update` | `internal/updater` + `handlers_updates.go` + `handlers_enrollment.go` + `handlers_install.go` | `UpdateService` | `ManifestStore`, `SigningKeys` |
| `notification` | `internal/notifications` + `handlers_push.go` | `Notifier` (already exists) | (none beyond store) |
| `auth` | `internal/auth` + `handlers_auth.go` | `AuthService` | `UserStore`, `JWTSigner` |
| `audit` | parts of `db` + `handlers_audit.go` | `AuditLogger` | `AuditStore` |
| `cert` | `internal/cert` | `CertProvider` (already exists) | — |
| `relay` | `internal/relay` + `internal/signaling` | `relay.Conn` (already exists) | `SessionRegistry` (Phase 13b) |
| `transport` | `internal/protocol` + `internal/agentapi` | `Codec`, `AgentConnection` | — |
| `observability` | `internal/metrics` + future tracing | (utility, no port) | — |

The residual `api` package becomes a thin route-mounting layer. Each domain owns a `*Handlers` struct implementing its slice of the oapi-codegen `ServerInterface`; `api.NewServer` composes a top-level handler from the per-module pieces.

**AMT + MPS structure**: `internal/mps` is renamed to `internal/amt/transport` to express the transport-layer relationship. They are NOT merged — the protocol/domain boundary is preserved.

### CI gates (all start in warn mode)

1. **Go** — `go-arch-lint` (v1.15.0+). Components match the module list above; allowed-edge list; deny-by-default. Config at `.go-arch-lint.yml`. `depguard` is **not** added — `go-arch-lint` alone is the Go enforcement tool.
2. **Rust** — `cargo-deny` for dependency-direction enforcement (no `platform-*` crate may import `mesh-agent-core` internals beyond declared re-exports; no crate may add new HTTP deps without ADR amendment). `cargo-modules` snapshot test for the internal module graph of `mesh-agent-core`. Both wired into [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh) and CI.
3. **Web** — `eslint-plugin-boundaries` with three element groups (`feature`, `lib`, `app`), and `dependency-cruiser` snapshot of cross-feature edge count. Snapshot stored at `web/dependency-cruiser.snapshot.json`.

### Warn → error flip criterion

Each gate flips from warn to error at the first commit where its violation count reaches **zero**. No soak period. Per-gate independent timing — whichever clears first, flips first. Implemented via a small script (`scripts/arch-lint-flip.sh`) invoked by CI; an explicit ADR amendment can defer a flip (e.g. mid-Phase-13b kickoff).

### Exception policy

Exceptions live in the lint tool's own config file (e.g. `.go-arch-lint.yml`'s `exceptions:` block), each annotated with an ADR reference. File-level inline suppression directives are **not** introduced — the existing no-suppression rule ([`.claude/rules/sonarcloud.md`](../../.claude/rules/sonarcloud.md)) extends to architecture lint, and the write-guard hook ([`.claude/hooks/pretooluse-write-guard.sh`](../../.claude/hooks/pretooluse-write-guard.sh)) already blocks the common suppression strings.

### Implementation tempo

**Opportunistic — no greenfield refactor PRs.** Every architectural change rides on a functional change. Triggers in [plan §9](../../.claude/plans/archive/modular-monolith-evaluation.md). The first time a trigger fires post-ADR, that PR pays the small architectural tax; subsequent PRs in the module run on the new shape.

A PMAT baseline ([ADR-019](ADR-019-pmat-quality-overlay.md), PMAT plan §5.3) must be recorded **before** the first opportunistic trigger fires, so post-decomposition drift is measurable.

## Follow-up ADRs

This ADR is the only blocker. The following can land in any order once this is accepted:

- **ADR-021** — Go per-aggregate repositories; transactions threaded via `context.Context` from the use-case layer.
- **ADR-022** — Web per-feature state ownership; 11 of 12 Zustand stores move into their feature folder; only `useAuthStore` stays global.
- **ADR-023** — Relay extraction readiness; Redis-backed `SessionRegistry` outbound port. Memberlist deferred until server count > 20 or Redis Pub/Sub fanout becomes the hot path.
- **ADR-024** — Rust agent `ControlMessageHandler` trait around the inner `handle_control` fan-out at [`agent/crates/mesh-agent-core/src/session/handler.rs`](../../agent/crates/mesh-agent-core/src/session/handler.rs).

## Out of scope (explicit non-goals)

- **No microservices decomposition** in this ADR. The modular-monolith shape preserves the option without committing to it; see plan §14 for the readiness audit.
- **No event bus, message queue, or CQRS.** Hexagonal accommodates a future `EventPublisher` port additively when the first cross-service domain event appears.
- **No new programming languages.**
- **No replacement of Zustand, chi/v5, oapi-codegen, or QUIC.**
- **No big-bang refactor PR.** Incompatible with the `dev`-only branching rule.
- **No DDD-lite, no ubiquitous-language enforcement, no aggregate-root type-level constraint.**
- **No `depguard`** — `go-arch-lint` alone for Go.
- **No prophylactic ports.** Earned-port rule is mandatory.

## Consequences

**Positive.**

- Every cross-module call has a named contract. Code review can verify discipline against the contract instead of against habit.
- Phase 13b extraction at the `relay` boundary becomes a transport swap (in-process `SessionRegistry` adapter → Redis adapter), not a redesign — see ADR-023.
- The `db.Store` shrinks from 56 methods toward <30 as per-aggregate repos extract (plan §7).
- The `*Server` god-struct shrinks to a thin composition of per-module handler structs.
- Future microservice extraction (plan §14) starts ~70% complete the day the relevant module's boundaries are clean.

**Accepted trade-offs.**

- Higher discipline cost than the originally-proposed layered hybrid. Mitigated by the earned-port rule.
- Three new CI gates added to the precommit gauntlet and CI workflows. Warn-mode start absorbs initial violations; auto-flip protects against regressions once clean.
- Opportunistic-trigger model means full decomposition takes months, not weeks. Accepted — the plan explicitly rejects greenfield refactor PRs.
- The PMAT baseline is a prerequisite, not a side concern. ADR-019 plan §5.3 makes this binding.

## Amendments

### Amendment 1 — cargo-deny strict policy: HTTP inventory, duplicate allowlist, flip executed (2026-05-29)

Closes the three items §5.2/§5.4 deferred for the Rust `cargo-deny` gate.
(Formerly standalone ADR-026, consolidated here when per-file ADRs became
mutable — [ADR-036](ADR-036-mutable-adrs-current-state-doctrine.md).)

**HTTP-stack inventory (amends §5.2).** The agent's inbound HTTP surface at the
time of this amendment: direct workspace deps `reqwest` (update-manifest fetch +
GitHub sync) and `tokio-tungstenite` (WebSocket); transitive via `reqwest`
`hyper`/`hyper-rustls`/`hyper-util` + `http`/`http-body`/`http-body-util`;
transitive via `tokio-tungstenite` `tungstenite`. **Policy:** adding a new
*direct* HTTP crate (`ureq`, `isahc`, `surf`, `attohttpc`, `awc`, …) to any
workspace `Cargo.toml` requires an ADR amendment before merge; this inventory is
the canonical allowlist. `cargo-deny` cannot express "direct-only" bans
(`[bans.deny]` rejects any graph presence), so the rule is enforced via code
review on `Cargo.toml` diffs plus this policy. One explicit `[bans.deny]` keeps
`openssl` out even transitively (rustls-only policy; openssl complicates static
cross-compilation).

**Transitive-duplicate allowlist (amends §5.4).** The 28 pre-existing
`multiple-versions` warns are upstream version stagger — the RustCrypto 0.10/0.11
split, the rand/getrandom major split, the rcgen 0.13/0.14 (+ yasna) split, the
Windows API binding cohort, and single-version slack (`bitflags`, `nix`, `nom`,
`thiserror`, `webpki-roots`, …). Each is recorded in
[`agent/deny.toml`](../../agent/deny.toml) `[bans.skip]` with an `@<old-version>`
spec and a `reason`. Reviewer rule: each skip is an IOU — when an upstream bump
resolves a duplicate, drop the entry; the list drifts down only.

**Wildcard handling (amends §5.4).** The 7 `wildcards` warns are workspace
path deps using `version = "*"` (not external CVE vectors). Resolved without
weakening the gate by adding `publish = false` to all five workspace crates
(they ship via GitHub Releases per [ADR-005](ADR-005-agent-auto-update.md), never
to crates.io) and `allow-wildcard-paths = true` in
[`agent/deny.toml`](../../agent/deny.toml) — which `cargo-deny` applies only to
`publish = false` crates, so the former is a prerequisite for the latter.

**Gate flip (executes §5.4).** `multiple-versions` and `wildcards` both flip
`warn → deny` in [`agent/deny.toml`](../../agent/deny.toml); the marker
[`.claude/.markers/arch-lint-flipped/cargo-deny`](../../.claude/.markers/arch-lint-flipped/cargo-deny)
records the event, and [`scripts/arch-lint-flip.sh`](../../scripts/arch-lint-flip.sh)
gains a state-aware cargo-deny gate (`flipped`/`eligible`/`no config`; `--apply`
mutates both severities and writes the marker). All four arch-lint gates are now
strict (`depcruise`, `eslint-boundaries`, `cargo-deny`; `go-arch-lint` and
`cargo-modules` were strict at adoption).

## References

- Plan: [`.claude/plans/modular-monolith-evaluation.md`](../../.claude/plans/archive/modular-monolith-evaluation.md) (resolved 2026-05-19; §11 carries the full resolution table)
- Paired ADR: [ADR-019](ADR-019-pmat-quality-overlay.md) — PMAT baseline must precede first opportunistic trigger
- Tooling: [`go-arch-lint`](https://github.com/fe3dback/go-arch-lint), [`eslint-plugin-boundaries`](https://github.com/javierbrea/eslint-plugin-boundaries), [`dependency-cruiser`](https://github.com/sverweij/dependency-cruiser)
- Constraint sources: [`.claude/rules/sonarcloud.md`](../../.claude/rules/sonarcloud.md), [`.claude/rules/git.md`](../../.claude/rules/git.md), [`.claude/rules/tdd.md`](../../.claude/rules/tdd.md), [`.claude/hooks/pretooluse-write-guard.sh`](../../.claude/hooks/pretooluse-write-guard.sh)
- Critical pinch-points cited: [`server/internal/api/api.go`](../../server/internal/api/api.go), [`server/internal/db/store.go`](../../server/internal/db/store.go)
