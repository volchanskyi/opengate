---
number: 88
title: A benchmark measures the code, not the harness
---

# ADR-088 — A benchmark measures the code, not the harness

## Context

The handshake benchmark's measured region included its own pipe and the
scheduler moving between two goroutines. The number moved when the runner was
busy and did not move when the handshake changed.

## Decision

**The measured region is the code under test**, with setup and transport
outside it.

**No benchmark toggles its own clock inside the measured loop.** Stopping and
starting the timer per iteration measures the timer.

**Each lazily loaded bundle carries its own size budget.** One large dependency
inside a shared budget leaves every other lazy chunk unmeasured, because the
shared figure is dominated by the one thing.

**A value fixed for the life of an object is computed once.** The certificate
authority hash was being recomputed per connection inside the loop that was
supposedly measuring the connection.

## Consequences

Guards refuse a clock toggled inside a measured loop and a bundle subtracted
from a budget without a budget of its own, so the shapes cannot come back.
