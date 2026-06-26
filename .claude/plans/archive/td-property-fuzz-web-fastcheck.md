# Micro-Plan: Web Property-Based Testing (`fast-check`)

**Parent:** `td-property-fuzz-testing-expansion.md` (track 3 of 3). **Register:**
[techdebt.md](../../techdebt.md) — "Test-technique gaps". **Branch:** `dev`. **Owner:** web.

## 1. Goal

Add `fast-check` property tests (run under vitest) to the highest-value web surfaces:
form validation, Zustand reducers, and API-response handling — complementing the
existing example-based store tests
([connection-store.test.ts](../../../web/src/features/session/state/connection-store.test.ts),
[auth-store.test.ts](../../../web/src/state/auth-store.test.ts), …).

## 2. Scope (prioritised)

1. **Input validation** — form/field validators never crash and reject malformed input
   deterministically.
2. **Zustand reducers** — store actions preserve invariants over arbitrary action
   sequences (no impossible state).
3. **API-response handling** — parsers tolerate arbitrary/partial JSON shapes (no
   uncaught throw; typed fallback).

**Out:** component render fuzzing; visual/E2E.

## 3. File inventory

| File | Change |
|---|---|
| `web/package.json` / `package-lock.json` | add `fast-check` (devDependency). |
| `web/src/**/<surface>.property.test.ts` | **New** per prioritised surface; co-locate with the existing `*.test.ts` (validators near their module; reducers near each store). |

## 4. Determinism (mandatory — [tests-determinism.md](../../rules/tests-determinism.md))

- `fc.assert(fc.property(...), { numRuns: <fixed>, seed: <fixed> })` — pinned runs +
  seed so failures reproduce; no `.skip`/`.only`.
- A discovered counterexample is added as an explicit example-based case so it re-runs
  even without fast-check.

## 5. Approach (TDD)

1. Start with input validation: property = "validator never throws and accepts iff the
   value matches the schema" over arbitrary strings/objects.
2. Add a reducer property: arbitrary action sequence ⇒ store invariant holds (e.g. no
   negative counts, selected-id always in the list or null).
3. Add an API-response property: arbitrary/partial payload ⇒ handler returns a typed
   result or a controlled error (never an uncaught throw).
4. Fix any real defect surfaced (don't weaken the property); `npm test` green.
5. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## 6. Acceptance criteria / DoD

- [ ] ≥1 `fast-check` suite per prioritised surface, each asserting a real invariant.
- [ ] Runs under vitest deterministically (pinned `numRuns` + `seed`); no `.only`/`.skip`.
- [ ] Any defect surfaced is fixed; counterexamples kept as explicit cases.
- [ ] `npm run lint` + `npm test` + `/precommit` green; no `any` introduced (strict mode).

## 7. NFRs

- **Security/robustness:** validators + response handlers hardened against malformed
  input (XSS/`any`-bypass adjacency).
- **Maintainability:** properties document store/validator invariants.
- **Performance:** bounded `numRuns` keeps vitest fast.

## 8. Reviewer/QA checklist

- [ ] `numRuns` + `seed` pinned; no focus/skip markers.
- [ ] Reducer property covers action *sequences*, not a single action.
- [ ] No `any` added to satisfy a property (strict-mode rule).
- [ ] Ship per surface; keep PRs small.
