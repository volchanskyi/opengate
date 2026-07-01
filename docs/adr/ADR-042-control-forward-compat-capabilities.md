---
adr: 042
title: Control Protocol Forward Compatibility and Capabilities
status: Accepted
date: 2026-07-01
---

# ADR-042: Control Protocol Forward Compatibility and Capabilities

## Status

Accepted.

## Context

OpenGate control messages are internally tagged MessagePack enums. Before this
decision, an unknown agent-to-server control type could drop the connection, and
an unknown server-to-agent tag failed during Rust deserialization before the
agent could ignore it. That made additive wire evolution unsafe for mixed
server/agent versions.

## Decision

Ship two protections together:

1. Unknown control types are tolerated in both directions.
   - Go decodes the unknown `type` string and logs/ignores it at dispatch.
   - Rust decodes unknown tags into `ControlMessage::Unknown`.
   - Malformed frames, oversize frames, and low-level decode errors remain fatal.
2. New server-to-agent variants are capability-gated.
   - Agents advertise capabilities in `AgentRegister`.
   - The server refuses to send `RequestHardwareReport` without
     `HardwareInventory`.
   - The server refuses to send `RequestDeviceLogs` without `DeviceLogs`.

Bidirectional golden fixtures include unknown future control-type cases, so both
codec stacks prove the compatibility behavior.

## Consequences

- Mixed-version fleets can tolerate additive control-message rollouts.
- Capability gating is the primary safety mechanism; tolerant unknown decoding is
  the backstop.
- Future server-to-agent variants must define and check an advertised capability
  before being sent.

