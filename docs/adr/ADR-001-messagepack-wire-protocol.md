---
number: 1
title: MessagePack wire protocol
---

# ADR-001 — MessagePack wire protocol

## Context

JSON is too verbose for a per-second telemetry path, protobuf needs a schema
compiler in both toolchains, and a bespoke format is a maintenance cost forever.

## Decision

MessagePack, with internally tagged enums in Rust (`#[serde(tag = "type")]`) and
matching struct tags in Go. Frames are `[1-byte type][4-byte big-endian length][payload]`;
ping and pong are a single byte. The handshake uses raw binary rather than
MessagePack, on type bytes `0x10`–`0x15`.

MessagePack is compact, needs no schema, and both languages have a good library.

## Consequences

Every message has a Rust side and a Go side that must agree, which
[ADR-002](ADR-002-golden-files.md) holds.
