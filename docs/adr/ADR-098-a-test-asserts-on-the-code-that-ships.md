---
number: 98
title: A test asserts on the code that ships, and assertion shape is not evidence of value
status: Accepted
date: 2026-09-04
---

# ADR-098 — A test asserts on the code that ships, and assertion shape is not evidence of value

## Context

The brief was to cut unit-test maintenance burden: prefer behaviour-driven
journeys, delete tests that check framework behaviour or functionality that does
not exist, avoid self-verification loops and over-mocking, and prove the suite
fails when the code is broken.

A census was run over all four test trees — 1,526 Go test functions, 862 Rust
tests, 1,554 web tests, 86 shell test files — with the breakage evidence taken
from a full mutation run rather than inferred. **It refuted the premise it
started from, and the refutation is the reason this record exists.**

Upkeep is not a burden. Across every web test file changed six or more times in
a year, test files changed *less* than the code they cover — 159 test-file
commits against 166 source-file commits. The whole web unit suite runs in 15
seconds. Across 1,554 unit, 32 integration and 77 end-to-end tests there is
exactly one duplicated name, and between the Go acceptance tier and 1,269 Go
unit tests, none.

**Assertion shape does not predict detection, and grading by it inverts.**
Grading every web test file by assertion form against the breakage report:

| Test-file weakness by assertion shape | Mean unnoticed-breakage rate |
|---|---|
| ≥30% "weak" assertions | 5.8% |
| <10% "weak" assertions | 9.3% |

`use-visible-interval.ts` is 100% "weak" by shape and catches 22 of its 24
breakages. `format-bytes.ts`, the exemplar with no weak assertions at all,
misses 13.3%. An earlier reading of the same tree claimed 238 worthless
presence-only tests and a 29% cut in web test lines; of 358 presence-only
blocks, 262 use a query that pins a literal string or an accessible name, so the
query *is* the assertion, and most of the rest are legitimate negative
assertions. **A rule that deleted tests by assertion shape would have deleted
working tests and left the broken ones.**

What the census did find is a small set of tests that provably cannot fail, and
four defects — one of them live, in the credential path.

`web/src/lib/api.test.ts` carried the browser's authentication middleware copied
verbatim out of `api.ts`, twice, and asserted on the copy. It imported one
unrelated constant from the real module and never the client itself. Deleting
`api.use(authMiddleware)` from production left both tests green, as did renaming
the header or dropping the `Bearer` prefix — every technician's browser could
have stopped sending its credential with nothing anywhere going red. Nothing
else could see it either: the file reported two passing tests, coverage counted
the copy's lines as covered because they executed, and the breakage report
showed `api.ts` carrying ten breakable points and its test reaching none, which
is a number nobody reads per file.

Beside it: a rule-staging test that computed its expectation by calling the
function under test and accepted a ±50% band around production's own answer; an
untested session-rehydration path; and a repository layer with no completeness
gate for tenant isolation, where the database layer has an excellent one.

The Rust agent's figure was measuring code no customer runs. Nine `edge-tsdb`
modules sit behind a `bakeoff` feature that every consumer disables, and the
mutation tool builds with default features, so 61 of the Rust leg's 299
unnoticed breakages — a fifth — were in a bake-off reference implementation.

## Decision

**A test asserts on the code that ships, and nothing about how it asserts is
evidence of what it is worth.** The rule lives in
[`.claude/rules/test-value.md`](../../.claude/rules/test-value.md) and states
both halves, because the second half is the one a later reader will be tempted
to undo.

Four things a test may not be:

1. **A copy of the shipped module.** Copying production code into a test and
   asserting on the copy produces a test that passes for as long as the copy is
   correct, which is forever.
2. **A reason to reshape production.** When a module is awkward to test, the
   answer is a test approach that works against it as written — not an export, a
   factory or a seam that exists only so a test can reach it. `api.test.ts` was
   fixed by mocking `openapi-fetch`, a genuine third-party boundary, capturing
   what the module registers at load and invoking the **real** middleware.
   Production is unchanged.
3. **A change to the environment it does not put back.** A global or prototype
   reassignment with no restore makes a verdict depend on what ran before it.
4. **An assertion about behaviour the product does not have** — a literal
   compared with itself, a compile-time trait check whose body cannot fail at
   runtime, a documented no-op's return value, or a third-party library's own
   behaviour.

Two of those are matched by a write-time hook and a repo sweep over the same
analyser: a test file that never binds the primary export of the module it is
named for, and an un-restored global or prototype reassignment. **The rest is
prose a reviewer applies, deliberately** — a matcher cannot tell a decorative
class assertion from one where colour is the only carrier of state, and the
measurement above says a matcher that tried would do net harm.

So the rule also states, as a standing instruction, what is **not** refused: a
presence-only assertion whose query pins a literal or an accessible name; a
styling assertion where colour is the product signal; a page-structure walk,
including the three that are the only way to assert a technician's comment
rendered as characters rather than as an element; a `does_not_panic` test on a
path a real machine takes; and a seam test pinning a client constant against an
external contract. **A change that lowers the caught-rate is not a cleanup,
whatever it does to the line count.**

Scope follows shipping, not convenience: code behind a feature no consumer
enables is carved out of the mutation run by name, with the reason written
beside it, and a build-graph test holds the premise that every consumer keeps
the feature off. An exclusion is re-earned — a sweep fails on an entry whose
file now passes, so it cannot outlive its reason.

## Consequences

The credential path is exercised by its own test, and breaking it three
different ways now turns that test red. A canary meant for five machines
reaching two hundred fails the rollout test. A new tenant-scoped repository
cannot ship without a proof that it refuses another customer's rows. The Rust
figure describes the agent customers run.

Thirty tests that could not fail are gone, and no test was deleted for looking
weak. Four separate would-be-changes were withdrawn during the review — a
112-assertion rewrite, a conversion of 89 structure walks to role queries, a
production factory added for a test's benefit, and a cull of the Null-implementation
tests that a container-hosted machine actually exercises — each because it would
have lost detection or added flakiness. That record is kept so the same ideas
are not re-proposed.

The cost is that most of this rule is judgement rather than a matcher, and
judgement decays. What holds it is the measurement being repeatable: the caught
rate per file is a number any change can be graded against, before and after.
