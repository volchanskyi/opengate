# Test Value — A Test Asserts On The Code That Ships

**Enforced by:**
[`.claude/hooks/pretooluse-test-value-guard.sh`](../hooks/pretooluse-test-value-guard.sh)
(write time) and
[`scripts/tests/test-value.test.sh`](../../scripts/tests/test-value.test.sh)
(gauntlet shell-tests step), both over the single analyser
[`scripts/test-value-check.sh`](../../scripts/test-value-check.sh).
**No bypass.**

Companion to [`tests-determinism.md`](tests-determinism.md), which says a test
must always run. This says what it must run *against*.

## What it cost

`web/src/lib/api.test.ts` carried the browser's authentication middleware
copied verbatim out of `api.ts`, twice, and asserted on the copy. It imported
one unrelated constant from the real module and never the client itself.
Deleting `api.use(authMiddleware)` from production left both tests green, as
did renaming the header or dropping the `Bearer` prefix. Every technician's
browser could have stopped sending its credential with nothing anywhere going
red.

Nothing else could see it either. The file reported two passing tests; coverage
counted the copy's lines as covered because they executed; the breakage report
showed `api.ts` carrying ten breakable points and `api.test.ts` reaching none
of them, which is a number nobody reads per-file.

## The rule

### A test exercises the shipped module, never a copy of it

Copying production code into a test file and asserting on the copy produces a
test that passes for as long as the copy is correct, which is forever — the
copy is never edited again. The shipped code beside it can be deleted outright
and nothing goes red.

### A test must not drive production code

When a module is awkward to test, the answer is a test approach that works
against the module as written — not an export, a factory or a seam that exists
only so a test can reach it. The fix for `api.test.ts` mocks `openapi-fetch`,
a genuine third-party boundary, captures what `api.ts` registers at module
load, and invokes the **real** middleware; production is unchanged.

The one sanctioned seam in this repository is the fault-injection substitution
point, which is supplied at test time rather than compiled into the shipped
binary ([`ci-cd-determinism.md`](ci-cd-determinism.md)).

### A test leaves the environment as it found it

A global or prototype reassignment with no restore makes a test's verdict
depend on what ran before it and changes the verdict of everything that runs
after. Put the assignment behind a `try`/`finally` or an `afterEach` that puts
the original back. The layout override in
`web/src/features/devices/DeviceList.test.tsx` is the reference shape: it
captures `Element.prototype.getBoundingClientRect`, replaces it inside a
`try`, restores it in the `finally`, and its comment names the two mutants it
exists to kill.

### A test asserts behaviour the product has

Two classes of test cannot fail and are deleted on sight rather than
maintained:

- **A test of the test.** `const msg: ControlMessage = { type: 'RelayReady' };
  expect(msg.type).toBe('RelayReady')` builds a literal and asserts the field
  it just set. TypeScript types erase at compile time, so no production code
  runs at all. The same goes for a store's "initial state" test asserting a
  literal equals itself, and for a compile-time trait check
  (`fn assert_impl<T: ControlMessageHandler>()`) whose body can never fail at
  runtime because non-compliance would not compile.
- **A test of behaviour that does not exist.** `FileHandler::handle_upload` is
  a documented no-op; a test asserting what it returns pins the absence of a
  feature.

Testing a third-party library is the same defect wearing different clothes —
asserting that xterm's stylesheet imports is a test of xterm.

## What is deliberately **not** banned, and why

The tempting rule is to grade tests by the shape of their assertions and delete
the weak-looking ones. **The measurement says that rule deletes working tests.**
Grading every web test file by assertion form against the nightly breakage
report inverted the expected correlation:

| Test-file weakness by assertion shape | Mean unnoticed-breakage rate |
|---|---|
| ≥30% "weak" assertions | 5.8% |
| <10% "weak" assertions | 9.3% |

`use-visible-interval.ts` is 100% "weak" by shape and catches 22 of 24
breakages. `format-bytes.ts`, the exemplar with no weak assertions at all,
misses 13.3%.

So none of the following is refused, and a cleanup that removes them is a
regression, not a tidy-up:

- **A "presence-only" assertion whose query pins a literal string or an
  accessible name.** `getByText('Permissions')` throws when the text is absent,
  so the query *is* the assertion.
- **A styling assertion where colour is the product signal.** The maintenance
  badge's text is the constant "Maintenance"; its colour is the only thing that
  says how long a machine has been left in the window. Same for rollout tone,
  the highlighted drop zone during a device drag, and which log facet is
  selected.
- **A page-structure walk.** `closest('li')` scopes an assertion to one row and
  `container.querySelector('script')` + `toBeNull` is the only way to assert
  that a technician's comment containing `<img src=x onerror=…>` rendered as
  characters — an element with no accessible role cannot be asserted absent
  through a role query. Rewriting these to role queries trades a deterministic
  lookup for one that throws on ambiguity as the DOM grows.
- **A `*_does_not_panic` test on a real path.** `NullServiceLifecycle` and
  `NullInput` are selected at runtime on a machine without systemd, so a
  technician clicking remote desktop on a container-hosted machine is exercising
  shipped behaviour.
- **A seam test pinning a client constant against an external contract** —
  `metrics/vocabulary_test.go`, `incident-lifecycle.test.ts`, the health and
  maintenance metadata maps. These touch no branch and catch nothing in a
  mutation run, and they are the only thing standing between a renamed
  server-side status and a blank screen.

**Do not trade detection for tidiness.** A change that lowers the caught-rate is
not a cleanup, whatever it does to the line count.

## What the hook refuses

Only what the evidence supports, over web test files, judged on the content the
tool call would produce:

1. **A test that never binds the primary export of the module it is named
   for** — the `api.test.ts` defect. A test beside `foo.ts` must bind `foo`'s
   own export (`foo`, or the camel-case of a hyphenated name), by name or
   through a namespace import. A module with no export of that name has no
   primary export and the check does not apply.
2. **A global or prototype reassignment with no restore**, where a restore is
   an assignment to the same target inside a `finally`, an `afterEach` or an
   `afterAll`.

Everything else above is prose the reviewer applies, not a pattern a script
matches — deliberately, because §"What is deliberately not banned" is the part
that the data supports and a matcher cannot judge.

Exemptions live in `ALLOWLIST_PRIMARY_EXPORT` in
[`scripts/test-value-check.sh`](../../scripts/test-value-check.sh), each with a
comment naming the defect it covers. An exemption is re-earned, not kept: the
sweep fails on an entry whose file now passes, so it cannot outlive its reason.

## Proving the suite still fails when the code is broken

A caught-rate is a number the suite reports about itself. The check that it
means anything is to break the product and watch the suite go red:

- stop the byte-unit ladder at GB, and a 3 TB fileserver reads "1024 GB";
- bind disk *free* where disk *total* belongs, and a full disk reads as empty;
- drop a repository query's tenant clause, and one customer's machines appear
  under another;
- return 200 with an empty body where a log pull should refuse with 403;
- stop incrementing an enrolment token's use count, and an exhausted token keeps
  enrolling machines;
- invert the maintenance-window check, and alerts fire through a customer's
  approved patch window.

Each of those must fail a named test. One that stays green is a gap to close
before anything nearby is deleted.
