# Master Plan: QUIC Client-First Handshake + Fast-Path Reconnect

**Type:** Master plan. To be **broken into micro-plans** for implementing engineers
(per the review workflow). Do **not** implement directly from this file.
**Status:** Complete — W1 through W5 landed; final decision captured in
[`ADR-037`](../../docs/adr/ADR-037-client-first-fast-path-reconnect.md).
**Supersedes / retires:** [`archive/quic-stream-ownership-fix.md`](archive/quic-stream-ownership-fix.md)
(the old "wait for a quic-go fix, then revert" plan — disproven; see §2).
**Readiness context:** this is the storm-defense prerequisite tracked in
[`docs/Multiscale-Readiness.md`](../../docs/Multiscale-Readiness.md) §4.

---

## 1. One-paragraph summary

The QUIC control stream now uses the intended **agent-opens / agent-speaks-first**
model. The agent sends `0x11` `AgentHello` for a cold start or fallback, or `0x14`
`SkipAuth` for a reconnect fast path; the server accepts the stream and branches on
that first byte. The old server-opens/server-speaks-first model was a workaround for
a deadlock misdiagnosed as a quic-go mTLS bug; ADR-037 supersedes that rationale.

---

## 2. Confidence & evidence (high — independently verified this investigation)

| Claim | How verified |
|---|---|
| The deadlock is **not** mTLS-specific and **not** a quic-go bug | A 4-cell TLS/mTLS matrix written against the repo's own **quic-go v0.60.0**: "opener opens but doesn't write → peer `AcceptStream` times out" reproduced **identically** with server-only TLS *and* mutual TLS; writing one byte first fixed it; server-opens-writes-first (current design) works in both. It is RFC 9000 §2 stream-discovery. |
| Server-opens was a **workaround**, not a design choice | The original Architecture Design (§2.1/§2.2/§3.3) has the **agent connect outbound and maintain the streams**, with the fast path explicitly **agent-initiated** (*"Agent sends [0x14]"*). The design is internally inconsistent (it also draws the full handshake server-message-first), which is the latent flaw the workaround mis-resolved. |
| The misdiagnosis is original to Phase 4 | Commit `97cb935` (initial Phase 4) already shipped `OpenStreamSync` with the comment *"With mTLS, AcceptStream blocks until the client writes data"*. The server **never** called `AcceptStream` as code in committed history. |
| The `0x14` fast path was never built | `MsgSkipAuth = 0x14` is a constant; `HandshakeResult.Skipped` is hardcoded `false` ([handshaker.go](../../server/internal/agentapi/handshaker.go)); no `git` history ever added `0x14` handling in `agentapi`. |
| Changing this is **safe now** | Live prod (verified 2026-06-11): **1 node, 1 server replica, 1 connected agent**. Zero current operational impact; this is correctness/readiness debt. |

Current code anchors: client-first branch
[`handshaker.go`](../../server/internal/agentapi/handshaker.go), bounded
`AcceptStream` [`server.go`](../../server/internal/agentapi/server.go), agent
`open_bi` + write-first [`main.rs`](../../agent/crates/mesh-agent/src/main.rs), and
mTLS `RequireAndVerifyClientCert` [`cert.go`](../../server/internal/cert/cert.go).

---

## 3. The real fix (what & why)

Realign to the **designed** model: the **agent opens** the control stream and
**writes first**; the server replies. Three outcomes, all from one protocol reorder:

1. **Deadlock gone, the QUIC-correct way** — the opener writes first, so the peer's
   accept returns. Independent of TLS mode.
2. **Even (client-initiated) stream IDs** — the conventional model the design assumed.
3. **`0x14` fast path becomes implementable** — the agent can send the cached-cert
   hash first, which is exactly what the storm defense requires.

**Final client-first flow** (ADR-037):

```
Full handshake (cold):
  Agent  → Server: [0x11] agent_nonce(32) agent_cert_hash(48)   # agent opens + writes first
  Server → Agent:  [0x10] server_nonce(32) CA_cert_hash(48)

Fast path (reconnect):
  Agent  → Server: [0x14] cached_CA_cert_hash(48)               # agent opens + writes first
  Server: verify hash == current CA cert hash → no handshake reply
```

> **This is a breaking wire-protocol change.** A client-first server cannot
> handshake with a server-first agent and vice-versa. Production has **one** agent
> and an Ed25519-signed auto-update channel (Phase 14), so the cutover is
> manageable — but a micro-plan **must** own the rollout: either (a) coordinate the
> server deploy with an agent auto-update push, or (b) implement transitional
> dual-mode handshake (server peeks the first frame: if data arrives it accepts;
> if not, it falls back to opening). Decide explicitly.

---

## 4. The reconnection-storm rationale (why this matters at all)

> The design's purpose-built defense against exactly this is the three
> fast-reconnect mechanisms: QUIC 0-RTT (§1), cached server-cert-hash (§2.1), and
> the 0x14 fast path (§3.3: "skip full signature exchange"). Their whole reason to
> exist is to make a storm cheap — verify a 48-byte hash instead of running
> signatures.

> There's a real design question the fast-path work should settle: at scale, do you
> need both the TLS mTLS client-cert verify and the [0x10–0x13] signature exchange
> on every reconnect, or does QUIC 0-RTT/session-resumption + the 0x14 hash-check
> give you the same security for far less per-reconnect CPU? That's the actual
> 20k-readiness conversation, and the current server-opens code can't even start it.

This master plan exists to make that conversation possible. The fast path is the
necessary (not sufficient) prerequisite for surviving Large-tier reconnection
storms — see [`docs/Multiscale-Readiness.md`](../../docs/Multiscale-Readiness.md) §4/§8.

