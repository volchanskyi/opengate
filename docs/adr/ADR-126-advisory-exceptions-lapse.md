---
number: 126
title: An advisory with no fixed release gets an exception that lapses on its own
---

# ADR-126 — An advisory with no fixed release gets an exception that lapses on its own

## Context

The lockfile audits read the advisory database as it stands on the day, not the
diff, so an advisory published today fails every commit from then on. Usually
the answer is a version bump.

On 2 October 2026 GitHub reviewed GHSA-vfj7-8cjw-p6xm against `braces`: every
release is affected, none is patched, and the maintainer disputes the report.
`braces` reaches the web tree only through `micromatch`, inside three
development tools already on their newest releases, which read only patterns
written in this repository. With no release to move to, the audit refused every
commit, and nothing in the repository could change its answer.

## Decision

- [`npm-audit.sh`](../../scripts/npm-audit.sh) is the only npm audit. The
  gauntlet and CI's security-audit job run it once per lockfile directory. It
  fails on any high or critical advisory that
  [`npm-audit-exceptions.json`](../../scripts/lib/npm-audit-exceptions.json)
  does not name for that directory.
- An exception names one advisory, the directory, the package, the newest
  release of that package when the entry was written, a review date and a
  reason. Every field is required.
- An exception fails the audit on its own when its review date passes, when its
  package publishes any other release, or when the audit stops reporting its
  advisory. A new release may be the fix; an advisory that is withdrawn or
  outgrown leaves an entry that excuses nothing.
- An exception is for an advisory with no fixed release. An advisory with a fix
  is fixed.
- An audit the script cannot read is retried after a pause; an audit that
  reports a finding runs once.

## Consequences

- An advisory with no fix blocks until someone writes down why it is tolerable
  and until when, in a change reviewed like any other.
- Every other advisory in the same directory still blocks, including a second
  one against the same package.
- On the review date the gauntlet and CI fail until the entry is renewed with a
  fresh reason or removed.
