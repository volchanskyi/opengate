---
number: 55
title: No fault code in the shipped binary
---

# ADR-055 — No fault code in the shipped binary

## Context

Proving the system survives failures means causing failures. The obvious way —
an injector compiled into the server, switched on by configuration — puts code
that breaks the product inside the product.

## Decision

**No fault code is compiled into the shipped server.** Faults come from outside
the process, in two forms.

**In-process behaviour is faulted by substituting an adapter in a Go test
harness.** The seam already exists because the ports exist; a test supplies a
failing implementation of one.

**Deployed faults come from tooling scoped to staging**, holding no production
credential, applied from version-controlled templates.

**The machine-facing network path is faulted by an unprivileged link shaper in
the path**, rather than by a privileged agent on the node.

Rejected: a compiled-in injector, for the reason above. A build-tag fault
binary, because a non-shipping variant means the drills no longer run against
the image that ships. `toxiproxy`, because it is TCP only and cannot touch the
QUIC path, which is the one that matters most. A privileged node agent, on the
measurement it would distort. A second cluster for the drills, because the
storage cap ([ADR-035](ADR-035-block-volume-budget.md)) does not allow one.

## Consequences

The drills need staging and a link shaper rather than a flag, which makes them
slower to run and impossible to leave switched on in production.
