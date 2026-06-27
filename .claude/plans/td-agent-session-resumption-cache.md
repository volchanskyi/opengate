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
  restart" is too strong: the agent builds one Quinn config before the reconnect
  loop and clones it, so ordinary drops resume **in-process**. Restarts are visible
  only for update/explicit-restart/watchdog/deregister/crash. The in-process
  cache + observability subset (§4) is therefore more valuable than §1 implies.
- **Resumption value is real but modest** — local benchmark: resumed reconnect
  ~278 µs faster, ~213 fewer allocs/op vs cold mTLS. Supports the direction; does
  not by itself justify a fork.
- **Server-side resumption under mTLS is proven** —
  [`quic_resumption_test.go`](../../server/internal/agentapi/quic_resumption_test.go)
  asserts `DidResume == true` with `PeerCertificates` retained.

**Revised recommendation:** ship the §4 no-regret subset now (perms hardening +
in-process resumption evidence); track PR #2907 release availability + Quinn
compatibility; implement the disk-backed store on the upstream serialization API
when released (the clean Option B path), preserving single-use-ticket semantics;
keep 0-RTT disabled pending a separate replay-safety ADR.

## 1. Problem (proven, not assumed)

- **Resumption already works in-process.** rustls 0.23.40 defaults
  `ClientConfig.resumption` to `in_memory_sessions(256)` (`builder.rs:172` →
  `client_conn.rs:512`); the agent builds the quinn config once at
  [main.rs:410](../../agent/crates/mesh-agent/src/main.rs#L410) and clones it, so the
  `Arc<dyn ClientSessionStore>` is shared across reconnects.
- **It is lost on every process restart.** The agent exits on auto-update
  (`EXIT_CODE_RESTART`), on 10 failed attempts, and on crash; systemd restarts it with a
  fresh in-memory store → a **cold** mTLS handshake. In production almost every reconnect
  crosses a restart, so the in-memory store rarely helps — hence the original ask for
  *persistence*.
- **A ticket is a bearer credential.** The W3 server test
  ([quic_resumption_test.go](../../server/internal/agentapi/quic_resumption_test.go))
  shows a resumed session keeps `PeerCertificates` — the server trusts the resumed
  identity without the client re-presenting its cert.
- **Key perms gap (pre-existing).** The client key is written with no explicit mode
  ([identity.rs:103](../../agent/crates/mesh-agent-core/src/identity.rs#L103)).

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
| **C. Decide *not* to persist client sessions; close W3 via ADR** | No (by decision) | **Zero debt** — a documented decision, not a gap. The shipped `0x14` fast-path already removes the app round-trip; `td-agent-reconnect-backoff-jitter.md` reduces needless restarts. Residual = TLS-handshake CPU on the occasional restart, bounded. |

**Recommendation (revised by the 2026-06-25 review): ship §4 now + Option B when
released.** The marginal value (TLS-handshake CPU on a rare restart, *on top of* the
`0x14` fast-path) does not justify forking a TLS library (**A**) or weakening forward
secrecy. But it no longer needs to be closed as impossible (**C** as permanent): rustls
PR #2907 is merged, so **B** becomes a clean, fork-free path once a consumable release
lands. Ship the §4 no-regret subset now; implement the disk store on the upstream API
when available; keep 0-RTT off pending a replay-safety ADR.

## 4. No-regret subset — implementable now, independent of A/B/C

These deliver real value under **any** decision and do **not** depend on persistence:

1. **Perms hardening** — `set_permissions(0o600)` on `agent.key` after write
   ([identity.rs:103](../../agent/crates/mesh-agent-core/src/identity.rs#L103)); `0o700`
   on `{data_dir}`. Fixes a genuine pre-existing exposure. TDD: a test asserts the modes.
2. **In-process resumption evidence** — wrap rustls' `ClientSessionMemoryCache` in an
   observing `ClientSessionStore` that logs + increments a counter when
   `take_tls13_ticket` returns `Some`. Satisfies the [Multiscale-Readiness §4](../../docs/Multiscale-Readiness.md)
   "observe resumption" ask for **in-process** reconnects (not cross-restart).

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

Requester to choose **A / B / C** (recommendation: C). On decision:
- **C** → implement §4 + write the ADR; close/limit the W3 entry.
- **A** → spike the rustls patch surface first (confirm minimal exposure needed), then
  implement §5-A with the ADR security note.
- **B** → open the upstream proposal; park this plan until it lands.

## 8. Sources

- rustls client API: https://docs.rs/rustls/latest/rustls/client/ ; session value is
  client-opaque by design (verified in `rustls-0.23.40` source, §2).
- quinn `Connecting`/0-RTT: https://docs.rs/quinn/latest/quinn/struct.Connecting.html ;
  0-RTT client issue: https://github.com/quinn-rs/quinn/issues/1371
