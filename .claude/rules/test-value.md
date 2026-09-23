# Test Value — A Test Asserts On The Code That Ships

**Enforced by:**
[`.claude/hooks/pretooluse-test-value-guard.sh`](../hooks/pretooluse-test-value-guard.sh)
(write time) and
[`scripts/tests/test-value.test.sh`](../../scripts/tests/test-value.test.sh)
(gauntlet shell-tests step), both over
[`scripts/test-value-check.sh`](../../scripts/test-value-check.sh).
**No bypass.**

Companion to [`tests-determinism.md`](tests-determinism.md), which governs
whether a test runs. This governs what it runs against.

## The rules

### A test exercises the shipped module, never a copy of it

- Production code is not copied into a test file and asserted on.

### A test must not drive production code

- No export, factory or seam exists only so a test can reach it.
- Mock a genuine third-party boundary, capture what the module registers at
  load, and invoke the real code.
- The one sanctioned seam is the fault-injection substitution point, supplied at
  test time rather than compiled into the shipped binary
  ([`ci-cd-determinism.md`](ci-cd-determinism.md)).

### A test leaves the environment as it found it

- A global or prototype reassignment is restored in a `try`/`finally`, an
  `afterEach` or an `afterAll`.
- Reference shape: `web/src/features/devices/DeviceList.test.tsx`.

### A test asserts behaviour the product has

Two classes are deleted on sight:

- **A test of the test** — building a literal and asserting the field just set;
  a store's "initial state" test asserting a literal equals itself; a
  compile-time trait check whose body cannot fail at runtime.
- **A test of behaviour that does not exist** — pinning a documented no-op, or
  asserting a third-party library's own behaviour.

## What is deliberately not banned

Assertion shape is not evidence of value. Measured against the nightly breakage
report:

| Test-file weakness by assertion shape | Mean unnoticed-breakage rate |
|---|---|
| ≥30% "weak" assertions | 5.8% |
| <10% "weak" assertions | 9.3% |

None of the following is refused, and removing them is a regression:

- A presence-only assertion whose query pins a literal string or an accessible
  name.
- A styling assertion where colour is the product signal.
- A page-structure walk — `closest('li')` to scope an assertion to one row,
  `container.querySelector('script')` + `toBeNull` to assert an injected tag
  rendered as characters.
- A `*_does_not_panic` test on a real path. `NullServiceLifecycle` and
  `NullInput` are selected at runtime on a machine without systemd.
- A seam test pinning a client constant against an external contract.

A change that lowers the caught-rate is not a cleanup.

## What the hook refuses

Over web test files, judged on the content the tool call would produce:

1. **A test that never binds the primary export of the module it is named for.**
   A test beside `foo.ts` binds `foo`'s own export (`foo`, or the camel-case of
   a hyphenated name), by name or through a namespace import. A module with no
   export of that name is out of scope.
2. **A global or prototype reassignment with no restore**, where a restore is an
   assignment to the same target inside a `finally`, an `afterEach` or an
   `afterAll`.

Everything else above is applied by the reviewer, not matched by a script.

Exemptions live in `ALLOWLIST_PRIMARY_EXPORT` in
[`test-value-check.sh`](../../scripts/test-value-check.sh), each with a comment
naming the defect it covers. The sweep fails on an entry whose file now passes.

## Proving the suite still fails when the code is broken

Each of these must fail a named test; one that stays green is a gap to close:

- stop the byte-unit ladder at GB, and a 3 TB fileserver reads "1024 GB";
- bind disk *free* where disk *total* belongs;
- drop a repository query's tenant clause;
- return 200 with an empty body where a log pull should refuse with 403;
- stop incrementing an enrolment token's use count;
- invert the maintenance-window check.
