# Micro-plan W3 — QUIC 0-RTT / TLS session resumption (evaluation + optional impl)

**Parent:** [`fast-path-reconnect-fix.md`](fast-path-reconnect-fix.md). **Depends on:** W1. Can run parallel to W2.
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
- [`server/internal/agentapi/server.go`](../../server/internal/agentapi/server.go) — `quic.Config` (`Allow0RTT` and/or session-ticket settings; today it's only `{KeepAlivePeriod}`).
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
