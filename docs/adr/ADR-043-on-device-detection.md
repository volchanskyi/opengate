---
number: 43
title: The device detects its own problems
---

# ADR-043 — The device detects its own problems

## Context

Sending every reading to the server and deciding there costs bandwidth on the
machine and cardinality in the metrics store, and it stops working the moment
the machine is offline — which is often exactly when something is wrong.

## Decision

**The agent samples and judges locally, always on.** `mesh-agent-core::ml` holds
a deterministic k-means model with `k=2`, an ensemble that requires every model
to agree, and a rolling window of anomaly bits. Sampling starts on every agent;
a failure is logged and does not disturb control or session traffic.

**Process samples are bounded and stripped.** A top-N ranking carrying the
executable's base name and, optionally, a hash of its command line. Full command
lines are not collected. Redaction covers assignments, flag values, bearer
tokens, cloud access keys and credentials embedded in addresses.

**Threshold rules are evaluated beside the model.** A rule is a vitals
dimension, a comparator and a boundary, evaluated by a pure stateful evaluator
with hysteresis — a separate clear boundary, so a reading hovering on the line
does not flap — and a sustain window, so a spike is not a breach.

**Rules are delivered per tenant and gated on capability.** An agent receives
the rules for its own tenant on registration, and only if it said it supports
them.

**A breach rides the existing health summary** rather than adding a message
family, and emission is throttled and driven by state change: a breach that is
still true is not re-reported every cycle.

## Consequences

The detection loop allocates nothing after the model is loaded, and tests assert
that. A benchmark records detection latency, sampling cost and memory so a
regression on a small machine is visible before it ships.
