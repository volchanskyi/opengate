---
number: 19
title: PMAT runs alongside the existing gates
---

# ADR-019 — PMAT runs alongside the existing gates

## Decision

**PMAT is added at three points, each switchable on its own, and it replaces
nothing.**

1. **A server for the coding agent**, so complexity and duplication can be asked
   about while working.
2. **A step in the pre-commit run**
   ([`pmat-precommit.sh`](../../scripts/pmat-precommit.sh)), which fails when
   any file the change touches grades below B+: Go, Rust, TypeScript and
   JavaScript through PMAT's own check, generated code left out, and Python by
   its own score, 80 or more, since PMAT's check holds Python to B whatever
   floor it is asked for. Per-file, because a repository average moves too
   slowly to be actionable. It asks for the PMAT version the manifest pins, so a
   version move reaches this check and the nightly together.
3. **A daily analytics run** publishing the trend to Grafana, and failing the
   night, with an alert naming the branch it graded, when any file but
   generated code sits below B+ by the same rules, when the repository score
   falls under its committed floor
   ([`pmat-summarize.sh`](../../scripts/pmat-summarize.sh)), or when either
   worsened since the night before. A rule against the night before alone
   alerted on a regression once and passed it the next night.

The mutation engines, SonarCloud, the lints, the security audits, the secrets
and infrastructure scanners, coverage, taint and dead-code sweeps all stay
exactly as configured.

**Automatic fixing stays off.** PMAT's auto-commit mode would commit as the CI
identity, write source ahead of any test, and push ahead of the refactor
marker — three things the commit hooks refuse, correctly. It runs in dry-run
only, and its suggestions go through the ordinary flow.

**Its strict compliance rule set is not adopted** until it has been read against
the no-suppression policy.

## Consequences

The grade is driven mostly by file size, so the usual response to a failure is
to split a file along a seam that already exists.
