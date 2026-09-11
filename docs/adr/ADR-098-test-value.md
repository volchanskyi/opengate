---
number: 98
title: A test asserts on the code that ships
---

# ADR-098 — A test asserts on the code that ships

## Context

A test file carried the browser's authentication middleware copied out of the
module beside it, twice, and asserted on the copy. Deleting the real
registration left both tests green, as did renaming the header. Every
technician's browser could have stopped sending its credential with nothing
going red.

Nothing else could see it. The file reported two passing tests. Coverage counted
the copy's lines as covered, because they ran.

## Decision

**A test exercises the shipped module, never a copy of it.** A copy is correct
forever, because nobody edits it again.

**A test does not reshape production to be testable.** No export, factory or
seam that exists only so a test can reach something. Where a module is awkward,
the answer is a test approach that works against it as written — mock the
genuine third-party boundary and invoke the real code.

**A test puts back what it changed.** A global or prototype reassignment with no
restore makes every later test's result depend on what ran before it.

**A test asserts behaviour the product has.** Building a literal and asserting
the field you just set tests nothing. Nor does asserting what a documented
no-op returns, or that a third-party library works.

**Assertion shape is not evidence of value, and grading by it deletes working
tests.** Measured against the nightly breakage report, files with the most
"weak-looking" assertions missed 5.8% of breakages and files with the fewest
missed 9.3% — the opposite of the expected direction. So a presence-only
assertion whose query pins a real string, a styling assertion where colour is
the signal, a page-structure walk, and a client constant pinned against an
external contract are all kept. A change that lowers what the suite catches is
not a cleanup.

## Consequences

The guard refuses only what the measurement supports: a test that never binds
the main export of the module it is named for, and an unrestored global. The
rest is prose a reviewer applies, deliberately, because a matcher cannot judge
it. The rule is written down in
[`test-value.md`](../../.claude/rules/test-value.md).
