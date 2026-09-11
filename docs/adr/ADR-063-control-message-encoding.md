---
number: 63
title: Control messages are hand-encoded and complete
---

# ADR-063 — Control messages are hand-encoded and complete

## Context

`ControlMessage` is one wide struct carrying every message family. Encoding it
by reflection allocated on a path the server walks for every device heartbeat,
and nothing checked that each variant the server can send is one the agent can
read.

## Decision

**`ControlMessage` encodes itself**
([`control_encode.go`](../../server/internal/protocol/control_encode.go)). The
encoder marks which fields are set into a stack array, counts the marks for the
map header, and writes only those. No reflection and no boxing.

**The bytes are identical to what reflection produced** — same field order, same
keys, same integer widths. Byte identity, rather than merely a valid encoding,
is the point: it makes the change invisible to the committed golden fixtures,
the Rust decoder, and every agent already deployed. Decoding stays reflective.

**A zero value is only on the wire where zero is a legal value.** An
informational field absent from a message takes its zero at decode. A field the
receiver acts on is checked at send, so a message that would mean something
different when a field went missing is refused instead.

**Every server-to-agent message has a decoder, a golden fixture and a test**, and
a test asserts the set is complete. Completeness is checked rather than assumed,
because the failure it prevents is silent: the server sends, the agent ignores,
and nothing reports anything.

## Consequences

An empty restart reason is a `400` rather than a restart with no reason
recorded. Adding a variant means adding its reverse golden in the same change,
or the completeness test fails.
