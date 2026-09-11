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
2. **A step in the pre-commit run**, which fails when any single file's grade
   drops below B+ since the last run. Per-file, because a repository average
   moves too slowly to be actionable.
3. **A daily analytics run** publishing the trend to Grafana.

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
