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
enrollment ([ADR-004](../Architecture-Decision-Records.md)).

**The agent opens the control stream and writes first.** The endpoint that opens
a bidirectional stream has to write before the other side can accept it, so
ownership follows from who has something to say first — and on a reconnect that
is the agent. The server accepts in
[`agentapi/server.go`](../../server/internal/agentapi/server.go).

**The certificate is the whole authentication.** There is no second proof
exchanged in the handshake. A peer that presents a certificate the server's own
authority signed is that device.

**Handshake byte `0x14` saves a round trip, not cryptographic work.** An agent
that already holds the current authority hash says so in its first message, and
the server skips sending it back. The saving is one exchange.

**TLS session resumption carries the cryptographic saving.** `quic-go` issues
session tickets on its own; the agent keeps one and presents it, which turns a
full handshake into 1-RTT. Nothing pins the server's ticket key across a
restart, so the first reconnect after a server restart is always a full
handshake. Zero-RTT is not used — it would need replay protection the control
path does not have.

## Consequences

The server needed no change to gain resumption. Measured in production, an
interrupted connection comes back in well under the budget the reconnect path
was designed for.
