---
number: 23
title: The relay keeps its own session registry
---

# ADR-023 — The relay keeps its own session registry

## Context

The relay pairs a technician's browser with an agent. It needs to know which
sessions are open and which side of each is connected.

## Decision

**A slim `SessionRegistry` port, with one in-process implementation.** The
registry answers which sessions exist and who holds each side. Pairing is local:
both sides of a session land on the same server, so the registry does not need
to be shared.

The port exists because the relay's tests need to drive it directly, which is
one of the reasons [ADR-020](ADR-020-module-boundaries.md) admits for an
interface — not because a second implementation is expected.

## Consequences

The registry is memory in one process. A server restart ends the sessions it was
holding, which is what ending a remote-desktop session on a restarted server
means anyway.
