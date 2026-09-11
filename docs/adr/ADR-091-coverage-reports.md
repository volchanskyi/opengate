---
number: 91
title: Coverage reports and the guards that read them
---

# ADR-091 — Coverage reports and the guards that read them

## Context

`cargo llvm-cov` names every file by its absolute path on the machine that ran
it. Every scanner reads the tree from inside a container mounted somewhere else,
so those paths matched no indexed file and the coverage was dropped without a
word. No Rust file had ever carried a coverage figure, for the life of the
project, while two 80% jobs passed against that same report.

## Decision

**The report is rewritten into the coordinates of whatever reads it, and then
asked whether it worked.**
[`rust-lcov-relativize.sh`](../../scripts/rust-lcov-relativize.sh) fails on a
report that still names an absolute path and on one that names no source at
all. The rewrite is not the guard; the read-back is.

**The list of what goes unmeasured has one shape everywhere it is written** —
the scanner configuration, the per-language jobs, the local run, and the target
that generates the report. A path exempt in all four is checked by nothing, and
that had already happened.

**Exclusions are re-earned, not inherited.** Four carve-outs were deleted rather
than moved, because the code had gained a harness since.

**The local guard reads the coverage it uploaded.** A branch that has just left
`main` holds file-level data only where it changed a file, so asking the
analysis about a file it holds nothing for answers about the previous commit
rather than about the coverage. The guard asks the analysis first and falls back
to the report the scan just uploaded. Neither answering is a refusal, not a
pass.

**The refusal is scoped to what the gate actually measures, and the report's own
read-back is what scopes it.** Three silences look identical from the guard and
only one of them is the defect:

- a file the analysis never indexes, or one the coverage exclusions name, has no
  figure anywhere by design. The guard takes both lists from the analysis's own
  configuration rather than keeping a copy, so a change confined to the load
  harness and a documentation tool is nothing to cover rather than a permanent
  refusal.
- a file with no executable line — a Rust module that is doc comments and `pub
  mod` declarations — gets no record from `llvm-cov` while every other file in
  its crate does. A report that names sources was read, so a file missing from
  it has nothing to execute.
- a report that names nothing is the measurement going missing, and that still
  refuses.

## Consequences

Coverage exclusions carry a justification each, name files rather than
directories, and are held in agreement across the four places by
[`sonar-coverage-exclusion-guard.sh`](../../scripts/sonar-coverage-exclusion-guard.sh).
