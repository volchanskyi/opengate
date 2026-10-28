---
number: 29
title: Every test runs on every machine
---

# ADR-029 — Every test runs on every machine

## Context

A test that skips itself when a dependency is missing passes on a machine where
it never ran. Nothing distinguishes that from a test that ran and passed.

A guard that sweeps the repository for a defect fails the same way. A sweep that
matched nothing reads like one that found nothing wrong, and so does one that
can no longer catch the shape it names.

## Decision

**No test skips itself.** Not for a missing database, not for an unset
environment variable, not for a platform. A result is a pass or a failure.

**A missing dependency is provisioned.** `server/internal/testpg` starts a
throwaway PostgreSQL container when no address is configured, so the
database-backed tests always run. It fails loudly if it cannot.

**A test that needs a file creates it** in its own temporary directory rather
than reading one off the host.

**Generator-style tests always do real work** — write to a temporary directory
and assert — and touch committed fixtures only under an explicit flag.

**A hook refuses the markers at edit time**: Go's skip calls, the web runners'
skip and focus forms, and Rust's ignore attribute. Focus markers are never
committed, because they silence every other test.

**A guard proves itself before it judges.** A test that sweeps the repository
for a defect:

- shows the defect first, against a case built to hold it, beside the shape it
  accepts;
- reads its subject whole, line continuations joined and workflows read as
  structure;
- counts what it read, and fails a sweep that read nothing;
- prints with each result the short name of its check, which each CI/CD rule
  lists under the section the check holds
  ([`rule-check-binding.test.sh`](../../scripts/tests/rule-check-binding.test.sh)).

## Consequences

The test run needs a container runtime. That is the cost, and it is smaller than
a suite that is green on a machine where half of it never executed.

A guard is longer than the sweep it runs. The demonstration is what tells a
sweep that still holds from one that has stopped matching.
