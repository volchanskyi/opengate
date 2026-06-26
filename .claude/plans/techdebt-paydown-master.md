# Master Plan: Technical-Debt Paydown — Register → Micro-Plans

**Type:** Master plan / index. Breaks the [techdebt register](../techdebt.md) into
self-contained micro-plans for implementing engineers (per the review workflow).
Do **not** implement directly from this file — implement from the named micro-plan.
**Status:** Proposed — micro-plans drafted; awaiting per-plan approval.
**Branch:** `dev`.

> Micro-plans are referenced by filename (inline code) rather than links: active
> plans get renamed/archived, and the repo doc-link checker only permits links to
> `plans/archive/`. All micro-plan files live beside this one in `.claude/plans/`.

---

## 1. Decisions locked (this round)

| # | Decision | Rationale |
|---|---|---|
| Scope | Cover the **whole register** | Requester choice. |
| Ticket cache | Agent session resumption is **disk-persisted** with key-grade perms | Captures the dominant cross-restart reconnect path; in-memory already works in-process (see §2). |
| Granularity | **One micro-plan per register entry** (no combining) | Each entry gets its own file inventory, TDD steps, reviewer checklist. |

## 2. Research findings that re-scope the register (direct proofs)

These corrections came from reading the code/deps, not the register prose, and they
change two specs:

1. **Resumption is already ON (in-memory).** rustls 0.23.40 `builder.rs:172` sets
   `resumption: Resumption::default()` → `in_memory_sessions(256)`
   (`client_conn.rs:512`). The agent builds the quinn config **once**
   ([main.rs:410](../../agent/crates/mesh-agent/src/main.rs#L410)) and clones it per
   attempt, so the `Arc<dyn ClientSessionStore>` is shared and in-process reconnects
   already resume. The real gap is **persistence across process restarts** (the agent
   exits on auto-update / 10-failed-attempts / crash → systemd restart → fresh
   in-memory store → cold handshake) and **no agent-side resumption evidence**
   ([Multiscale-Readiness §4](../../docs/Multiscale-Readiness.md) demands it).
   → `td-agent-session-resumption-cache.md`
2. **A resumption ticket is a bearer credential.** The W3 server test
   ([quic_resumption_test.go](../../server/internal/agentapi/quic_resumption_test.go))
   shows a resumed session retains `PeerCertificates` — the server trusts the resumed
   mTLS identity without the client re-presenting its cert. A persisted ticket must be
   protected ≥ the client key. The client key is currently written with **no explicit
   `chmod`** ([identity.rs:103](../../agent/crates/mesh-agent-core/src/identity.rs#L103)),
   so the cache work must also harden key perms.
3. **The reconnect flap is structural and jitter is absent.**
   [reconnect_with_backoff](../../agent/crates/mesh-agent-core/src/connection.rs#L165)
   escalates delay only within one call and has no jitter; every drop path
   `continue`s the outer loop, re-entering backoff fresh at attempt 1. §4 requires
   "backoff **and jitter**." → `td-agent-reconnect-backoff-jitter.md`
4. **Perf-benchmark CI regression (#10) is already owned** by the proposed
   `benchmarks-grafana-trends.md` master plan (Go+Rust benchmark trends + Telegram
   regression alerts on VictoriaMetrics). Excluded here.

## 3. Register → micro-plan map

| Sev | Register entry | Micro-plan file | Feasibility |
|---|---|---|---|
| M | W3 — agent TLS session resumption | `td-agent-session-resumption-cache.md` | Ready (researched) |
| M | ADR-035 residual — IaC codification | `td-iac-codify-backup-bucket-iam.md` | Ready (codebase part only) |
| M | ADR-024 — 3 residual WebRTC mutants | `td-webrtc-dispatch-mutation-harness.md` | **Blocked/optional** — needs a headless WebRTC harness; decide build-vs-accept |
| L | Agent reconnect backoff/flap-guard | `td-agent-reconnect-backoff-jitter.md` | Ready (researched) |
| L | web TS pinned ^5.9.3 | `td-typescript-6-bump.md` | **Blocked upstream** — watch+bump procedure |
| L | Docker Hub authed fallback verify | `td-dockerhub-authed-fallback-verify.md` | Ready (verification) |
| **M** | Go mutation run: cap-cancellation + score stability → **shard by package** | `td-gremlins-timeout-stability.md` | Ready (investigated 2026-06-19: Go leg crosses the 100-min cap since 06-18, killing the nightly run + trend data; coefficient drives runtime; fix = shard, not raise cap) |
| L | property/fuzz test gaps | `td-property-fuzz-testing-expansion.md` (index → `td-property-fuzz-go-rapid.md`, `td-property-fuzz-rust-cargofuzz.md`, `td-property-fuzz-web-fastcheck.md`) | Subdivided into 3 track plans |
| L | perf-benchmark CI regression | — | **Owned by** `benchmarks-grafana-trends.md` |

**Not micro-plannable (user-owned, recorded in the ADR-035 entry):** external uptime
SaaS account + Cloudflare DNS `status.` retirement. These need an account/DNS action,
not code; they stay tracked in [techdebt.md](../techdebt.md).

## 4. Sequencing

1. **Storm-readiness pair** (cohesive, agent-side, §4-backed): resumption cache →
   backoff/jitter. Do together; ship behind the existing client-first cutover.
2. **Quick wins / low-risk:** Docker Hub verify, gremlins stability.
3. **IaC:** backup bucket + IAM codification.
4. **Larger / conditional:** property-fuzz expansion (opportunistic), WebRTC harness
   (only if the build-vs-accept decision is "build"), TS 6 bump (when upstream allows).

## 5. Conventions every micro-plan inherits

Per [CLAUDE.md](../../CLAUDE.md): work on `dev`; **TDD** (failing test before source);
`/precommit` before every commit, `/refactor` after; author = Ivan, no `Co-Authored-By`.
No lint/quality-gate suppressions without explicit approval
([sonarcloud.md](../rules/sonarcloud.md)). Each micro-plan ends with a `/docs` update
step where it touches behavior. Each is independently reviewable and keeps the gauntlet
green per commit.
