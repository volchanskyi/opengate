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
the path**, rather than by a privileged agent on the node. This holds for the
cluster.

**The browser test stack's two routers are the one exception.** That stack is
a throwaway set of containers on a test machine
([`docker-compose.test.yml`](../../deploy/docker-compose.test.yml)). Its office
and home routers hold network-admin, which they need to translate addresses,
drop UDP and shape the rate for the networks behind them. Nothing else in a
compose stack may hold it, and nothing there runs privileged
([`compose.rego`](../../policy/docker_compose/compose.rego)).

Rejected: a compiled-in injector, for the reason above. A build-tag fault
binary, because a non-shipping variant means the drills no longer run against
the image that ships. `toxiproxy`, because it is TCP only and cannot touch the
QUIC path, which is the one that matters most. A privileged node agent, on the
measurement it would distort. A second cluster for the drills, because the
storage cap ([ADR-035](ADR-035-block-volume-budget.md)) does not allow one.

## Consequences

The drills need staging and a link shaper rather than a flag, which makes them
slower to run and impossible to leave switched on in production.
