# Data Erasure

Deleting a device, or purging a whole tenant, **irreversibly erases that
subject's data across every store** and deprovisions its agents. There is no
soft-delete, no grace window and no undo.

Use it for decommissioning, for offboarding a customer, and for answering a
right-to-be-forgotten request.

## What gets erased

| Store | Deleting one device | Purging a tenant |
|---|---|---|
| Time-series metrics | That device's series | Every device's series |
| Device records, inventory and process history | Erased with the device | Every device in the tenant |
| Alerts and their evidence | Erased, and affected incidents repaired | Alerts, incidents and their history erased |
| Cold-tier archives, where configured | That device's data | The tenant's data |
| The agent's own local store | Wiped when the agent next connects | Every agent in the tenant |
| **Audit events** | **Retained** — they are the proof the erasure happened | **Retained** |

The tenant record itself is retained for a tenant purge: it anchors the retained
audit trail and the deny-list.

> **Erasure runs entirely on the server.** The keys that authorise deletion in the
> metric store, and any archive credentials, never reach a device.

## Deleting one device

From the device page, **Delete device**, behind a confirm step. The agent is
deregistered, the machine's data is erased, and the machine cannot re-register
with the same identity.

### Incident counts are repaired, not left wrong

An incident's "how many alerts, across how many machines" counts are application
state. Erasing a machine's alerts would otherwise leave an incident claiming 40
machines when its fortieth was decommissioned last week.

So the erasure restates both counts from the alerts that survive — restating
rather than subtracting is what makes a resumed purge safe to run twice — and
closes any incident the erasure emptied, recording why. **No cause code is set**:
a cause code is a person's answer, and `false_positive` in particular decides
whether a rule gets retuned.

## Purging a tenant

Administrators start and watch a tenant purge from **Data lifecycle**
(`/settings/data-lifecycle`).

A purge is a persisted, resumable job. It reports progress through these stages:

| Stage | Meaning |
|---|---|
| `requested` | The subject is recorded as deleted and further writes are blocked |
| `central-logical-complete` | Deletion issued in the metric store; database rows removed |
| `central-physical-compaction-pending` | Waiting for the metric store to confirm it is empty |
| `object-delete-pending` | Clearing cold-tier archives, where one exists |
| `edge-erase-pending` | Waiting for agents to reconnect and wipe their local stores |
| `complete` | Verified empty |

**Logical completion is not physical completion.** Once ingest is blocked and the
subject is no longer queryable, the data is gone as far as anything can read it;
the metric store frees the disk later, on its own schedule.

The ordering is strict — record the deletion first, then metrics, then archives,
then the database row last — so a crash mid-purge leaves the subject marked
deleted rather than half-alive. Each stage is guarded, so an interrupted job
resumes safely from where it stopped.

## Nothing comes back

Every purge first records the subject in a permanent deny-list. Every write path
checks it, so no live stream, in-flight catch-up, or misbehaving agent can
re-create purged data:

- A connected agent is deregistered immediately.
- An offline agent is denied by its own identity the moment it reconnects.
- A tenant-level entry covers all of its devices.

The deny-list holds identifiers and the scope of the purge — never telemetry — so
it is kept indefinitely at negligible cost.

A periodic sweep also removes any metric series whose device no longer exists, as
a second line of defence: the stores are not one transaction, so a partially
failed purge is caught rather than left behind.

## Records also age out on their own

Erasure runs off a subject. A second, independent rule runs off age: an alert,
the evidence frozen with it, and the incident it folded into are kept for **one
year**, after which a periodic sweep removes them.

Two things bound what that sweep touches, so it can never take work somebody is
still doing:

- **An open incident is never removed, at any age.** Only a closed room is a
  candidate, and only once every alert that folded into it has itself aged out —
  so an alert never survives with its investigation detached.
- **Age is counted from when the record was received, not from when the event
  happened.** A retroactive finding is legitimately months old the day it
  arrives, and counting from the event would erase it before anyone read it.

Audit events are outside this, for the same reason they survive a purge: they
are the proof of what happened.

## The one caveat: database backups

The metric store keeps no backups, so its erasure is immediate.

Database backups are copies in object storage and cannot be surgically edited. A
purged subject is **fully erased only once every backup that still contains it has
aged out** under the bucket's retention policy. Plan retention accordingly when
answering a right-to-be-forgotten request.

## Related

- [Tenancy and Access](./Tenancy-and-Access.md) — what deleting a customer cascades
- [Fleet and Devices](./Fleet-and-Devices.md) — where the delete action lives
- [API Reference](../architecture/API-Reference.md) — the delete and purge endpoints
