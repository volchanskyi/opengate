# Micro-plan W3 — QUIC 0-RTT / TLS session resumption (evaluation + optional impl)

**Parent:** `fast-path-reconnect-fix.md` (active master plan, `.claude/plans/`). **Depends on:** W1. Can run parallel to W2.
**Branch:** `dev`. **Type:** spike → decision (+ optional implementation).

## Why — this is the *actual* storm lever
Per W2's finding, the dominant per-reconnect cost is the **full TLS 1.3 mTLS
handshake** (ECDSA client-cert chain verify), which the `0x14` app-layer fast path
does **not** avoid. **QUIC 0-RTT / TLS session resumption does** — it lets a
reconnecting agent skip the full handshake. At Large-tier reconnection storms this is
the highest-leverage reduction; at free-tier capacity it's what lets a CPU-bound node
absorb churn. So W3 likely matters more than W2.

## Questions to settle (the deliverable is a decision)
1. **Does 0-RTT work with mTLS client certs** in quic-go v0.60.0 (server) + quinn
   (agent)? Client-auth + 0-RTT has known constraints — confirm empirically, don't assume.
2. **Replay safety.** 0-RTT early data is replayable. What does the agent send in
   0-RTT (the AgentHello / `0x14`)? Is it idempotent under replay, or must early data
   be restricted? Consider **1-RTT session resumption** (no 0-RTT early data) as the
   safer middle ground — it still skips the expensive handshake crypto.
3. **Measured saving.** Prototype and measure per-reconnect CPU/latency: cold mTLS vs
   resumed. Quantify before committing.

## Files (if adopted)
- [`server/internal/agentapi/server.go`](../../../server/internal/agentapi/server.go) — `quic.Config` (`Allow0RTT` and/or session-ticket settings; today it's only `{KeepAlivePeriod}`).
- Agent quinn client config — enable resumption / 0-RTT + a **session-ticket cache** persisted across reconnects (and across process restarts if safe).

## Steps
1. Spike: stand up resumption with mTLS on v0.60.0/quinn; confirm it completes.
2. Assess replay safety; pick **0-RTT** vs **1-RTT resumption** vs **defer**.
3. Measure the saving; record numbers.
4. If adopted: implement behind the same TDD discipline (a test proving a resumed
   reconnect skips the full handshake) + goldens if any wire surface changes.
5. Feed the decision + measurements into **W4**'s ADR.

## Reviewer acceptance
- [ ] A written decision (adopt 0-RTT / adopt 1-RTT resumption / defer) with the
      replay-safety analysis and the measured per-reconnect saving.
- [ ] If implemented: a deterministic test shows a resumed reconnect avoids the full
      handshake; mTLS identity still enforced; `/precommit` green.

## Out of scope
The app-layer `0x14` round-trip → **W2**. ADR write-up → **W4**.

---

## Findings & Decision (2026-06-17)

**Spike artifact:** [`server/internal/agentapi/quic_resumption_test.go`](../../../server/internal/agentapi/quic_resumption_test.go)
— deterministic, always-run tests plus paired `BenchmarkQUICHandshake_{Cold,Resumed}`,
run against the repo's own quic-go v0.60.0 and real mTLS cert config
(`cert.Manager`, `RequireAndVerifyClientCert`, TLS 1.3).

### Q1 — Does resumption work with mTLS client certs? (empirical, not assumed)
Yes — both modes complete and **keep the verified client identity**:
- **1-RTT session resumption** (`TestQUICSessionResumption_PreservesMTLSIdentity`):
  the reconnect resumes (`DidResume==true`) and the server still holds the client
  certificate (`ConnectionState().TLS.PeerCertificates` populated, CN preserved).
  The identity is carried by the session ticket; the expensive client-cert-chain
  verification is **not** repeated.
- **0-RTT early data** (`TestQUIC0RTT_ClientCertBehaviour`): with server `Allow0RTT`
  + client `DialAddrEarly`, `Used0RTT==true` on **both** ends and the server still
  holds the client cert. The "client-auth + 0-RTT" constraint does **not**
  functionally block 0-RTT on this version.

### Q2 — Replay safety
- **1-RTT resumption: replay-safe.** The resumed handshake still performs a fresh
  ECDHE and the PSK binder is bound to fresh ClientHello randomness; a captured
  handshake cannot be replayed to establish a session or inject data.
- **0-RTT: replayable by design** (RFC 8446 §8 / RFC 9001 §9.2). A captured 0-RTT
  flight can be resent and the server re-processes the early-data application bytes
  (the attacker cannot decrypt responses or continue the session — no PSK secret —
  so it is not a confidentiality breach, but the early-data message is delivered ≥1
  extra time). In OpenGate the agent's first reconnect bytes are the `0x14`
  AgentHello (cached CA-cert hash); a replay would start a **duplicate
  reconnect/registration** for that device, churning the `conns` map and
  online/offline events. Avoiding that needs server-side single-use-ticket /
  anti-replay handling, or restricting 0-RTT to truly idempotent early data.

### Q3 — Measured per-reconnect saving
Paired benchmarks (full dial, both endpoints in-process over loopback; the *delta*
isolates the asymmetric handshake crypto skipped on resume):

| | ns/op | allocs/op |
|---|---|---|
| Cold (full mTLS) | ~1.52M | ~2,580 |
| Resumed (1-RTT)  | ~1.16M | ~2,374 |
| **Saving**       | **~360µs (~23%)** | **~207** |

The saved work is certificate parsing + the asymmetric operations (client-cert-chain
ECDSA verify + each peer's CertificateVerify ECDSA sign/verify). On the **server** —
the side a reconnection storm multiplies — resumption removes the client-cert-chain
verify and the server's own CertificateVerify signature.

### Decision: adopt 1-RTT session resumption; defer 0-RTT
- **Adopt 1-RTT resumption.** It captures essentially all of the per-reconnect
  *crypto/CPU* saving (the storm-cost lever) with **zero replay exposure** and **no
  weakening of mTLS** (identity verified at the cold handshake, carried by the ticket).
- **No server change required.** quic-go / Go `crypto/tls` issues session tickets by
  default; the spike proves resumption succeeds against the **unmodified**
  `ServerTLSConfig` and a `quic.Config` that does **not** set `Allow0RTT`. Keep
  `Allow0RTT` **off** to foreclose 0-RTT replay.
- **Defer 0-RTT.** On top of 1-RTT resumption it saves only ~1 RTT of *latency* and
  **no additional crypto/CPU**, in exchange for replay exposure needing anti-replay
  handling. Revisit only if reconnect tail-*latency* (not CPU) becomes binding.

### Implementation status / residual
- **Server:** already resumption-capable — no change. `TestQUICSessionResumption_PreservesMTLSIdentity`
  is the always-run regression guard that the server keeps resuming with mTLS preserved.
- **Agent (quinn, Rust):** to *realize* the saving in production the agent must enable
  TLS session resumption and persist a session-ticket cache across reconnects (and, if
  safe, across process restarts). Tracked in [`techdebt.md`](../../techdebt.md).
  This is a backward-compatible client-side change (a server without tickets, or a
  client without a cache, simply falls back to a full handshake) — unlike W1 it is
  **not** a breaking wire change, but it should ship with an agent reconnect
  verification within the same coordinated rollout.
- **W4 ADR input:** "adopt 1-RTT resumption, defer 0-RTT," with the measured saving
  and the replay analysis above.
