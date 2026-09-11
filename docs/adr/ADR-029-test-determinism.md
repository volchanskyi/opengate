---
number: 29
title: Every test runs on every machine
---

# ADR-029 — Every test runs on every machine

## Context

A test that skips itself when a dependency is missing passes on a machine where
it never ran. Nothing distinguishes that from a test that ran and passed.

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

## Consequences

The test run needs a container runtime. That is the cost, and it is smaller than
a suite that is green on a machine where half of it never executed.
