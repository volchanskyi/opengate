---
adr: 078
title: "The Triage Workspace Reads a Snapshot"
status: Accepted
date: 2026-08-17
---

# ADR-078: The Triage Workspace Reads a Snapshot

## Status

Accepted.

## Context

[ADR-077](ADR-077-investigations-api-and-the-keyset-triage-queue.md)
gave the triage queue an API. Nothing read it. Contoso's 02:41 driver rollout —
312 alerts across 40 machines folded into one incident — existed only as JSON.

Grafana has no ingress anywhere in the cluster: it is a platform-operator tool,
never a product surface. Every product view reads the OpenGate API, which is why
the workspace is built here rather than pointed at a dashboard.

Four questions decided its shape, and each has an answer that looks reasonable,
passes every test written by hand, and is wrong.

**Where the queue lives.** An incident in `new` *is* the triage queue
([ADR-075](ADR-075-incident-grouping-lifecycle-and-auto-resolve.md)). A separate
"investigations" list built on top of it would need a promotion step, a
conversion, and a second thing to fall out of sync with the first.

**What the room may fetch.** The strongest pull is a convenience one: an operator
looking at an alert from 02:41 wants the machine's current CPU, and the device
API is one call away. That call is the on-demand pull the programme forbids —
the edge is the only holder of high-resolution telemetry, and central asking a
machine for it at read time is the architecture it was built to replace.

**Which moves to offer.** The server enforces a four-status lifecycle and refuses
anything outside it. A UI that offers every status and reports the refusal is
correct and useless: the operator learns the rules by being told no.

**How much coverage to show.** A rule's coverage splits four ways — watching,
throttled, cannot evaluate, never reported — and the natural instinct is to show
the first and summarise the rest. Silent partial coverage is the exact failure
class this programme exists to eliminate, and a rule with six machines it cannot
evaluate would read as healthy.

## Decision

**The room's request boundary is a test, not a convention.** Opening an incident,
reading its evidence, moving it, assigning it and commenting on it issue requests
under `/api/v1/investigations` and nowhere else. This is asserted directly —
every request the room makes is recorded and checked — so a later convenience
fetch fails the suite instead of passing review. Machines are named and linked,
never queried.

**An absence is stated.** Evidence is frozen on the machine at the moment the
alert fires, and what is not in that snapshot is recorded nowhere. So the room
says which parts the machine recorded nothing for, says when the size cap cost
the blob something, and says when a room holds no alerts — rather than leaving a
gap a reader takes for a pending load. A machine erased under
[ADR-074](ADR-074-alert-store-accounted-ingest-and-the-erasure-cascade.md)'s
cascade takes its alerts with it and restates the incident's counts, so the room
states how many machines are on screen against how many the incident covers.

**The lifecycle is mirrored client-side and drives what exists.** The permitted
moves out of each status are declared in
[`incident-lifecycle.ts`](../../web/src/features/investigations/incident-lifecycle.ts),
so a resolved room offers no move at all and a transition the server would refuse
is never rendered. The vocabularies — statuses, severities, the closed set of
seven cause codes — are typed against the generated API types with `satisfies`,
so a value the spec grows fails to compile here instead of rendering as a raw
wire string. Resolving asks for its cause code before it sends, because that
answer is what the curated rule pack is retuned from. A refused move surfaces the
server's own words and changes nothing locally.

**All four coverage states are shown, against the fleet size, and a split that
does not add up is the finding.** The API guarantees the four sum to the fleet;
when they do not, the room says so rather than presenting the numbers as fact.

**Evidence is drawn without a charting engine.** A series is a fixed handful of
frozen points, and the room draws it as inline SVG whose accessible label carries
the same reading in words. The lazy uPlot chunk stays where it is — on the device
page — and the initial bundle stays inside its budget
([`.size-limit.json`](../../web/.size-limit.json)).

**The API client serializes every array query parameter comma-joined.** Every
array parameter in [`openapi.yaml`](../../api/openapi.yaml) is declared
non-exploded, and the generated server binds each as one comma-separated value.
openapi-fetch explodes by default, so a repeated parameter would have arrived
with every value after the first dropped and no error anywhere. The setting is on
the client itself ([`api.ts`](../../web/src/lib/api.ts)) because it is a property
of the whole spec, not of one call.

## Consequences

The room is legible offline of the fleet: an incident from a machine that has
since been erased still opens, still reads, and still resolves.

Naming a machine by the leading block of its id rather than its hostname is the
price of the request boundary — the incident read carries `device_id` and no
hostname, and joining one in would mean a fleet read from the room. The link to
the machine's own page is the way through.

A new status or cause code in the spec breaks the build here until its label is
written, which is the intended cost: an unlabelled vocabulary member would
otherwise reach an operator as a wire value.

The evidence sparkline will not grow into a general chart. A richer reading of a
window belongs on the device page, where the charting engine already is.
