---
number: 116
title: A forwarded address is trusted only from a named proxy
---

# ADR-116 — A forwarded address is trusted only from a named proxy

## Context

Behind a proxy, every request arrives from the proxy's address. Anything that
counts per caller — a rate limit, an audit line — counts the proxy instead, and
believing a forwarded header from any peer lets a caller claim to be anyone.

## Decision

**The deployment names its proxies by service, and a forwarded address is
believed only from a peer it named.** From anything else, the address is the
address the connection came from.

**A load run presents one address per technician and one per machine**, so a
per-caller limit is measured per caller rather than being hit at once by one
generator pretending to be a fleet.

**A run counts how much of what it asked was refused, and carries the count into
its own evidence.** The chain that makes a presented address believed runs
through five files and a cluster, and only the files half is checkable as text:
whether the generator pods became endpoints of the named service in time, and
whether the resolver answered for them, is a fact about the night. A night where
they did not is a night spent behind one allowance — it fills with refusals,
reds the error-rate gate, and is shaped exactly like a night against a slow
server. What k6 publishes about failures is a single pass/fail rate with no
breakdown by status, so the count is taken by the scenarios themselves, through
the one request client they all share, and read back beside the run's verdict.

## Consequences

Adding a proxy means naming it in the deployment. An unnamed proxy is not
trusted, and the run says so in a number rather than in the shape of a slow
night: a broken test setup and a product regression stop looking the same.
