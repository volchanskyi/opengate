---
number: 37
title: QUIC transport and reconnect
---

# ADR-037 — QUIC transport and reconnect

## Context

Agents run on untrusted networks behind NAT, on connections that drop. They need
a transport that survives that, authenticates both ends, and reconnects cheaply
enough to be worth doing often.

## Decision

**QUIC with mutual TLS**, using `quic-go` at the version pinned in
[`server/go.mod`](../../server/go.mod). Agent certificates are issued at
enrollment ([ADR-004](ADR-004-mutual-tls-enrollment.md)).

**The agent opens the control stream and writes first.** The endpoint that opens
a bidirectional stream has to write before the other side can accept it, so
ownership follows from who has something to say first — and on a reconnect that
is the agent. The server accepts in
[`agentapi/server.go`](../../server/internal/agentapi/server.go).

**The certificate is the whole authentication.** There is no second proof
exchanged in the handshake. A peer that presents a certificate one of the
server's trusted master keys signed, and that the server's certificate ledger
lists as that device's current certificate, is that device; how a certificate is
classified, renewed and admitted expired is
[ADR-128](./ADR-128-machine-identity-lifecycle.md).

**Handshake byte `0x14` saves a round trip, not cryptographic work.** An agent
that already holds the hash of a master key the server trusts says so in its
first message, and the server skips sending it back. The saving is one exchange.
A stale hash is refused with close code 2, and the agent's next connection is a
full handshake.

**TLS session resumption carries the cryptographic saving.** `quic-go` issues
session tickets on its own; the agent keeps one and presents it, which turns a
full handshake into 1-RTT. Nothing pins the server's ticket key across a
restart, so the first reconnect after a server restart is always a full
handshake. Zero-RTT is not used — it would need replay protection the control
path does not have. A resumed handshake is classified like a full one, because
the check runs in `VerifyConnection`, which Go runs on both, and a ticket for an
expired certificate never resumes.

**The agents' UDP port also answers the session helper.** The listener hands a
packet whose first byte has its two top bits clear — never a QUIC packet, always
a STUN message — to the helper
([ADR-127](./ADR-127-direct-link.md)) through `quic-go`'s 32-slot queue for
non-QUIC packets, which drops overflow without holding agent traffic. That split
holds only while no agent clears the QUIC fixed bit, and `quinn` clears it only
when the server offers to let peers do so, which `quic-go` never does.
[`TestTheAgentListenerNeverOffersToLetPeersClearTheFixedBit`](../../server/tests/integration/helper_test.go)
reads the transport parameters the listener sends and fails on that offer, so a
`quic-go` release that makes it cannot route agent packets to the helper
unnoticed.

## Consequences

The server needed no change to gain resumption. Measured in production, an
interrupted connection comes back in well under the budget the reconnect path
was designed for.
