---
number: 59
title: A session row is released when its session ends
---

# ADR-059 — A session row is released when its session ends

## Context

Each remote session writes a row saying a device is busy. Rows outlived their
sessions and devices showed as occupied by sessions nobody was in.

## Decision

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
