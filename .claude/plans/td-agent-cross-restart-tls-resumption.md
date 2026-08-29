# Micro-Plan: Cross-Restart TLS Session Resumption (BLOCKED)

**Status:** **BLOCKED ON AN UPSTREAM RELEASE — do not start.** The
deliverable is not buildable on the pinned dependencies, and the barrier is
not ours to remove. **Branch:** `dev`. **Owner:** agent (Rust).
**Register entry:** [`techdebt.md`](../techdebt.md) — "Cross-restart TLS
resumption is blocked on a rustls release".
**Sibling:** the `td-agent-key-perms-and-resumption-evidence` micro-plan
carries the half that *is* implementable, and is not gated on this.
**Verified against the working tree 2026-08-29.**

The agent resumes TLS **within a process** — one `rustls::ClientConfig`,
so one `Arc<dyn ClientSessionStore>`, spans every reconnect. What it cannot
do is carry a ticket **across a process restart**: a fresh process starts
with an empty in-memory store and pays a full mTLS handshake. Persisting
that store to disk is what this plan is for, and it needs an upstream API
that has been written but not released.

---

## 1. The gap, stated narrowly

Ordinary connection drops re-dial **in process** and are already covered
(the config is built once at
[`main.rs:432`](../../agent/crates/mesh-agent/src/main.rs#L432) and cloned per
attempt at [`main.rs:599`](../../agent/crates/mesh-agent/src/main.rs#L599); the
clone shares `crypto: Arc<dyn ClientConfig>`).

What loses the store is a **process restart**: auto-update, an explicit
`RestartAgent`, watchdog rollback, a failed-connect exit, a crash, or
deregistration. Those events — not every reconnect — are the whole
population this plan would cover.

## 2. Why it is blocked (verified from source, not from changelogs)

| # | Fact | Evidence |
|---|---|---|
| B1 | The repo is pinned to **rustls 0.23.43** and **quinn 0.11.11**. | [`Cargo.lock`](../../agent/Cargo.lock); [`Cargo.toml:30-31`](../../agent/Cargo.toml#L30) |
| B2 | On 0.23.x the TLS 1.3 client session value is **opaque**: no public `Codec`, and `ticket()` / `secret()` are `pub(crate)`. The secret is `Zeroizing` and the value embeds a `&'static` cipher-suite reference. | `rustls-0.23.43` `src/msgs/persist.rs:76-90,284-288` |
| B3 | `internal::msgs` re-exports the **server** session value only; the client value is not reachable by any public or internal path. `serde` is a dev-dependency. | `rustls-0.23.43` `src/lib.rs`; its `Cargo.toml` |
| B4 | ⟹ A custom `ClientSessionStore` on the pinned version can hold tickets in memory and hand them back, but **cannot serialize one by any API**. This is deliberate client-side forward secrecy, not an oversight. | B2 + B3 |
| B5 | The newest published **stable** rustls is **0.23.43** — the pinned one. A `0.24.0-dev.1` prerelease exists. | crates.io API, 2026-08-29 |
| B6 | quinn cannot follow rustls to 0.24 yet: `quinn-proto 0.11.15` — inside quinn **0.11.11**, the newest published quinn — requires `rustls = "0.23.5"`. | `quinn-proto-0.11.15/Cargo.toml:118-122`; crates.io |

**The blocker is therefore exactly:** a **released stable rustls carrying
the client-session serialization API**, plus a **quinn that depends on it**.
Neither exists today. Nothing in this repo can shorten that.

## 3. What changed upstream — the ceiling moved

The original analysis concluded the API might never exist and that a
fork was the only route. That is now wrong in the agent's favour. In
**rustls `0.24.0-dev.1`** the client session API is rebuilt, and both
historical objections are answered:

| # | Finding | Evidence (`rustls-0.24.0-dev.1`) |
|---|---|---|
| C1 | **The serialization API exists and is public.** `Tls13Session::encode(&self, buf: &mut Vec<u8>)` and `Tls13Session::from_slice(bytes, provider)`. The doc comment on `encode` reads *"Encode this ticket into `buf` for persistence."* | `src/client/mod.rs:115-190` |
| C2 | **The `Weak::ptr_eq` identity gate is gone.** `compatible_config` does not exist anywhere in the crate. The store is keyed by `ClientSessionKey { config_hash: [u8; 32], server_name }` — a **content-derived** hash. | `src/client/config.rs:323-367`; repo-wide grep for `ptr_eq` returns nothing |
| C3 | ⟹ The previously-recorded design constraint — *"a disk-loaded ticket cannot carry weak refs to a previous process's Arcs, so the store must re-hydrate against live ones"* — is **obsolete**. A content hash is by construction reproducible in a new process. | C2 |

So the clean, fork-free route is real and its hardest design objection has
dissolved. It is simply not yet consumable.

## 4. A new risk to weigh before building it

`config_hash` is derived from the `TypeId` **and** `hash_config()` of the
server verifier, the client credential resolver, and the time provider
(`SecurityDomain::new`, `src/client/config.rs:476-513`).

`TypeId` is stable within one build of one binary. It is **not** guaranteed
stable across a **rebuilt** binary. If it shifts on rebuild, a persisted
ticket keyed under the old hash is simply not found and the connection
falls back to a full handshake — a safe failure, silently.

That matters here more than it would elsewhere: **auto-update is the agent's
single largest restart cause**, and it is precisely the restart that arrives
with a newly built binary. A disk cache could therefore miss on the
majority of the restarts it was built to cover.

**This must be measured before the work is scheduled, not after.** It is
cheap to settle once the API is consumable: build a store, restart the
process against the *same* binary and then against a *rebuilt* one, and
compare hit rates.

## 5. Options, and where they now stand

| Option | Verdict |
|---|---|
| **A. Fork or patch rustls** to expose client-session serialization | **Rejected.** Maintaining a fork of a security-critical crate, in order to write PSK material to disk *before* the upstream API ships, is worse debt than the gap it closes. C1 makes it unnecessary besides. |
| **B. Implement on the upstream API** | **The path** — once B5/B6 clear. No fork, and §4's risk decides whether it is still worth the work at that point. |
| **C. Decide not to persist; close it by decision** | **Live and legitimate.** The saving is TLS-handshake CPU on an occasional restart, *on top of* a `0x14` fast path that already removes the application round trip and full-jitter backoff that already damps reconnect storms ([`connection.rs`](../../agent/crates/mesh-agent-core/src/connection.rs)). If §4 measures a poor hit rate on rebuilt binaries, C becomes the recommendation rather than the fallback. |

## 6. The unblock trigger

Re-open this plan when **both** hold:

1. A **stable** rustls release (not `-dev`) exposes `Tls13Session::encode`
   / `from_slice` or an equivalent; **and**
2. a published **quinn** depends on that rustls major.

Check with:

```sh
curl -s https://crates.io/api/v1/crates/rustls | jq -r '.crate.max_stable_version'
curl -s https://crates.io/api/v1/crates/quinn  | jq -r '.crate.max_stable_version'
# then confirm the rustls requirement the new quinn-proto actually carries
```

On unblock, do §4's measurement **first**. A poor hit rate on a rebuilt
binary means Option C, and this plan closes without code.

## 7. Scope, for when it unblocks

- `agent/crates/mesh-agent-core/src/tls_session_cache.rs` — a disk-backed
  `ClientSessionStore` honouring **single-use ticket semantics** (each
  value returned from `take_tls13_ticket` at most once, including across a
  restart — a crash between take and use must not resurrect a ticket).
- Wiring in `build_quic_config`
  ([`main.rs:166`](../../agent/crates/mesh-agent/src/main.rs#L166)) via
  `Resumption::store`.
- The cache file is PSK material: `0600`, inside the data directory, under
  whatever the sibling plan established.
- An integration test: populate the store, drop and rebuild the client from
  disk, assert the **server** reports `DidResume == true` — the same check
  [`quic_resumption_test.go`](../../server/internal/agentapi/quic_resumption_test.go)
  already makes.
- An ADR recording the on-disk-PSK decision: persistence trades away some
  client forward secrecy, deliberately. **0-RTT stays off** regardless,
  pending its own replay-safety ADR.
- Docs: the metric added by the sibling plan gains a cross-restart reading.

## 8. Meanwhile

Nothing. Do not vendor, patch, or pin a prerelease to get ahead of this.
The sibling plan's server-side `DidResume` counter is what makes the
eventual before/after measurable, and it does not need this plan to land.
