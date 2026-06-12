# Micro-plan W4 — ADR for the client-first handshake + reconnect model

**Parent:** [`fast-path-reconnect-fix.md`](fast-path-reconnect-fix.md). **Depends on:** W2 + W3 decisions settled.
**Branch:** `dev`.

## Why
Record the durable decision and **correct the false "quic-go mTLS bug" rationale**
that lives in the frozen ADR log (ADR-005, [Architecture-Decision-Records.md:57](../../docs/Architecture-Decision-Records.md#L57)).

> **Immutability:** per the user's direction, ADRs may be amended **in place**
> regardless of the current immutability rule — the
> [`current-state-docs-doctrine-and-adr-mutability.md`](current-state-docs-doctrine-and-adr-mutability.md)
> plan owns that governance flip. This micro-plan may therefore amend ADR-005's
> rationale directly (or, if the flip hasn't landed yet, supersede via a new ADR —
> coordinate with that plan's status at implementation time).

## What the ADR records
1. **Client-first handshake** — the agent opens the control stream and speaks first;
   server replies. Restores even (client-initiated) stream IDs. (Was the designed
   model; server-opens was a workaround for a misdiagnosed deadlock — RFC 9000
   stream-discovery, proven mTLS-independent on quic-go v0.60.0.)
2. **The actual auth model** — **mTLS (`RequireAndVerifyClientCert`) + a `0x10`/`0x11`
   nonce/cert-hash exchange + DeviceID from the peer cert. No app-layer signatures**
   (`0x12`/`0x13` were defined but never implemented). This explicitly settles the
   master plan's "mTLS vs signatures" question: it's mTLS-only.
3. **`0x14` fast path** (W2 outcome) — app-layer round-trip optimization, modest
   without W3.
4. **0-RTT / session resumption** (W3 outcome) — the real per-reconnect TLS-cost
   lever; record adopt/defer + the replay-safety rationale + measured saving.

## Steps
1. Write the ADR (next sequential number in [`docs/adr/`](../../docs/adr/)) capturing 1–4.
2. Amend/supersede **ADR-005**'s "quic-go bug" rationale (per the immutability note).
3. Add the [`.claude/decisions.md`](../decisions.md) row; note in [`.claude/phases.md`](../phases.md).
4. **Reconcile the docs:** update [`docs/Multiscale-Readiness.md`](../../docs/Multiscale-Readiness.md) §4 — its "0x14 skip-signature storm defense" framing predates the "no signatures" finding; correct it to "0-RTT/resumption is the TLS-cost lever; `0x14` is an app-layer round-trip optimization." Reconcile the master plan §3/§4 notes likewise.

## Reviewer acceptance
- [ ] New ADR merged; ADR-005 rationale corrected (amended or superseded per the flip).
- [ ] `decisions.md` + `phases.md` updated; Multiscale-Readiness §4 + master-plan notes reconciled to the "no signatures / 0-RTT-is-the-lever" reality.
- [ ] No ADR links to mutable plan files (link to ADRs/code/URLs only); `/precommit` green.

## Out of scope
Code changes (W1/W2/W3) and workaround removal (W5).
