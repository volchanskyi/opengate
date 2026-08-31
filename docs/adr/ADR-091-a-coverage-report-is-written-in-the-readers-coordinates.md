---
number: 91
title: A coverage report is written in the reader's coordinates
status: Accepted
date: 2026-08-31
---

# ADR-091 — A coverage report is written in the reader's coordinates

## Context

`sonar.rust.lcov.reportPaths` had named a report since the scanner was first
wired up, and no Rust file has ever carried a coverage figure. Not a low one —
none. `coverage` and `lines_to_cover` are null for every Rust component across
the project's entire measure history, while both the CI job and the local
gauntlet went on passing a ≥80% line-coverage check against the same report.

`cargo llvm-cov --lcov` writes each `SF:` record as an absolute path on the
machine that produced it. Every scanner that reads the report analyses the tree
from inside a container mounted somewhere else — `/usr/src` for the Docker
scanner `make sonar` uses, `/github/workspace` for the scan action CI uses — so
each path resolved to no indexed file and its coverage was discarded. The
TypeScript report, written with paths relative to its own module, imported
correctly the whole time, which is why the failure looked like nothing at all
rather than like a broken upload.

It surfaced only because a change came along whose sole Sonar-analysed source
files were Rust, and the new-coverage guard's second check refused to pass on a
diff that SonarCloud had no hit counts for. A guard written for a different
reason found this one.

Two further things were true underneath it. The `--ignore-filename-regex` in the
Makefile carved out four files the CI job and the gauntlet both measured, so the
report the gate read described a narrower workspace than either ≥80% job ever
ran — and nothing compared the two lists. And the whole Rust workspace clears
86.9% with those four included, so the carve-outs were not holding anything up.

## Decision

**The report is written in the coordinates of whatever reads it, and then
asked whether that worked.**
[`rust-lcov-relativize.sh`](../../scripts/rust-lcov-relativize.sh) rewrites every
`SF:` record to a repository-relative path and then reads the result back: it
fails on a report still naming an absolute path, and on one naming no source at
all. The rewrite is not the guard — a rewrite that silently matched nothing is
the same false green in a new place. Both the Makefile target and the CI job run
it, so the artifact CI uploads is already portable.

**The list of what is not measured lives in one shape everywhere.** The
Makefile's ignore list is held equal to CI's by
[`sonar-coverage-exclusion-guard.sh`](../../scripts/sonar-coverage-exclusion-guard.sh),
which previously compared only CI and the gauntlet. The Makefile is the one that
generates what the gate reads, which makes it the one where a drift is invisible.

**The four carve-outs are removed rather than moved.** `main.rs`, `webrtc.rs`,
`terminal.rs` and `session/relay.rs` are measured, at 23.7% to 87.6%, and the
workspace still reports 87.3% to the gate. An exclusion is re-earned
([`coverage-exclusions.md`](../../.claude/rules/coverage-exclusions.md)), and
these were not being asked to hold anything up. No entry was added to
`sonar.coverage.exclusions` for them.

## Consequences

Rust joins the gate: 13,454 lines to cover at 87.3%, where before it contributed
nothing in either direction. New-code coverage moved from 91.6% to 90.9% —
Rust's new lines are covered at roughly the rate the rest of the tree is, which
is the finding that could not be made while the language was invisible.

The four files that were carved out are now visible as the low points they are.
That is the intended cost: a number nobody could see was not protecting them.
