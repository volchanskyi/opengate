---
number: 6
title: Platform traits, with null implementations
---

# ADR-006 — Platform traits, with null implementations

## Context

An agent must not fail to start because a machine has no display.

## Decision

Agent capabilities are Rust traits: `ScreenCapture` is asynchronous and
dynamically dispatched, `InputInjector` and `ServiceLifecycle` are synchronous
and object-safe. The agent implements Linux; every trait a platform does not
implement resolves to its null implementation.

## Consequences

Headless hosts, containers and CI runs use the null implementations.
