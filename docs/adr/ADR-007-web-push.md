---
number: 7
title: Web push, with the keypair in the data directory
---

# ADR-007 — Web push, with the keypair in the data directory

## Context

Changing the push keys invalidates every existing browser subscription.

## Decision

Standard web push with no third-party push service. The server generates a VAPID
keypair on first run and persists it to `{dataDir}/vapid.json`. A `Notifier`
interface keeps push out of the HTTP handlers.

## Consequences

The key file is mounted from the same Secret as the other server keys
([ADR-034](ADR-034-server-keys-from-a-secret.md)).
