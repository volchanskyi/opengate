---
adr: 059
title: Agent Session Row Lifecycle — Unpaired Release and Relay-Keyed Sweep
status: Accepted
date: 2026-07-25
---

# ADR-059: Agent Session Row Lifecycle — Unpaired Release and Relay-Keyed Sweep

## Status

Accepted.

## Context

`POST /api/v1/sessions` writes an `agent_sessions` row, hands the browser a
token, and asks the agent to dial the relay with it. The row is the only record
of the session, and the device page reads it back as **Active Sessions**.

Deletion had exactly one trigger: the relay's `OnSessionEnd`, which fires when a
paired session finishes piping. Three paths never reach it, and each leaves a row
that nothing will ever remove:

- **Never connected.** The token is issued, then neither side uses it — the tab
  closed, the operator navigated away, the agent dropped before dialling back.
- **Half-open.** One side registers and leaves before its peer arrives. No pipe
  ever starts, so nothing tears the session down; the relay's map entry and its
  `ActiveSessionCount` leaked with the row.
- **Process restart.** Every session a process was serving dies with it. The
  rows survive, and no surviving connection can claim them.

Operationally this shows as a device page reporting a live session token when
nothing is connected, with no way to clear it short of a manual `DELETE`.

## Decision

Two mechanisms, split by whether the server can still observe the connection.

**1. Bounded unpaired release, for what the process can see.**
[`Relay.Unregister`](../../server/internal/relay/relay.go) ends a session that
never reached the pipe: it drops the map entry, decrements the active count,
releases the registry record, fires `OnSessionEnd`, and closes any connection
still waiting on the missing peer. External cleanup runs before the graceful
WebSocket close because that close may wait for a peer acknowledgement.

The relay WebSocket handler defers `Unregister` after every successful register
and bounds `WaitForPeer` with
[`RelayPeerTimeout`](../../server/internal/api/api.go). An upgraded WebSocket
whose peer disappears is therefore released even when the HTTP request context
cannot observe the disconnect without an active socket read. A session that
reaches the pipe is left alone — that teardown belongs to the pipe, and a second
release would double-count — and a `released` flag under the session lock keeps
a concurrently-registering peer from resurrecting a release already in
progress.

**2. Tenantless relay-end deletion.** `OnSessionEnd` runs after the originating
request contexts are gone, so it calls
[`DeleteRelaySession`](../../server/internal/session/postgres.go), whose admin
scope and globally unique token lookup work across tenants. Request-driven
deletion keeps the tenant-scoped `Delete` path. The callback tolerates
`ErrSessionNotFound` because the stale sweep may win the cleanup race.

**3. A relay-keyed sweep, for what the process cannot see.** A periodic job deletes every row
older than a grace period whose token the relay does not currently hold, running
cross-tenant outside any request tenant. The relay's live token set is the liveness
oracle: a session mid-flight is named by it and spared however long it runs,
while a token abandoned seconds after issue is collected on the next tick. A
process holds no relay sessions at boot, so rows left by its predecessor are
collectable as soon as they age past the grace period. The sweep runs once at
startup and then on a ticker; the grace period only has to outlast the gap
between issuing a token and connecting with it, so it doubles as the worst-case
lag before a session orphaned by a restart leaves the device page.

The device page re-reads its session list on the same 30 s poll that refreshes
the device, so a session that ended anywhere leaves the card without a reload.

## Alternatives considered

**An `expires_at` column renewed by a heartbeat.** The conventional lease: stamp
an expiry at creation, have the piping relay extend it, and sweep on the column
alone. It works, and it is replica-safe by construction. Rejected as more
machinery than the problem needs — a migration, a repository write on a timer,
and a renewal loop per live session — for an answer the relay already holds in
memory. The keep-list gets the same protection for live sessions with no schema
change and no periodic write. The heartbeat column becomes the right answer if
the relay is ever pooled across replicas, because a keep-list is then only as
complete as the `SessionRegistry` adapter behind it; the port is already the seam
for that.

**Deleting all rows at startup.** Correct and trivial for a single replica, and
it collapses the restart case to zero lag. Rejected because a second replica
starting would delete sessions its peer is actively serving — a failure mode with
no local symptom, introduced by a deployment change rather than a code change.

**Filtering the read instead of deleting.** `ListActiveForDevice` could return
only rows the relay holds, making the page correct without a sweep. Rejected: it
fixes the display and leaves the table growing forever, and it makes a read
depend on process-local state, so the same query answers differently per replica.

**Waiting only for request-context cancellation.** After a WebSocket upgrade,
the server cannot rely on the HTTP request context to notice a closed unpaired
socket when nothing reads from it. Rejected because that leaves the relay entry
in the live-token keep-list indefinitely. A bounded peer wait preserves the
normal fast pairing path without introducing a second reader that could consume
terminal or desktop frames before the pipe starts.

## Consequences

- `session.Repository` has two explicit background paths:
  `DeleteRelaySession(ctx, token)` for immediate relay teardown and
  `DeleteStale(ctx, cutoff, keep)` for reconciliation. Both carry their own
  admin tenant scope, and RLS widens that scope across tenants.
- `Relay` gains `ActiveTokens` and `Unregister`. `ActiveTokens` is the liveness
  oracle above; `Unregister` is safe to defer unconditionally, being inert for
  unknown, already-released, and piping tokens.
- An unpaired row is bounded by the peer-wait timeout; a row abandoned by a
  process restart is bounded by the sweep grace period. Piped sessions are
  deleted as their relay ends.
