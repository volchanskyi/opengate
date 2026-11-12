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

**An answer pages itself under the message ceiling.** The server reads no machine
message longer than the largest one a machine sends
([ADR-044](ADR-044-telemetry-ingest.md)), and a thousand long lines can run to
several mebibytes. The machine stops adding lines once the answer nears the
ceiling, cutting a single line too long for it, and says more remain
([`device_logs.rs`](../../agent/crates/mesh-agent/src/device_logs.rs)); the log
view's next page starts after the lines it received
([`LogExplorer.tsx`](../../web/src/features/devices/LogExplorer.tsx)). Raising
the ceiling to fit a full answer was not taken: it would raise the bytes every
machine connection may hold before the server reads them.

**Host logs are read through the operating system's own tools** — `journalctl`
with JSON output on Linux, and the agent's own rotated files for its own logs.
No log library is linked into the agent, which keeps a copyleft dependency out
of a binary shipped to customer machines.

**Filters are pushed down to the reader.** Severity, time range and unit bound
what is read rather than being applied to records the agent already paid to
parse.

## Consequences

A disconnected machine exposes no history — which is the cost, and is accepted,
because the alternative is a central store of everyone's logs. There is also
nothing secret-dense to erase centrally when a customer asks.
