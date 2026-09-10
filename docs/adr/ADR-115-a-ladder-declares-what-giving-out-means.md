---
number: 115
title: A ladder declares what giving out means
status: Accepted
date: 2026-09-10
---

# ADR-115 — A ladder declares what giving out means

## Context

The breakpoint family exists to raise the load until something gives and report
what gave. The answer it is supposed to produce is three things: the last load
that held, the first that did not, and which reading said so.

It has never produced any of them. The profile declared a ladder and the run
walked it, and what came out was a bundle of phases with no statement about which
of them was the point. Whoever read it was left to decide, after the fact,
whether a rung had given out — which means the family's answer was whatever the
run happened to survive, and it moved with whoever was reading.

The 2026-09-09 ladder is the demonstration. It reached four thousand machines
with no errors at all and every leg valid. That establishes nothing except that
the answer is higher than four thousand, and it was reported as a clean night.

The gap is one the profile is already the home for.
[ADR-101](ADR-101-one-measurement-one-limit-one-file.md) settled that every
number a night is judged by lives in the profile, because a limit kept somewhere
else is a limit an edit does not reach. A definition of *giving out* is such a
number, and it was in no file at all.

Three kinds of giving out are worth telling apart, and only one of them is loud:

- **Work refused or dropped.** The system says no. It usually moves first and it
  is unmistakable.
- **Work done, far too slowly.** The shape a technician actually meets — the
  fleet page still loads, and it takes eleven seconds. It can arrive with no
  errors at all, so an error-rate definition alone never sees it.
- **The target out of the processor it was given.** This is the one that says the
  hold-up is the hardware rather than the code. Neither of the other two can tell
  those apart, and the reading only became available with
  [ADR-109](ADR-109-a-sweep-varies-one-thing-and-somebody-reads-it.md)'s per-phase
  figure for how hard the target worked.

## Decision

**The profile writes down what counts as giving out, and the run reports the
rung that held, the rung that did not, and the reading that decided it.**

`gave_out` names any of the three terms above; a rung crossing any one of them
has given out. A profile in the breakpoint family that declares none is refused,
because a ladder with no definition reports whatever it survived. A term the run
could not read decides nothing — an absent busy-ness is a question nobody asked,
which is not the same as a target at rest.

Only rungs are searched. A ladder ends at the load it started from so the report
can say whether the system came back, and a phase at or below the level before it
is that wind-down rather than a step up.

**The answer states how many rungs it read.** "Nothing gave out" is an answer
shaped as an absence, and an absence is satisfied by the absence of the whole
conversation: a ladder whose phases never arrived reports it exactly as readily
as one that held all the way up. The count travels in the bundle — schema 6 — and
a bundle carrying an answer that read no rung is refused, which is
[`ci-cd-determinism.md`](../../.claude/rules/ci-cd-determinism.md)'s rule about
absence-shaped checks applied to a measurement rather than to a gate.

## Consequences

The committed ladder declares an error rate above 5%, a 95th-percentile wait
above two seconds, or the target past 95% of its processor allowance. All three
are loose against what the ladder has measured — its walked rungs returned no
errors and middle-case waits of 19 to 62 milliseconds — deliberately, so the
first thing the family reports is the system giving out rather than the
measurement's own noise.

The answer is printed beside the run's results as well as written into the
bundle, because whoever is looking at a red ladder wants the rung rather than the
file.

What this does not do is decide whether giving out is a failure. The family's
gates are loose on purpose — failing under overload is the finding it exists to
produce — and what they hold is the recovery. This adds the reading; whether a
particular rung is a regression is a comparison against the nights before it,
which needs nights of it first.
