---
number: 2
title: Golden files prove the two languages agree
---

# ADR-002 — Golden files prove the two languages agree

## Context

Two independent implementations of one protocol drift silently.

## Decision

Rust generates canonical binary fixtures in `/testdata/golden/` and Go tests
decode them. Go-to-Rust fixtures sit beside them, so the contract is checked in
both directions, and each fixture carries a `.meta.json` sidecar recording what
it is meant to contain.

## Consequences

A fixture that is regenerated wrongly fails against its own description rather
than quietly becoming the new expectation.

Run `make golden` after any protocol change. Fixtures are committed.
