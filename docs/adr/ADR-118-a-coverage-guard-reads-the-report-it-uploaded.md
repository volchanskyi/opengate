---
number: 118
title: A coverage guard reads the report it uploaded
status: Accepted
date: 2026-09-10
---

# ADR-118 — A coverage guard reads the report it uploaded

## Context

Every `new_*` condition on SonarCloud is scoped by git blame, and the gauntlet
scans *before* the commit exists — so the lines just written carry no commit,
sit outside the new-code period, and are measured by nothing. Three guards run
after `make sonar` to close that, each reading a measure computed from file
content rather than from blame
([`sonarcloud.md`](../../.claude/rules/sonarcloud.md)).

The coverage guard's second check reads, for every changed file, the per-line
hit counts from the analysis just uploaded, and compares them against the lines
the diff touched. It had done that for 486 commits.

Then it refused, and went on refusing: *SonarCloud reported no coverage figures
for any changed file*. Two complete scans later it still refused, and the reason
was not the change. Asked directly, SonarCloud had **no file components at all**
on `dev` — not for the files this change touched, not for
`server/internal/cert/cert.go`, `web/src/App.tsx` or a Rust crate root, none of
which the change goes near. The project-level analysis was there and current,
with coverage at 89.6%.

`dev` is a short-lived branch: the instance's long-lived pattern is
`(branch|release)-.*`, which `dev` does not match, and `main` — the one branch
that does — holds all 168 files. A short-lived branch keeps file-level data only
where *that branch* changed the file. So what the analysis holds per file is not
a fact about the coverage report at all; it is a fact about which files the
branch's own commits touched.

Which makes the guard's source blame-shaped in exactly the way the guard exists
to escape. It worked for 486 commits because a commit usually follows one that
touched the same tree. It stopped working the first time the preceding commit
touched only test-harness files, which the guard excludes from its source set —
leaving no guarded file with a component, for a change that had just added a
well-covered one.

## Decision

**The guard reads the coverage from the reports the scan uploaded, where the
analysis has nothing to say.**

[`sonar-project.properties`](../../sonar-project.properties) names all three —
the Go cover profile, the web LCOV, the Rust LCOV — and they are generated
immediately before the scan. They describe the working tree, so their line
numbers are the ones the diff names and there is no blame gap at any point; and
they are the same numbers SonarCloud was handed, so the figure the guard
computes is the figure the gate will compute once the lines have a commit.

The analysis is still asked first, and still wins wherever it answers. That
keeps the read-back that proves the upload arrived
([`ci-cd-determinism.md`](../../.claude/rules/ci-cd-determinism.md)): a file the
branch *did* change is read from what SonarCloud stored, not from a local file
that might never have been sent.

**Neither source answering is still a refusal.** A guard that cannot ask must
not answer, and the repair narrows what "cannot ask" means rather than removing
it.

## Consequences

The guard now answers on the change in front of it rather than on the change
before it, which is what it was written to do. On the commit that exposed this
it reads 147 of 156 changed lines covered.

The fix is deliberately not "make `dev` a long-lived branch". That would change
what the gate measures — a long-lived branch gets its own new-code period rather
than everything since it left `main`, which is the boundary
[`sonarcloud.md`](../../.claude/rules/sonarcloud.md) is written around — and it
would leave the guard depending on a branch-retention policy nobody here
controls. Reading the report removes the dependency instead of re-tuning it.

The generalisation is worth stating: **a reading taken from a store is a fact
about what the store kept, not about what was measured.** The cache read-back
([ADR-086](ADR-086-the-cluster-is-the-source-of-truth-for-what-is-deployed.md))
asks a store whether it kept a write, which is the right question to ask a
store. This asked a store for a measurement and got its retention policy back
instead. Where the measurement itself is still on disk, that is what to read.
