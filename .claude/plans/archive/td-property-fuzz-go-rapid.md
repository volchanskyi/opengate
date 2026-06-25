# Micro-Plan: Go Property-Based Testing (`rapid`)

**Parent:** `td-property-fuzz-testing-expansion.md` (track 1 of 3). **Register:**
[techdebt.md](../../techdebt.md) — "Test-technique gaps". **Branch:** `dev`. **Owner:** Go.

## 1. Goal

Add `pgregory.net/rapid` property tests to the highest-defect-density server surfaces
(parsers/boundary math), complementing the existing protocol fuzz
([codec_fuzz_test.go](../../../server/internal/protocol/codec_fuzz_test.go)). Module:
`github.com/volchanskyi/opengate/server`.

## 2. Scope (prioritised — start at the top)

1. **APF/AMT parsers** — byte-level decoders (highest defect density).
2. **Converters** — model↔wire/API conversions (round-trip identity).
3. **Pagination math** — offset/limit/total never produce out-of-range or negative.
4. **Relay framing** — frame split/join invariants.

**Out:** rewriting existing table tests; non-parsing business logic.

## 3. File inventory

| File | Change |
|---|---|
| `server/go.mod` / `go.sum` | add `pgregory.net/rapid`. |
| `server/internal/<pkg>/<surface>_property_test.go` | **New** per prioritised surface (e.g. APF/AMT parser package, converters, pagination). |

## 4. Determinism (mandatory — [tests-determinism.md](../../rules/tests-determinism.md))

- Use `rapid.Check(t, …)` inside normal `Test*` funcs → always run under `go test` (no
  build tag, no skip).
- Pin iteration count and seed for reproducibility (rapid's `-rapid.checks` /
  `-rapid.seed`); a discovered counterexample is added as a **table-test fixture** so it
  re-runs deterministically even outside rapid.

## 5. Approach (TDD)

1. Pick the APF/AMT parser first. Write a property: `decode(encode(x)) == x` for valid
   inputs, and `decode(arbitrary bytes)` never panics / returns a typed error.
2. If the property finds a real defect, **fix the parser** (don't weaken the property).
3. Add converter round-trip + pagination-bounds properties.
4. `cd server && go test ./...` green; capture any counterexample as a fixture.
5. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## 6. Acceptance criteria / DoD

- [ ] ≥1 `rapid` property per prioritised surface that actually exercises it (asserts a
      real invariant, not a tautology).
- [ ] Tests run under plain `go test` deterministically (bounded checks + pinned seed);
      no skip/build-tag gating.
- [ ] Any defect surfaced is fixed; any counterexample committed as a fixture.
- [ ] `/precommit` (incl. coverage + mutation guards) green.

## 7. NFRs

- **Security/robustness:** parser hardening against malformed input.
- **Maintainability:** properties document invariants; seeds make failures reproducible.
- **Performance:** bounded checks keep the gauntlet fast.

## 8. Reviewer/QA checklist

- [ ] No unbounded/time-based property runs; checks + seed pinned.
- [ ] Round-trip/bounds properties assert real invariants.
- [ ] Counterexamples committed as fixtures.
- [ ] Ship per surface if needed; keep PRs small.
