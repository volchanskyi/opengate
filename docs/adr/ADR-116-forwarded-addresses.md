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

## Consequences

Adding a proxy means naming it in the deployment. An unnamed proxy is not
trusted, which fails visibly rather than silently attributing everything to one
address.
