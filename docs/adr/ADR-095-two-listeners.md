---
number: 95
title: The server binds two listeners
---

# ADR-095 — The server binds two listeners

## Context

The metrics exposition and the profiler were served on the same port as the API.
The profiler in particular will happily spend a core and dump the heap for
anyone who asks.

## Decision

**Two listeners.** The public one serves the API, the WebSocket relay and the
health check. The second binds cluster-only and carries the exposition and the
profiler.

**A second port rather than a path carve-out**, because there is nowhere in an
HTTP router to put a boundary that a proxy misconfiguration cannot route around.

**The health check stays public**, because the kubelet probes the container's
published port.

**The profiler is registered by hand**, not by importing a package for its side
effect, so it cannot arrive on the public listener because somebody imported
something.

**Every consumer moved in the same change, and each gets a read-back.** A
consumer left pointing at the old port fails at the port rather than by
returning nothing.

**A check that asserts an absence proves it reached something first.** The smoke
run asserts the exposition and the profiler are not what the public edge
answers with — and an empty body matches neither pattern, so a request that
resolved nowhere would report the boundary healthy. Every absence-shaped check
asks whether the edge answered at all, and the target is named rather than
assumed. A missing port argument fails the smoke test rather than defaulting to
something.

## Consequences

The stub-driven test in
[`smoke-test-edge.test.sh`](../../scripts/tests/smoke-test-edge.test.sh) drives
the script against an edge that keeps the boundary, one that breaks it, and one
that is not there, and requires a different verdict from each.
