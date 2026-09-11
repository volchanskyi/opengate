---
number: 46
title: Logs stay on the machine
---

# ADR-046 — Logs stay on the machine

## Context

Log lines are the densest source of secrets and personal data in the product.
Storing a fleet's logs centrally creates one place holding all of it, which then
has to be protected, retained, and erased on request.

## Decision

**Nothing raw is stored centrally.** `GET /api/v1/devices/{id}/logs` resolves the
connected agent, sends `RequestDeviceLogs`, waits on a per-connection
single-flight waiter, and returns the lines in the same response. There is no
table and no cache. Tenant isolation for raw logs is the agent connection's
scope rather than a database row, because there is no row.

**One pull per connection at a time.** Responses carry no correlation
identifier, so a second concurrent caller gets `409`, and a timeout is `504`.

**Reading raw logs needs administrator rights and is audited.** Every pull
writes a `device.logs.read` event recording who, which machine, and what window
was asked for — never the content.

**The response is bounded** by line count, bytes per line and how long the
server will wait, and passes a server-side redaction guard that strips
authorisation headers, key-value credentials, cloud keys and private keys even
when the agent's own redaction is off. Two independent guards, because one of
them being wrong is the case worth surviving.

**Host logs are read through the operating system's own tools** — `journalctl`
with JSON output on Linux, and the agent's own rotated files for its own logs.
No log library is linked into the agent, which keeps a copyleft dependency out
of a binary shipped to customer machines.

**Filters are pushed down to the reader.** Severity, time range and unit bound
what is read rather than being applied to records the agent already paid to
parse.

**Rates, not lines, are telemetry.** How many errors a minute is a number and
goes to the metrics store like any other reading. The lines themselves stay put.

## Consequences

A disconnected machine exposes no history — which is the cost, and is accepted,
because the alternative is a central store of everyone's logs. There is also
nothing secret-dense to erase centrally when a customer asks.
