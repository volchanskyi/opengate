---
number: 42
title: Unknown control messages are tolerated
---

# ADR-042 — Unknown control messages are tolerated

## Context

Agents update over the air and a fleet is never all on one version. A server
sending a message an older agent has never heard of, or an agent sending one a
server predates, must not break the connection between them.

## Decision

**An unknown message type is ignored, not fatal.** Go decodes the type string
and drops it at dispatch; Rust decodes it into `ControlMessage::Unknown`. A
malformed frame, an oversized frame or a decode error stays fatal — tolerance is
for messages from the future, not for corruption.

**New server-to-agent messages are gated on a declared capability.** An agent
lists what it supports in `AgentRegister`, and the server refuses to send a
message the agent has not claimed. Tolerant decoding is the safety net;
capabilities are the actual gate.

Golden fixtures include unknown-type cases in both directions, so each side's
tolerance is proved rather than assumed.

## Consequences

A control message can be added to one side and deployed before the other side
knows about it.
