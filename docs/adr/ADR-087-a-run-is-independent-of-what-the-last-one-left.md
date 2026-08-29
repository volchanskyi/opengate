---
number: 87
title: A run is independent of what the last one left
status: Accepted
date: 2026-08-29
---

# ADR-087 — A run is independent of what the last one left

## Context

Three scheduled workflows were red, and none of them said why.

**The load test** had failed every night since 2026-08-22. The visible symptom
was always the same line, from k6: *no online machine to open a session against —
the QUIC harness must be holding a fleet connected while this scenario runs.*
Nothing in any run said what had happened to the fleet.

Measured against the live staging database on 2026-08-29: eight organizations
named `opengate-loadtest-customer-01` through `-08`, no sites, no load-test
accounts, two offline machines. The fleet builds its customers through the API
before it connects anything, a customer's name is unique inside its tenant, and
the customer names were built from the marker alone while every account address
already carried the run's seed. So the first night created eight customers, and
every night after it was refused the first customer it asked for, aborted the
fixture, and connected nothing. The relay scenario then reported the only fact it
could see: there was no machine.

Two things kept those customers alive. The staging deploy's reset truncates the
tables the browser suite needs empty — accounts, sites, machines — and
`organizations` was added to the schema three weeks after that list was written
and never added to it. And the run's own cleanup did not remove a customer at
all: it selected sites through `sites.owner_id`, a column dropped when the
organization became the visibility boundary, so the whole removal aborted on the
first statement and nothing was cleaned. Its unit tests drive a psql stand-in
that matches on statement text and knows nothing about columns, so the drift was
invisible to them.

The step that would have named all of this — the one that waits for the
backgrounded fleet, prints its log and reports its verdict — carried no
condition, so it ran only when everything before it had passed. It was skipped on
exactly the path where its output is the answer.

**The performance stack** had never had a green run. Both families stop before
their first phase with *the node's processor is 100% committed against a limit of
95% — production shares it.* Two faults compound. The reading is the one-minute
load average, which describes the minute before the check; on a runner that is
the minute the job spent building images and a two-thousand-machine fleet, so it
reports the build as the run's own commitment. And the ceiling it is compared
against protects a neighbour the disposable stack does not have — the profile's
own comment says a throwaway stack asks for no limits, while the schema made
declaring one mandatory.

**CI** failed its quality gate at `new_coverage` 47.3%. A refactor moved the
periodic workers into `server/internal/app/background.go`; git blame dates every
line of a split file to the split, so CI measured all eighty-nine as new code.
The local coverage guard reads `new_coverage` from the API — the blame-scoped
metric — during a scan that runs before the commit exists, so the lines being
committed carried no commit and were measured by nothing. The guard was green on
the exact change it exists to catch.

## Decision

### A run's names carry the run

Every name a fixture creates is built from the marker **and the run's seed** —
accounts already were, customers now are. Two nights then never ask the server
for the same thing, whatever the night before left behind.

### A safety ceiling belongs to the environment that has the thing it protects

The processor ceiling is a promise made to whatever else sits on the node.
`environment: staging` shares one with production and declares it;
`environment: runner` is created by the job and thrown away with it, has no
neighbour, and **may not** declare one — driving the processor is the scaling
sweep's experiment, and a ceiling nothing consults reads as protection that is
not there. The memory and disk ceilings hold everywhere, because past them the
node has nowhere to put what the run produces. The profile schema enforces both
halves, so neither can be written the wrong way round.

The processor reading is the run queue **at the instant of the check**, the
reader's own thread subtracted, reported uncapped. A node committed to four times
what it has and one exactly full are different findings, and the minute before
the check is not the check.

### Cleanup counts every kind, and the reset empties every kind

The run removes accounts, customers, sites and machines, and **counts each one**
before and after. Counting is not a report about the removal; it is the
removal's only check, and a kind that is removed but never counted is a kind
whose residue nobody can see — which is how eight customers survived a week of
cleanups that each reported success. The deploy's reset empties the same set and
gives every tenant its default customer back.

Both are held to the live schema by a test that runs the actual script against a
database the migrations built, seeded with one of every kind. A stand-in that
matches on statement text cannot see a column move; a real database sees it the
day it happens.

### The fleet's account of itself survives a failed scenario

The step that reads the backgrounded fleet's log and verdict runs on every path.
The path where a scenario failed is the path where that log is the answer.

### The local coverage guard reads the diff, not only the blame

[`sonar-coverage-guard.sh`](../../scripts/sonar-coverage-guard.sh) makes two
checks, and needs both because neither can see what the other does. The
aggregate covers the commits already inside the new-code period. The diff checks
every line this change adds or edits against the hit counts SonarCloud computed
from the coverage report just uploaded — file content, not blame — so the commit
in front of it is measured. An analysis it cannot query fails the guard rather
than passing it.

## Consequences

The load test builds a fleet the night after any other night, and after any
deploy. The performance stack can reach its first phase. A run that cannot build
its fleet prints the reason. A schema change that strips a column out from under
the cleanup fails a test in the same commit. A file split out of another is
measured locally at the coverage CI will measure it at.

The diff check adds one Sonar request per changed source file to the gauntlet's
sonar stage. A profile that wants a processor ceiling on a disposable stack has
to change the environment class or the rule, which is the point.
