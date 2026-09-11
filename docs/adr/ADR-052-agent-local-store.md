---
number: 52
title: The agent keeps its own time-series store
---

# ADR-052 — The agent keeps its own time-series store

## Context

Central keeps one averaged reading a minute. Anything finer — the second the
processor spiked, the shape of a stall — exists only on the machine, and has to
survive the machine being offline and being switched off mid-write.

## Decision

**`redb` is the substrate**, chosen by measuring candidates rather than by
reading about them. It brings two-phase commit, so crash safety is inherited
rather than written; it runs no background threads, so latency stays inside the
agent's processor budget; it is pure Rust with no dependencies and a permissive
licence; and its density improves as the store fills.

**Three tiers in separate tables, written in one transaction** — raw seconds,
and two coarser rollups. One transaction means a crash leaves the tiers
consistent with each other.

**Values are fixed-point per metric**, which is the lever that actually moved
density, and raw samples are packed into large blocks of roughly three thousand
so the key index does not cost more than the data.

**Rollups are keyed by the sample's own timestamp and merged as they arrive**,
holding a sum and a count rather than a running average, so a late sample merges
correctly instead of dragging the average.

**A durable cursor marks how far catch-up has sent.** It survives restart, so a
machine that reconnects does not resend what already landed.

**The cap is on logical bytes and eviction is coarsest-first.** When the store
is full, the oldest raw seconds go before the rollups do — the shape of last
month matters more than the exact second.

**The store stamps a format version** and migrates forward, so an agent update
does not orphan what the machine already recorded.

## Consequences

A machine offline for days comes back with its own history intact and fills the
gap centrally, rather than leaving a hole nobody can reconstruct.
