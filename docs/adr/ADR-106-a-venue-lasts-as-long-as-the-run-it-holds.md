---
number: 106
title: A venue lasts as long as the run it holds
status: Accepted
date: 2026-09-09
---

# ADR-106 — A venue lasts as long as the run it holds

## Context

Three separate ceilings sat between the staging nightly and a run of any real
length. None of them appeared anywhere near the figure it capped, none was
declared as a limit, and each fails in a way that reads as something else
entirely.

| Where | What it was | What a run past it looks like |
|---|---|---|
| The pods holding the two generators | `sleep 1800`, and a fixed 1,500-second wait for a verdict | a harness that reached no verdict, which is what a broken cluster says too |
| The claim on the staging namespace | `leaseDurationSeconds: 2700`, written once at acquisition and never renewed | another run taking the namespace from under this one, with neither of them knowing |
| The credential the fleet enrols with | `expires_in_hours: 1` | every arrival past the first hour refused, reported as machines that failed to connect |

The profiles they had to cover asked for eight hours. The longest run that
could exist was thirty minutes, and nothing in any file said so.

The claim is the sharpest of the three, because it is the one where a run
carries on and reports numbers. Past forty-five minutes any waiter may take the
namespace; both runs then drive the same server, and every figure the first one
publishes afterwards is a reading of a system somebody else is also loading.

## Decision

**Everything a run's venue is given is derived from the run's own length.**

[`loadtest-run-budget.sh`](../../scripts/loadtest-run-budget.sh) takes the one
figure the workflow declares — how long the fleet is held — and produces the
pod lifetime and the wait for the verdict from it, plus the room the work either
side takes: the fixture build in front, and the k6 scenarios, bundle write and
collection behind. The margins are generous on purpose. A margin too small
costs a night with no measurement at all; one too large costs a pod idling in a
namespace the run already holds.

**A claim is renewed for as long as its holder is working.**
[`staging-lease.sh`](../../scripts/staging-lease.sh) leaves a renewer behind
when it acquires and stops it when it releases, renewing at a third of the
declared duration so two renewals can be missed before any waiter reads the
claim as stale. A renewal is a compare-and-set on the version just read, so one
that races a takeover loses rather than writing over the new holder.

**A claim taken anyway fails the run.** The renewer records what refused it and
the release step reports it and exits non-zero. Everything measured after that
moment was measured against a server another run was also driving, so a green
step there is the false green
[`ci-cd-determinism.md`](../../.claude/rules/ci-cd-determinism.md) exists to
refuse — and the release is the one step in the job that always runs.

**The credential outlives the run that spends it.** Its lifetime is the walk
rounded up to the hour, plus one for the fixture build. A longer-lived
credential is worth more to anyone who takes it, and the answer is that it is
deleted by the run's own cleanup rather than left to expire.

## Consequences

- A profile of any length is a matter of changing the profile. Nothing else has
  to be found and raised alongside it.
- The two runs that share the staging namespace are kept apart by the claim
  rather than by the gap between their crons, which is what makes
  [ADR-107](ADR-107-a-family-runs-somewhere.md)'s schedule-as-order workable.
- A job whose namespace was taken reports that rather than a set of numbers.
- The six-hour cap GitHub puts on a job is now the only ceiling left, and it is
  the reason the endurance run is five hours rather than eight.
