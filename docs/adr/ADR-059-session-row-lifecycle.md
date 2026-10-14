---
number: 59
title: A host holds one session row, and the row and the live session end together
---

# ADR-059 — A host holds one session row, and the row and the live session end together

## Context

Each remote session writes a row saying a device is busy. Rows outlived their
sessions and devices showed as occupied by sessions nobody was in.

Nothing stopped two sessions on one host: creating one checked only that the
host was connected, so two technicians could type into one customer server at
once, unaware of each other. Deleting a row through the API left the live relay
carrying the session, and any member could delete any session. The device page
listed each open session's token — the relay credential — to every member who
opened it, and the relay admitted a browser side from any member of the tenant
holding one.

## Decision

**A host holds at most one session, and it belongs to the technician who opened
it.** The row is unique on its device
([`019_one_session_per_host`](../../server/internal/db/migrations/019_one_session_per_host.up.sql)
keeps each host's newest row before it adds the index). Creating a session on a
host another technician holds is refused with `409`, naming the holder and the
start time; two technicians clicking at the same moment get one session and one
refusal. Creating one on a host the same technician holds replaces it: the server
ends the old relay with "Opened elsewhere" and starts the new session, which is
what a reload, a second browser or a laptop waking after its relay dropped needs.
The relay admits a browser side only from the holder, so a token that leaks
attaches nobody else.

**Only the holder or an administrator ends a session, and ending the row ends
the relay.** A delete by either — of the session, or of the host's session
through `DELETE /api/v1/devices/{id}/session` — removes the row and closes the
live relay with "Ended by <name>"
([`usecase/session.go`](../../server/internal/usecase/session.go)); anyone else
is refused with `403` and the session carries on. Both reasons travel as the
relay's close reason, under close code `StatusSessionEnded`
([`wsconn.go`](../../server/internal/api/wsconn.go)), so no new message is
needed. Every member sees who holds a host; administrators act, the split
[ADR-079](./ADR-079-rule-administration.md) draws for rules.

**No page carries a session token.** The session list and the device record name
the holder and the start time instead, and the fleet views show "Session open —
<name>, since <time>" wherever the host appears.

**Three releases, covering what the process can see and what it cannot.**

1. **An unpaired session releases its row** after a bounded wait. A session
   whose second side never arrives is not a session.
2. **The relay end deletes the row** when it ends the session, without needing a
   tenant in scope — the relay knows the session key, which is enough.
3. **A periodic sweep deletes rows the relay no longer holds.** This covers the
   case the process cannot see: a server that died holding sessions.

A lease column renewed by a heartbeat was rejected — it adds a write per session
per interval to solve a problem the relay already has the answer to. Deleting
every row at startup was rejected because it is only correct while there is one
replica. Filtering the read instead of deleting was rejected because it leaves
the rows there, and something else will eventually read them.

## Consequences

The sweep is keyed on what the relay holds, so it is correct whether the rows it
finds were left by this process or another.

A host's one session bounds a host's memory for sessions to one session's
worth ([ADR-127](./ADR-127-direct-link.md)). A technician who left a tab open on
a host they no longer need holds it until they close it, it drops, or an
administrator ends it.
