# Micro-Plan: Persistent Agent TLS Session-Ticket Cache

**Register entry:** [techdebt.md](../techdebt.md) — "W3 decision — adopt 1-RTT TLS
session resumption; agent-side enablement pending."
**Additional findings:** folded into "Review update" below.
**Master:** `techdebt-paydown-master.md`. **Branch:** `dev`. **Owner:** agent (Rust).
**Status:** **DEPENDENCY-BLOCKED — not permanently impossible.** The central
deliverable ("disk-persisted ticket cache surviving a process restart") is **not
buildable on the pinned rustls 0.23** (proven in §2), but the upstream barrier is
**fixed and merged** (rustls PR #2907, 2026-02-06); it is simply not yet available
through a released rustls + compatible Quinn. Ship the §4 no-regret subset now and
implement the disk store on the upstream API once released. Updated 2026-06-25.

## Review update (2026-06-25)

A follow-up review audited every claim in this plan against source and upstream.
Outcome: the pinned-dependency blocker is real, but the plan's conclusion was too
pessimistic. Folded findings:

- **Upstream is fixed, not blocked.** rustls issue
  [#2287](https://github.com/rustls/rustls/issues/2287) is the exact persistent
  `ClientSessionStore` barrier; PR
  [#2907](https://github.com/rustls/rustls/pull/2907) (merged 2026-02-06) makes
  client-cache values serializable. The accurate status is **blocked on a released
  rustls + compatible Quinn**, not "impossible." This revises §3 Option B (below)
  from "maintainers have historically declined" to "merged upstream, awaiting
  release."
- **Do not fork rustls (not Option A).** Forking a security-critical crate to write
  PSK material to disk before the upstream API ships is worse debt than the gap.
- **Restart-frequency premise overstated.** §1's "almost every reconnect crosses a
  restart" is too strong: the agent builds one Quinn config at
  [main.rs:374](../../agent/crates/mesh-agent/src/main.rs#L374) before the reconnect
  loop and clones it per attempt at
  [main.rs:455](../../agent/crates/mesh-agent/src/main.rs#L455), so ordinary drops
  resume **in-process**. A fresh in-memory store appears only when the process
  actually restarts: auto-update, explicit `RestartAgent`, watchdog rollback,
  failed-connect exit, crash, or deregistration. The in-process cache +
  observability subset (§4) is therefore more valuable than §1 implies.
- **Resumption value is real but modest** — a resumed reconnect measurably beats a
  cold mTLS handshake (repeated local medians landed near ~0.5 ms and a couple
  hundred fewer allocs/op saved), but the exact figure is machine- and run-specific
  (a single run saved only ~0.14 ms). Treat the direction as settled and the precise
  µs as unstable; it does not by itself justify a fork.
- **Server-side resumption under mTLS is proven** —
  [`quic_resumption_test.go`](../../server/internal/agentapi/quic_resumption_test.go)
  asserts `DidResume == true` with `PeerCertificates` retained.

**Revised recommendation:** ship the §4 no-regret subset now (perms hardening +
in-process resumption evidence); track PR #2907 release availability + Quinn
compatibility; implement the disk-backed store on the upstream serialization API
when released (the clean Option B path), preserving single-use-ticket semantics;
keep 0-RTT disabled pending a separate replay-safety ADR.

## Review update (2026-07-03) — empirical verification attempt + two new walls

An attempt to *empirically verify* §1's "resumption already works in-process" claim
(and to build the §4.2 evidence guard) hit two walls that sharpen this plan. The §1
claim is **asserted by construction** (shared `Arc<dyn ClientSessionStore>` + rustls
default `in_memory_sessions(256)`) but was **never observed**, and it turns out to be
**hard to observe**:

- **Wall 1 — Quinn surfaces no resumption signal (client-side).** quinn 0.11.9's
  `Connection::handshake_data()` returns only ALPN + SNI (`crypto::rustls::HandshakeData`);
  it does **not** expose rustls' `handshake_kind()` (`Full`/`Resumed`) nor a `DidResume`.
  So a QUIC-level integration test **cannot assert** the agent resumed. Only 0-RTT
  acceptance is observable (`Connection::accepted_0rtt`) — and 0-RTT stays off (replay).
- **Wall 2 — a rustls-layer in-memory reproduction is rejected before offering the PSK.**
  Driving two in-memory rustls handshakes with **one shared `Arc<ClientConfig>`** and a
  ticket-issuing server (ring `Ticketer`): the server issues 2 tickets and the client
  store's `insert_tls13_ticket` fires (tickets **are** cached), and on reconnect
  `take_tls13_ticket` fires — **yet the client still runs a full handshake**
  (`handshake_kind()==Full`, rustls debug: *"No cached session … Not resuming any
  session"*). Reproduced **with and without** mTLS client-auth, so the common gate is
  **not** the client-cert path. In `rustls::client::hs::retrieve`
  (rustls `client/hs.rs`), after `take_tls13_ticket` the ticket must pass
  `ClientSessionValue::compatible_config(verifier, client_creds)` (rustls
  `msgs/persist.rs`), which is a **`Weak::ptr_eq` identity check** on the live config's
  `ServerCertVerifier` **and** `ResolvesClientCert`. `has_expired` is ruled out (fresh
  ticket, `lifetime: 43200`s). Whether the `ptr_eq` failure is a two-independent-connections
  **harness artifact** or a real gate on the agent's shared config is **unresolved** — it
  needs the operational signal below to settle, not more static reasoning.

**Consequences for this plan:**

1. **§1 is unconfirmed, not confirmed.** Downgrade "resumption already works in-process"
   to "the config is *shaped* to resume in-process (shared store), but resumption has not
   been observed end-to-end." The only proven half is **server-side** acceptance
   ([`quic_resumption_test.go`](../../server/internal/agentapi/quic_resumption_test.go),
   `DidResume==true`).
2. **§4.2's evidence metric is insufficient as written.** Counting `take_tls13_ticket()
   == Some` **over-counts**: rustls takes the ticket and *then* rejects it at
   `compatible_config`/`has_expired`, so a taken-but-rejected ticket would be miscounted as
   a resumption. The honest signal must sit **downstream of the retrieve gate** — either
   the **server-observed `DidResume`** surfaced as a per-reconnect metric (extend the
   proven Go test into a live counter), or the client's `handshake_kind()==Resumed`, which
   Quinn does not expose (would need a rustls-layer harness or a Quinn patch). Prefer the
   server-side metric — it is the operational truth the pay-down trigger already names.
3. **New design constraint for Option B (disk store).** Even once client-session values
   are serializable (PR #2907), the `compatible_config` `Weak::ptr_eq` means a
   disk-loaded ticket cannot carry weak refs to a *previous* process's verifier/creds Arcs
   — on reload those Arcs differ and resumption is silently refused. The disk store must
   **re-hydrate loaded tickets against the current process's live verifier + client-cert
   resolver Arcs** (or the upstream API must provide a compatible re-association path).
   This is real work beyond byte (de)serialization and belongs in the §5 Option-B scope.

**Verification recipe for the deferred "do it right" (settles Wall 2 cheaply):** enable
rustls' `logging` feature and install a `log` sink in a Rust test; rustls' `retrieve`
then emits the exact rejection reason. That plus a server-side `DidResume` counter is the
minimum honest proof that the agent resumes — do this before claiming §1.

## 1. Problem (proven, not assumed)

- **Resumption already works in-process.** rustls 0.23.40 defaults
  `ClientConfig.resumption` to `in_memory_sessions(256)` (`builder.rs:172` →
  `client_conn.rs:512`); the agent builds the quinn config once at
  [main.rs:374](../../agent/crates/mesh-agent/src/main.rs#L374) and clones it per
  reconnect attempt at
  [main.rs:455](../../agent/crates/mesh-agent/src/main.rs#L455), so the
  `Arc<dyn ClientSessionStore>` is shared across reconnects.
- **It is lost only when the process restarts.** The in-memory store survives ordinary
  connection drops (they re-dial in-process, above). A restart — auto-update
  (`EXIT_CODE_RESTART`), explicit `RestartAgent`, watchdog rollback, failed-connect exit,
  crash, or server-driven deregistration — brings the agent back with a fresh in-memory
  store → a **cold** mTLS handshake. Those restart events, not every reconnect, are what
  *persistence* would cover — a narrower win than the original ask assumed.
- **A ticket is a bearer credential.** The W3 server test
  ([quic_resumption_test.go](../../server/internal/agentapi/quic_resumption_test.go))
  shows a resumed session keeps `PeerCertificates` — the server trusts the resumed
  identity without the client re-presenting its cert.
- **Key perms gap.** `agent.key` is written with no explicit mode on **both** identity
  paths — `AgentIdentity::generate`
  ([identity.rs:103](../../agent/crates/mesh-agent-core/src/identity.rs#L103)) and first
  enrollment's `PendingIdentity::generate`
  ([identity.rs:148](../../agent/crates/mesh-agent-core/src/identity.rs#L148)) — and the
  data dir is created with no explicit mode at
  [main.rs:319](../../agent/crates/mesh-agent/src/main.rs#L319).

## 2. BLOCKING FINDING — rustls 0.23 cannot serialize a client TLS 1.3 session (proven from source)

Verified against `rustls-0.23.40` + `quinn-0.11.9` in the cargo registry:

- `ClientSessionStore::insert_tls13_ticket` / `take_tls13_ticket` deal in
  `persist::Tls13ClientSessionValue` — an **opaque** value.
- That value has **no public `Codec`** (no `encode`/`read`); its byte accessors
  `ticket()` / `secret()` are **`pub(crate)`** (`persist.rs:281`/`285`); the secret is
  `Zeroizing` (deliberate forward secrecy); it embeds a `&'static` cipher-suite reference.
- rustls exposes a semver-exempt `internal::msgs` module, but its `persist` re-export
  (`lib.rs:498`) is **`ServerSessionValue` only** — the *server* value is serializable
  (stateless tickets); the **client** value is not.
- `serde` is a **dev-dependency only** in rustls — no serialization feature.
- quinn 0.11 exposes `into_0rtt()` and `handshake_data()` (ALPN/SNI), **not** the session
  ticket.

⟹ A custom `ClientSessionStore` can hold tickets **in memory and hand them back**, but
**cannot serialize them to disk by any public or internal API.** This is by design
(client-side forward secrecy). Cross-restart resumption is therefore **not implementable
on stock rustls 0.23**.

## 3. Options to fully implement — DECISION REQUIRED (requester, later)

| Option | Fully implements? | Cost / debt |
|---|---|---|
| **A. Fork/patch rustls** to expose client-session serialization + a disk-backed store | Yes | Maintaining a **fork of a security-critical crate** (supply-chain + every-upgrade burden) **and** writing PSK secrets to disk — a forward-secrecy downgrade rustls intentionally blocks. Trades the feature gap for *worse* debt. |
| **B. Implement on the upstream serialization API** (rustls PR #2907, merged 2026-02-06) | Yes, once released | Clean, no fork. The API now **exists in rustls main**; gated only on a released rustls + compatible Quinn the repo can consume — see Review update. This is the recommended long-term path. |
| **C. Decide *not* to persist client sessions; close W3 via ADR** | No (by decision) | **Zero debt** — a documented decision, not a gap. The shipped `0x14` fast-path already removes the app round-trip; the full-jitter reconnect backoff/flap-guard in [`connection.rs`](../../agent/crates/mesh-agent-core/src/connection.rs#L263) already damps reconnect storms. Residual = TLS-handshake CPU on the occasional restart, bounded. |

**Recommendation (revised by the 2026-06-25 review): ship §4 now + Option B when
released.** The marginal value (TLS-handshake CPU on a rare restart, *on top of* the
`0x14` fast-path) does not justify forking a TLS library (**A**) or weakening forward
secrecy. But it no longer needs to be closed as impossible (**C** as permanent): rustls
PR #2907 is merged, so **B** becomes a clean, fork-free path once a consumable release
lands. Ship the §4 no-regret subset now; implement the disk store on the upstream API
when available; keep 0-RTT off pending a replay-safety ADR.

## 4. No-regret subset — implementable now, independent of A/B/C

These deliver real value under **any** decision and do **not** depend on persistence:

1. **Perms hardening** — `set_permissions(0o600)` on `agent.key` after write in **both**
   `AgentIdentity::generate`
   ([identity.rs:103](../../agent/crates/mesh-agent-core/src/identity.rs#L103)) and
   `PendingIdentity::generate`
   ([identity.rs:148](../../agent/crates/mesh-agent-core/src/identity.rs#L148)); `0o700`
   on `{data_dir}` at its creation
   ([main.rs:319](../../agent/crates/mesh-agent/src/main.rs#L319)). Closes a real key/dir
   exposure. TDD: a test asserts the modes on every path.
2. **In-process resumption evidence** — measure resumption **downstream of rustls'
   retrieve gate**, not at ticket-take. Per the 2026-07-03 review, `take_tls13_ticket()
   == Some` over-counts (the ticket is taken and can still be rejected by
   `compatible_config`/`has_expired`), so an observing `ClientSessionStore` counter is
   **not** a faithful signal. Use the **server-observed `DidResume`** (extend
   [`quic_resumption_test.go`](../../server/internal/agentapi/quic_resumption_test.go)'s
   proven check into a per-reconnect metric) to satisfy the
   [Multiscale-Readiness §4](../../docs/Multiscale-Readiness.md) "observe resumption" ask
   for **in-process** reconnects (not cross-restart).

> If C is chosen, this subset *is* the deliverable. If A is chosen, it is the foundation
> the disk store builds on.

## 5. Scope by option (for when the decision lands)

- **Option A (full):** `agent/crates/mesh-agent-core/src/tls_session_cache.rs` (disk store)
  + a **vendored/patched rustls** exposing client-session (de)serialization +
  `build_quic_config` wiring (`Resumption::store`) + the §4 perms/evidence + an
  integration test (`resumption_survives_process_restart`: reload store from a
  `t.TempDir()` → assert server `DidResume == true`) + `docs/Multiscale-Readiness.md`.
  **Gate:** document the fork + the on-disk-PSK security decision in an ADR.
- **Option C (close):** new ADR (rustls client-side limitation + forward-secrecy rationale
  + why `0x14`/backoff suffice) + `decisions.md` row + remove/limit the W3 techdebt entry
  to "in-process only" + ship the §4 no-regret subset.

## 6. NFRs & trade-offs

- **Performance:** the only saving is the asymmetric TLS handshake on a restart; `0x14`
  already elides the app round-trip, so this is marginal for a small fleet.
- **Security:** persistence (A) writes a resumption PSK to disk — a forward-secrecy
  downgrade. Mitigated only partially by `0600` (the mTLS key is already on disk), but
  rustls blocks it deliberately. **0-RTT stays off** (replay) under all options.
- **Maintainability:** A adds a security-critical fork — the dominant cost. C and the §4
  subset add none.

## 7. Decision & next step

Ship the **§4 no-regret subset now** (perms hardening on both identity paths + the data
dir; in-process resumption evidence) regardless of the long-term choice. For cross-restart
persistence, **Option B is the recommended path**: rustls PR #2907 is already merged, so
the client-session serialization API exists in rustls main. The only gate is a consumable
rustls release plus a Quinn that depends on it — the repo is pinned to `rustls 0.23.40` /
`quinn 0.11.9` ([Cargo.lock](../../agent/Cargo.lock#L2806)), and current Quinn still
requires `rustls ^0.23.x`, so "blocked on a released rustls + compatible Quinn" still
holds. Concretely:

- **Now** → implement §4; land the perms/evidence work.
- **B (when a consumable release + compatible Quinn land)** → implement the disk-backed
  `ClientSessionStore` on the upstream serialization API (the §5 wiring, minus the fork),
  preserving single-use-ticket semantics; keep 0-RTT off pending a separate replay-safety
  ADR.
- **A** → not recommended: forking a security-critical crate to write PSK material to disk
  before B lands is worse debt than the gap, and B makes it unnecessary.
- **C** → only if the requester decides cross-restart persistence is not worth carrying at
  all; then close/limit the W3 entry to "in-process resumption only" via an ADR.

## 8. Sources

- rustls client API: https://docs.rs/rustls/latest/rustls/client/ ; session value is
  client-opaque by design (verified in `rustls-0.23.40` source, §2).
- quinn `Connecting`/0-RTT: https://docs.rs/quinn/latest/quinn/struct.Connecting.html ;
  0-RTT client issue: https://github.com/quinn-rs/quinn/issues/1371
