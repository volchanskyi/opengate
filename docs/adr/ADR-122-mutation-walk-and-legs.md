---
number: 122
title: A mutation shard walks the narrowest path holding its own units, and a leg publishes alone
---

# ADR-122 — A mutation shard's walk, and a leg that publishes alone

## Context

Twelve of twenty scheduled mutation nights produced no score at all, and the
cause was not the mutation tooling. Two structural choices turned an ordinary
flaky test into a total loss.

**Every Go shard surveyed the whole module.** `gremlins unleash` uses its path
argument twice: it walks it for mutants, and it runs `go test` over it to decide
which lines coverage reaches. Every shard was pointed at the module root, so
`tests/acceptance`, `tests/integration`, `tests/loadtest`, `tests/netfault` and
`tests/vmramseries` ran once per shard in the survey alone — twenty-seven times a
night, from cold under `GOFLAGS=-count=1`. One flaky test anywhere in them was
twenty-seven independent draws, and the three flakes fixed in the fortnight
before this all lived there.

**One lost shard voided all fifty-three.** The publish job carried a single
completeness boolean over the whole matrix and refused to emit any row unless
every shard was present. On six of the last ten red nights the failing leg was Go
alone: twenty-five Rust shards and the web shard had finished and their scores
were discarded. That is a detection gap as much as waste — a Rust regression was
invisible on any night the Go leg flaked.

## Decision

**A Go shard walks the narrowest path that still holds everything it mutates,
and the path is derived from the shard map rather than listed beside it.**
`mutation_go_shard_scan_path` reads a shard's own units: every unit under
`internal/` gives `./internal`, and a unit outside it gives the module root. A
unit moving out of `internal/` therefore moves its shard's walk in the same edit,
where a second list would be the thing that silently disagreed — a shard left on
the narrow walk whose file is no longer inside it mutates nothing and reports a
smaller number.

Today exactly one shard takes the module root: `go-observability-harness`, which
owns `tests/loadtest` and `tests/netfault`.

**The exclude regexes are stated in the walk's own coordinates, and anchored.**
gremlins matches them against the path it walked, so a shard walking `internal/`
sees `testutil/foo.go` and a rule naming `internal/testutil/` would match
nothing. Anchoring is load-bearing rather than tidy under that form:
`internal/api` and `internal/agentapi` become `api/` and `agentapi/`, and the
second contains the first as a substring, so an unanchored rule for one would
drop the other from mutation entirely in every shard that does not own it.

**A leg is published when all of its own shards finished.** The status builder
emits `complete_by_language` beside the existing `complete`, which keeps its
meaning exactly; the summarizer takes the set of complete legs and carries only
those; and the publish job merges, summarizes, uploads and pushes per leg. A leg
short a shard emits no score — a score over the shards that happened to finish is
a smaller number nothing marks as partial. A run with no complete leg at all
still fails at the publish step.

**An incomplete night is still a red night.** The gate job reads the whole-run
`complete`, unchanged. What moved is only that the legs which did finish are
published, and their regressions checked, before the run goes red.

## Why the narrowing is free, and where it is not

The reason to be careful is that the path decides what gets mutated as well as
what gets surveyed, and a narrowing that quietly drops mutants would cost score
rather than save time. Measured against `server/` at `1fe6ddd4`:

| | `gremlins unleash .` | `gremlins unleash ./internal` |
|---|---|---|
| Mutants over `internal/` | 2,152 runnable, 257 not covered | 2,152 runnable, 257 not covered |
| Coverage survey, 24 processors | 99.6 s | 73.5 s |
| `go test -cover`, 4 processors | 142.1 s | 115.1 s |

The two mutant sets are identical file by file, line by line, column by column
and status by status — an empty diff, not a matching total. The reason is that
`go test` coverage is per-package: without `-coverpkg` each package is measured
against its own statements only, so a harness package's tests never contributed a
line of `internal/` coverage to begin with.

Where it is not free is outside `internal/`. `tests/loadtest` and
`tests/netfault` carry 1,101 mutants, and in the run of 2026-09-12 their files
were 943 killed and 139 uncovered. Dropping them would have taken the Go leg from
2,996/3,485 = 85.9 to 2,053/2,403 = 85.4 against an absolute floor of 85.0 — so
the shard that owns them keeps the module root, and the score does not move at
all.

The per-mutant cost and the leash are unchanged, and deliberately so. The
coverage-elapsed ceiling the leash derives from describes the slowest survey any
shard runs, and that is still the harness shard's module-wide one.

## Consequences

- The harness packages run once a night in a survey instead of twenty-seven
  times. The flake exposure that produced twelve scoreless nights falls to a
  single draw; it does not go to zero, and `internal/api`, `internal/agentapi`
  and `internal/alerts` still run in all twenty-seven.
- No score moves, so nothing downstream is restated and the trend is continuous
  across the change.
- A Go flake no longer costs the Rust and web legs their scores or their
  regression checks.
- `mutation-workflow.test.sh` asks the question that decides a night — how many
  shards would mutate each file, given the walk each is pointed at and the regexp
  it is handed — rather than checking the unit map alone, and it refuses an
  unanchored exclude alternative and a shard whose walk cannot reach its own
  units.
- A leg whose test runner selected no tests measures nothing, and says so.
  [`assert-mutation-report.sh`](../../scripts/assert-mutation-report.sh) refuses
  a Stryker report holding a surviving mutant that tests cover and none of them
  completed against — the shape a runner that matches no test names leaves, a
  well-formed report scoring nought. The leg fails as incomplete rather than
  publishing a regression about tests that never ran. Stryker's vitest runner
  leaves exactly that on Vitest 5, which matches a test's name against its suite
  chain joined by `' > '` while the runner joins it with a space
  (stryker-js#6210); [`web/patches/`](../../web/patches/) carries the one-line
  correction, applied on every install, against the runner version pinned
  exactly beside it.
