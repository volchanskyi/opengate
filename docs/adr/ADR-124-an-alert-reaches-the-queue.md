---
number: 124
title: An alert a machine raises reaches the queue
---

# ADR-124 — An alert a machine raises reaches the queue

## Context

A machine detects that something is wrong, freezes what it knew at that moment,
and holds it. The server admits an alert, folds it into a room, and serves that
room to a technician. Both halves were built. Nothing joined them, so every
alert every machine raised went into a bounded queue on the machine, aged out,
and was discarded — and a triage queue that is always empty is
indistinguishable from an estate where nothing is ever wrong.

## Decision

**The machine offers its queue on its heartbeat and keeps each alert until it
is answered.** One alert at a time; an offered alert waits under the id it was
sent with ([`sink.rs`](../../agent/crates/mesh-agent-core/src/alerts/sink.rs)).
The server answers every alert from a machine that advertises `AlertAnswers`
exactly once, with `AlertAnswer`: `Stored` once the row is written or found
already written, `RefusedForGood` for an alert that would be refused the same way
every time — a bad shape, a rule it does not ship or that is stopped, a time
outside the window, unreadable evidence, a customer over its hourly ceiling — and
`TryAgain` for one that might pass later: a missing customer, a failed write, no
free persist slot. `Stored` and `RefusedForGood` end the alert; `TryAgain` puts it
back at the front of the queue for the next heartbeat, and a new session offers
every alert still waiting. A word the machine does not know reads as `TryAgain`,
so an answer a later server adds keeps the alert rather than losing it. A send
that fails on a closed connection is dropped by the server, and the machine
offers again on reconnect. Queue and waiting alerts share the queue's capacity,
oldest dropped and counted: the undelivered alerts are the older ones, and a
reconnect that delivered a stale backlog in preference to the present would
answer the wrong question. A machine without the capability is served as before:
a send that fails hands back what did not go.

**A re-delivery resolves to the row already written.** The identity is the
machine, the rule, the rule's revision and the window it fired for, and none of
those moves across a failed send. That is what makes handing an alert back safe.

**A rule travels carrying its revision and how bad it is.** The far end refuses
an alert that states no revision, because an alert without one would duplicate
itself on every reconnect; and a queue ordered by severity cannot order one that
states none. A machine cannot state either unless it was told, so both are on
the rule.

**An episode is one alert.** The evaluator separates the instant a rule starts
firing from the seconds it goes on firing. A disk over its line for ten hours is
one thing that happened, and the window the alert names is the stretch the rule
actually held over — from where the breach began, not from where the hold
elapsed.

**Evidence is composed and packed where the rule fires.** The machine's own
ranking of what else moved, the readings behind it, and what was running, all
redacted before the alert exists. Central keeps a sixty-second average per
dimension and never asks the machine afterwards, so what is not on the message
is recorded nowhere.

**A rule the machine owns is registered centrally rather than evaluated there.**
The phrases a log rule matches on are what the machine's log reader is made of,
so they stay compiled into it; the server holds its name, its revision, its
severity and where its alerts belong. Without that the server refuses every
alert the rule raises, which reads exactly like a machine that raised none. A
rule of that kind states no reading, no comparison and no line, has nothing to
retune, and costs the machine nothing per rule — one bounded poll a minute
covers the whole pack.

**Stopping one of those is enforced where the alert arrives.** A rule about a
reading is stopped by not being sent. A rule the machine carries goes on
matching whatever anybody set, so the customer's decision is read beside the
ruleset the connection was given and applied at admission, under its own counted
reason.

**A rule change reaches machines that are already connected.** A link is held
open for as long as it is healthy, so a change delivered only on registration
would be delivered only when something unrelated broke it. A stop, a resume and
a retuned number all go out to that customer's connected machines as they are
made. The push is best effort and never fails the change: what was asked for is
already stored, and a machine that could not be written to takes it as it
reconnects.

**A finding out of history is measured against how long an alert is kept.** It
was measured against how long a metric sample is kept, which is thirty days —
and a machine's own store reaches months further, so most of what a first scan
could answer was refused on arrival and counted as a clock fault. The retention
sweep already ages a row from the day it arrived, precisely so a legitimately
old finding gets a full year of somebody's attention.

## Consequences

The redaction that strips secrets out of what an alert carries now runs outside
a test for the first time, because it sits on the path an alert takes.

The nightly link-failure drill makes its machine find something wrong while it
is dark and asks whether the incident survived. It aims the rule at what that
machine's own disk is doing rather than at a number written down, and a machine
whose disk is under the lowest line the rule allows reports that it could not
observe rather than reporting a pass.