**Implementation reality (verified — settles the design question above):** the
handshake today is **mTLS + a single `0x10`/`0x11` nonce/cert-hash exchange, with no
app-layer signatures**. The retired `0x12`/`0x13` proof-message reservations were
never implemented in production. So the design's *"skip the signature exchange"*
is moot: there are no signatures. `0x14` skips the `0x10`/`0x11` **round-trip**,
not crypto; the dominant per-reconnect cost is the **TLS mTLS handshake itself**, which
only **0-RTT / session resumption (W3)** avoids. Net: W3 outranks W2 for storm cost,
and the "mTLS vs signatures" question is already answered — it's **mTLS-only**.

**Final reconciliation:** W5 retired `0x12`/`0x13`, so the active storm-readiness
levers are `0x14` for a round-trip reduction, 1-RTT TLS session resumption for
TLS CPU reduction, and reconnect backoff/jitter for herd control. 0-RTT remains
deferred because early data is replayable.

---

## 5. Workstreams (basis for the micro-plan breakdown)

- **W1 — Client-first handshake (Go + Rust + goldens). Done.** Reorder
  [handshaker.go](../../server/internal/agentapi/handshaker.go) to read AgentHello
  first then write ServerHello; switch the server to **accept** the
  control stream (replace `OpenStreamSync`/`openControlStream` with `AcceptStream`);
  switch the Rust agent to `open_bi` + write-first (replace `accept_bi`);
  regenerate the cross-language handshake **golden files** (the wire byte order
  changes); update integration tests (`connectAgent` opens the stream).
- **W2 — `0x14` fast path + the security decision. Done.** Implement agent-sends-`0x14`
  on reconnect + server hash-verify + ServerHello round-trip skip; wire
  `HandshakeResult.Skipped` for real. **Settle the §4 design question**
  (mTLS-only vs. 0-RTT/resumption +
  hash-check) and record it in the ADR (W4). Depends on W1.
- **W3 — QUIC 0-RTT / session resumption (evaluation). Done.** Assess enabling
  `Allow0RTT` server-side + a client session-ticket cache; quantify the
  per-reconnect CPU saving vs. replay-safety constraints. May fold into W2.
- **W4 — ADR + decommission the misdiagnosis. Done.** Write a **new ADR** documenting
  client-first handshake + the storm-defense rationale, **superseding ADR-005's
  rationale** (the "quic-go bug" claim). Per the ADR-immutability rule, do **not**
  edit the frozen [`Architecture-Decision-Records.md`](../../docs/Architecture-Decision-Records.md)
  in place — supersede. Add a [`decisions.md`](../../.claude/decisions.md) row.
- **W5 — Remove the workaround + harden. Done.** Delete the server-opens code path and
  the "(stream ownership workaround)" / "mTLS bug" comments; add a **bounded
  context** around the stream open/accept (the one valid idea from the retired
  plan — a defensive timeout, not a quic-go workaround).

**Out of scope here** (tracked in the readiness doc, not this plan): NetworkPolicy
for `:9091`, Redis enablement, KEDA, the QUIC NLB. Those are Large-tier cutover
items, gated separately.

---

## 6. Sequencing & gating

Completed order: `W1` → `W2` → `W3` → `W4` + `W5`. The remaining operational gate
is the coordinated production server + signed-agent rollout tracked in
[`techdebt.md`](../techdebt.md).

---

## 7. Reviewer acceptance criteria

- [x] A non-mTLS **and** mTLS test proves the client-first handshake completes with
      the agent opening the stream (no deadlock), on quic-go v0.60.0.
- [x] Control-stream IDs are **even** (client-initiated) on a live handshake.
- [x] `0x14` reconnect path verified: a second connect with a valid cached hash
      **skips** the ServerHello round-trip (assert `HandshakeResult.Skipped == true`),
      and a stale/invalid hash falls back to the full handshake.
- [x] Cross-language golden tests regenerated and green (Rust ↔ Go).
- [ ] Rollout handled (coordinated update or dual-mode) — the production agent
      reconnects across the cutover without manual re-enrollment.
- [x] New ADR merged; ADR-005 rationale superseded (not edited in place);
      `decisions.md` row added.
- [x] Server-opens code + "workaround"/"mTLS bug" comments removed; bounded timeout
      added. Full `/precommit` gauntlet green.

---

## 8. Execution workflow (enforced — no bypass)

Per [`CLAUDE.md`](../../CLAUDE.md): work on `dev`; **TDD** (failing handshake test
before the source reorder — and the wire-protocol change *needs* tests first);
`/precommit` before every commit; `/refactor` after; author = Ivan, no
`Co-Authored-By`. The golden-file regeneration is a generator step (`make golden`),
not a hand-edit. Each micro-plan keeps the gauntlet green per commit.

---

## 9. Notes (verbatim, per request)

> The design's purpose-built defense against exactly this is the three fast-reconnect
> mechanisms: QUIC 0-RTT (§1), cached server-cert-hash (§2.1), and the 0x14 fast path
> (§3.3: "skip full signature exchange"). Their whole reason to exist is to make a
> storm cheap — verify a 48-byte hash instead of running signatures.

> There's a real design question the fast-path work should settle: at scale, do you
> need both the TLS mTLS client-cert verify and the [0x10–0x13] signature exchange on
> every reconnect, or does QUIC 0-RTT/session-resumption + the 0x14 hash-check give
> you the same security for far less per-reconnect CPU? That's the actual
> 20k-readiness conversation, and the current server-opens code can't even start it.
