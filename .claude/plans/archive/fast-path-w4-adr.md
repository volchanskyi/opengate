# Micro-plan W4 — ADR for the client-first handshake + reconnect model

**Status:** Complete. Archived after ADR-037 and the state-register updates landed.
**Parent:** `fast-path-reconnect-fix.md`. **Depends on:** W1, W2, W3 — **all landed** (see [`phases.md`](../../phases.md)). This is the documentation micro-plan; W5 (cleanup) ran in parallel.
**Branch:** `dev`.

## Why
Record the durable decision for the client-first handshake + reconnect model, and
**correct the false "quic-go mTLS bug" rationale** carried by **ADR-005** (in the
frozen combined log, [`Architecture-Decision-Records.md`](../../../docs/Architecture-Decision-Records.md)).

## ADR governance — settled by ADR-036 (read first)
[ADR-036](../../../docs/adr/ADR-036-mutable-adrs-current-state-doctrine.md) froze the
combined log **ADR-001–012** (never edited or appended) and made per-file ADRs
**013+** mutable. ADR-005 lives in the **frozen** log, so it **cannot be amended in
place** — the earlier "amend per a governance flip" note is obsolete. Correct it
the only permitted way: write a **new per-file ADR** (next number = **ADR-037**)
that **supersedes ADR-005's rationale** (`supersedes:` frontmatter) and record the
supersession in the new ADR + [`decisions.md`](../../decisions.md). The frozen log
stays byte-for-byte untouched.

## What ADR-037 records
1. **Client-first handshake** (W1, landed) — the agent opens the control stream and
   speaks first (`0x11` AgentHello → server `0x10` ServerHello); the server
   `AcceptStream`s. Restores even (client-initiated) stream IDs (RFC 9000 §2.1).
   server-opens was a workaround for a misdiagnosed deadlock, proven
   mTLS-independent on quic-go v0.60.0.
2. **Auth model — mTLS-only** (settles the master-plan "mTLS vs signatures"
   question). `RequireAndVerifyClientCert` + the `0x10`/`0x11` nonce/cert-hash
   exchange + DeviceID from the peer cert. **No app-layer signatures** —
   `0x12`/`0x13` (`MsgServerProof`/`MsgAgentProof`) were defined but never
   implemented (W5 removes them).
3. **`0x14` fast path** (W2, landed) — the agent replays the cached CA-cert hash on
   reconnect; the server verifies it is current and skips the ServerHello/AgentHello
   round-trip (`HandshakeResult.Skipped` wired for real; full-handshake fallback on
   a stale hash). It saves a **round-trip, not crypto**.
4. **TLS session resumption** (W3, **decided** — archived
   [`fast-path-w3-0rtt-eval.md`](fast-path-w3-0rtt-eval.md)) — **the**
   per-reconnect TLS-cost lever. **Decision: adopt 1-RTT resumption, defer 0-RTT.**
   Empirically (`server/internal/agentapi/quic_resumption_test.go`): 1-RTT
   resumption completes under mTLS with the verified client identity preserved
   (`DidResume`, `PeerCertificates`), cutting per-reconnect cost **~23% / ~360µs**
   (~207 fewer allocs). 0-RTT also works on v0.60.0 but its early data is replayable
   (latency-only gain on top of resumption) → deferred. The server is already
   resumption-capable (tickets default-on, `Allow0RTT` off); the quinn agent-side
   ticket cache is the residual (tracked in [`techdebt.md`](../../techdebt.md)).

## Steps
1. Write **ADR-037** in [`docs/adr/`](../../../docs/adr/) capturing 1–4, with
   `supersedes: ADR-005` scoped to the **rationale** (the client-first / mTLS-only
   facts; ADR-005's other content stands).
2. Add the [`decisions.md`](../../decisions.md) row for 037. **Backfill the missing 036
   row** if the docs-doctrine plan has not — the index currently ends at 035 while
   ADR-036 already exists as a file.
3. Note completion in [`phases.md`](../../phases.md) (fast-path series: W4 done; W5
   remaining).
4. **Reconcile the docs to the "no signatures / resumption-is-the-lever" reality:**
   [`docs/Multiscale-Readiness.md`](../../../docs/Multiscale-Readiness.md) §4 (its "0x14
   skip-signature storm defense" framing predates the finding) and the master plan
   §3/§4 notes. (DD-E already aligned [`docs/Wire-Protocol.md`](../../../docs/Wire-Protocol.md)
   to the agent-first model — verify consistency, do not duplicate.)

## Reviewer acceptance
- [x] ADR-037 merged with `supersedes: ADR-005`; the frozen ADR-001–012 log is
      **byte-for-byte unchanged**.
- [x] `decisions.md` has rows for **036** (backfilled if needed) and **037**;
      `phases.md` updated.
- [x] Multiscale-Readiness §4 + master-plan notes reconciled; Wire-Protocol.md
      verified consistent.
- [x] No ADR links to mutable (non-archived) plan files — ADRs / code / URLs /
      archived plans only (write-guard enforced); `/precommit` green.

## Out of scope
Code changes (W1/W2/W3, all landed) and workaround/dead-code removal (W5).
