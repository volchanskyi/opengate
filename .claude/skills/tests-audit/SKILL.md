---
name: tests-audit
description: |
  Audit test coverage across integration tests, end-to-end tests, and cross-language
  golden-file tests. Identifies real gaps that existing CI does not catch, not duplicates
  of already-enforced checks. Reports findings with file:line references and proposes
  concrete fixes. Does NOT implement fixes — produces a plan for review.
---

# Test Coverage Audit

Audit the test suite to find coverage weaknesses that the **existing CI pipeline will not catch**. The goal is to discover real gaps, not re-enumerate checks already enforced. Every finding must be verified against the current CI configuration before being reported.

**Audit scope:** Go integration tests (`server/tests/integration/` + handler-level `*_test.go`), Playwright E2E tests (`web/e2e/`), and cross-language golden-file tests (`testdata/golden/` + Rust/Go test harnesses).

**Out of scope:** unit-level test quality, test-infra refactors unrelated to coverage, general code review.

**Severity:** CRITICAL (product flow with zero coverage + no CI gate), HIGH (regression can silently pass CI), MEDIUM (defense in depth or flakiness), LOW (hygiene / future-proofing).

---

## Section 0 — Prerequisites (MANDATORY)

These four reads are non-negotiable. Skipping them is the root cause of confident-but-wrong audit findings.

### 0a. Read the CI workflow first

**Before looking at a single test file**, read [`.github/workflows/ci.yml`](../../../.github/workflows/ci.yml) end-to-end. Build a mental map of what each job enforces and which jobs gate `merge-to-main`.

```bash
# Enumerate every job + its gate condition
grep -nE '^  [a-z-]+:$|needs:|if:' .github/workflows/ci.yml
```

Record at least:
- Which coverage thresholds exist and at what % (Rust, Go, Web).
- What the coverage-exclusion regex is at each threshold gate.
- Which jobs are listed in `merge-to-main.needs` (those are the actual merge gates).
- Whether golden files are regenerated in CI and whether drift is caught via `git diff --exit-code`.
- Whether SonarCloud runs with `sonar.qualitygate.wait=true`.

Any finding that duplicates one of these gates is a false positive. Discard it before writing the report.

### 0b. Inventory all test directories, not just `*_test.go` next to source files

Test suites in this repo live in multiple places. **Do not assume integration tests share directories with unit tests.**

```bash
# Find every directory containing test files
find . -type d -name tests -o -type d -name __tests__ -o -type d -name integration 2>/dev/null | grep -v node_modules | grep -v target
find server -type f -name '*_test.go' | xargs -I{} dirname {} | sort -u
find web -type f \( -name '*.spec.ts' -o -name '*.test.ts' -o -name '*.test.tsx' \) | xargs -I{} dirname {} | sort -u
find agent -type f -name '*.rs' -path '*/tests/*' | xargs -I{} dirname {} | sort -u
```

Expected directories in OpenGate (verify):
- `server/internal/*/` — unit + handler tests
- `server/tests/integration/` — cross-package integration tests (separate binary)
- `web/src/**/*.test.tsx` — Vitest unit/component
- `web/tests/integration/` — Vitest integration
- `web/e2e/` — Playwright E2E
- `agent/crates/*/tests/` — Rust integration tests
- `testdata/golden/` — shared cross-language golden binaries

Missing any of these from your inventory = the audit is incomplete.

### 0c. Read project state files

```bash
cat .claude/phases.md .claude/techdebt.md .claude/decisions.md
```

- `phases.md` lists every shipped feature — use it as the E2E coverage baseline.
- `techdebt.md` lists known gaps — do not re-report these as discoveries; frame them as "still open" or "resolved by this audit".
- `decisions.md` lists ADRs; some tests encode architectural contracts (e.g. ADR-002 mandates Rust→Go golden verification).

### 0d. Never accept a negative claim without a grep

Agent subagents will produce confident reports like "`X.go` has no test." **Verify every negative claim** by grepping for the feature keyword across all test files, not just by looking for a matching test filename.

```bash
# Example: verifying "handlers_install.go has no tests"
grep -rn 'install\.sh\|InstallScript\|GetInstallScript' server/internal/api/ --include='*_test.go'
```

A test for `handlers_install.go` may live inside `handlers_enrollment_test.go` or anywhere else. **Filename ≠ coverage.**

---

## Section 1 — Go Integration Test Audit

### 1a. Inventory integration tests

List every file under `server/tests/integration/` and every handler `*_test.go` under `server/internal/api/`. For each, record the test function names:

```bash
grep -rn '^func Test\w\+' server/tests/integration/ server/internal/api/ --include='*_test.go'
```

Group findings by production package under test: auth, devices, groups, users, sessions, agent API (QUIC), relay (WS), MPS, updates, notifications, device logs, hardware, restart.

### 1b. Cross-reference with CI-enforced coverage excludes

Read the coverage exclusion regex in `go-unit` job:

```bash
grep -n 'coverage-prod\.out\|grep -v -E' .github/workflows/ci.yml
```

Current exclusions typically include: `testutil`, `metrics`, `mps/wsman`, `openapi_gen`. **Files in the exclusion regex can silently regress**. For each excluded path, check whether integration tests exist:

```bash
# Example
grep -l 'mps/wsman\|ClientWsman\|DigestAuth' server/tests/integration/*.go
```

If an excluded path has no integration coverage either, flag it **HIGH** — regression is invisible on both gates.

### 1c. Negative-path coverage

For each HTTP handler, verify at least the following negative paths have tests:
- 401 (missing / malformed JWT)
- 403 (authenticated but not authorized — wrong owner, non-admin)
- 404 (resource does not exist)
- 409 (conflict — duplicate email, last admin, etc.)
- 400 (malformed body, invalid UUID, out-of-range pagination)

```bash
# Find handlers that never get a non-2xx assertion in their test file
for t in server/internal/api/*_test.go; do
  if ! grep -qE 'StatusUnauthorized|StatusForbidden|StatusNotFound|StatusConflict|StatusBadRequest' "$t"; then
    echo "GAP: $t has no negative-path assertions"
  fi
done
```

### 1d. Concurrency and fault injection

Control streams to agents (restart, hardware, device-logs) share a QUIC stream per agent. Check for concurrent-request tests:

```bash
grep -rn 'Concurrent\|go func\|errgroup\|sync\.WaitGroup' server/tests/integration/ --include='*_test.go'
```

If no concurrent test exists for a shared-stream handler, flag **MEDIUM**. If `-race` is not enabled in the CI integration job, flag **HIGH** (check [`ci.yml:224`](../../../.github/workflows/ci.yml#L224)).

### 1e. Postgres-specific coverage (post-Phase 13a)

After the SQLite → PostgreSQL 17 cutover, verify driver-specific behavior has targeted tests:

```bash
grep -rn 'TIMESTAMPTZ\|JSONB\|UUID' server/ --include='*_test.go' | grep -v '/db/migrations/'
```

Missing tests for `TIMESTAMPTZ` timezone handling, `JSONB` round-trip on `device_hardware.network_interfaces`, or `UUID` string validation → **MEDIUM**. These regressions survive a `pgx/v5` upgrade silently.

### 1f. Flakiness smells

```bash
# time.Sleep in tests = latent flake
grep -rn 'time\.Sleep' server/ --include='*_test.go'

# Hardcoded ports
grep -rn '127\.0\.0\.1:[0-9]' server/ --include='*_test.go'

# No t.Parallel usage
grep -rn 't\.Parallel' server/ --include='*_test.go' | wc -l
```

More than 5 `time.Sleep` call sites or zero `t.Parallel()` in a 50+ file suite → **LOW** / hygiene.

---

## Section 2 — Playwright E2E Audit

### 2a. Inventory E2E specs

```bash
ls web/e2e/*.spec.ts
grep -c '^test(' web/e2e/*.spec.ts
```

Count total tests and files. Compare with [`phases.md`](../../../.claude/phases.md) claims.

### 2b. Feature-to-spec coverage matrix

For every user-visible feature listed in `phases.md` under "Completed", confirm an E2E spec exists that exercises it end-to-end (not just API-level). Build the matrix:

| Feature | Spec file | Exercises UI? |
|---------|-----------|---------------|
| Auth (register/login/logout) | `auth.spec.ts` | Yes |
| Admin dashboard | `admin.spec.ts` | Yes |
| Device list | `device-list.spec.ts` | Yes |
| Session + terminal | — | **NO** |
| File Manager | — | **NO** |
| Device Logs UI | — | **NO** |
| Chat / MessengerView | — | **NO** |
| Agent Restart button | — | **NO** |
| Hardware Inventory | — | **NO** |
| Capability-based tabs | — | **NO** |
| Web Push subscribe + delivery | — | **NO** |

Each "NO" on a shipped feature is **HIGH** or **CRITICAL** depending on user impact. Core product loop (session + terminal) with no E2E = **CRITICAL**.

### 2c. Playwright config audit

```bash
cat web/playwright.config.ts
```

Verify:
- **Projects** — at minimum Chromium; ideally Firefox + WebKit on a nightly schedule (not blocking PR CI).
- **`retries`** — 0 is strict; 1 acceptable if flake pressure is real; ≥2 hides regressions.
- **`timeout`** — 30_000 is aggressive for Docker-backed flows; 60_000 is safer.
- **`serviceWorkers`** — should be `"block"` to avoid SW cache interference unless testing push.
- **`webServer`** — must wait on a health endpoint, not a fixed delay.
- **Reporter** — HTML + list is standard; JSON reporter useful for CI scraping.

### 2d. Test-quality smells

```bash
# Hard sleeps — anti-pattern
grep -n 'waitForTimeout' web/e2e/

# Brittle selectors
grep -nE 'querySelector|\\.css\\(' web/e2e/

# No assertion (navigate-only tests)
for f in web/e2e/*.spec.ts; do
  tests=$(grep -c '^test(' "$f")
  asserts=$(grep -cE 'expect\\(' "$f")
  [ "$asserts" -lt "$tests" ] && echo "GAP: $f has $tests tests but only $asserts expect() calls"
done
```

Any `waitForTimeout` = **LOW**. Brittle selectors = **LOW**. Navigate-only tests (no assertions) = **MEDIUM**.

### 2e. Error-path E2E

Verify at least one spec covers each of:
- Expired JWT → redirect to login
- Offline agent → session create returns sensible UI error
- Server 5xx → error boundary renders, does not crash
- Permission denied → user-facing message, no stack trace leak

### 2f. Accessibility and visual regression

- **A11y:** grep for `@axe-core/playwright`. Absent → **MEDIUM** (no regression detection).
- **Visual:** grep for `toHaveScreenshot`. Absent → **LOW** (feature-optional).
- **Keyboard navigation:** grep for `keyboard.press\\|Tab\\|Enter`. Sparse usage → **LOW**.

---

## Section 3 — Golden-File Cross-Language Audit

### 3a. CI wiring check

**Do not assume goldens are unverified in CI.** Check every step:

```bash
# Is there a dedicated golden job?
grep -n 'golden' .github/workflows/ci.yml

# Is git diff used to catch drift?
grep -n 'git diff --exit-code.*golden' .github/workflows/ci.yml

# Is the golden job required for merge?
grep -A 50 'merge-to-main:' .github/workflows/ci.yml | grep -E '^\s*-\s+golden'
```

If all three are present, Rust→Go drift is already caught. Do **not** report "CI misses golden drift".

### 3b. Variant coverage matrix (both directions)

List every Rust `ControlMessage` variant:

```bash
grep -n '^\s\{4\}[A-Z][a-zA-Z]*\s*\{' agent/crates/mesh-protocol/src/control.rs | head -50
```

List every Go `ControlMessageType`:

```bash
grep -nE '^\s+[A-Z][a-zA-Z]+\s+ControlMessageType\s*=' server/internal/protocol/control.go
```

Compute the intersection — those are the variants that **cross the language boundary** and need goldens. (Rust-only variants stay in-process via WebRTC data channels and don't need Go goldens. Go-only variants don't exist.)

For every intersection variant without a golden in `testdata/golden/control_*.bin`, flag **HIGH** — it can drift without CI detection.

### 3c. Reverse-direction gap

```bash
# Does a Go→Rust golden harness exist?
ls testdata/golden/go_*.bin 2>/dev/null
grep -rn 'read_golden_file\|DecodeControl.*go_' agent/crates/mesh-protocol/tests/
```

If both queries return nothing, reverse verification is missing. Cross-reference with [`techdebt.md`](../../../.claude/techdebt.md): this is the long-standing "Golden File Tests Are One-Directional" item. Flag **HIGH** (not new — reference the existing tech-debt entry).

### 3d. Edge-case corpus

Verify at least one golden exists for each edge class:
- Empty / default optional field absent
- Near-maximum payload (close to the frame length limit)
- Multi-byte UTF-8 in string fields
- Unknown-extra-key forward compatibility
- Malformed framing (length header mismatch, little-endian header, truncated)

```bash
ls testdata/golden/ | wc -l
# If the count is close to the number of variants, there's no edge-case corpus.
```

Missing edge-case corpus → **MEDIUM**.

### 3e. Assertion depth

Sample a few Go verifiers — do they decode into the expected struct and compare **every field**, or do they only assert `err == nil`?

```bash
grep -A 20 'TestGolden' server/internal/protocol/golden_test.go | grep -E 'assert\.Equal|require\.Equal' | head -20
```

If tests only check for decode success without field-level assertions, a silent semantic regression (e.g. a field being dropped) passes. Flag **HIGH**.

### 3f. Protocol version metadata

```bash
grep -rn 'protocol_version\|ProtocolVersion' agent/crates/mesh-protocol/src/ server/internal/protocol/
```

If absent, future protocol bumps cannot be tracked per-golden. Flag **LOW** (relevant when a bump is planned).

---

## Section 4 — Report Format

Produce a report with these sections in order:

### 4.1 Findings Summary

Table, one row per finding:

```
+-----+----------+-------------------------------------------------+----------+-------------+
| #   | Severity | Finding                                         | Section  | CI-caught?  |
+-----+----------+-------------------------------------------------+----------+-------------+
| 1   | CRITICAL | No E2E for session + terminal                   | 2b       | No          |
| 2   | HIGH     | `mps/wsman` excluded from coverage and Sonar,   | 1b       | No          |
|     |          | no integration test either                      |          |             |
| 3   | HIGH     | 8 cross-boundary ControlMessage variants lack   | 3b       | No          |
|     |          | goldens                                         |          |             |
+-----+----------+-------------------------------------------------+----------+-------------+
```

The "CI-caught?" column is the most important. If a finding is caught by CI, it should not appear here unless the severity concerns depth, not existence.

### 4.2 For each finding

- File path (for source gaps, `server/internal/api/handlers_foo.go`; for test gaps, the nearest existing test file plus what's missing).
- CWE or convention reference where applicable.
- Evidence: the grep result that confirms the gap.
- Proposed fix at a level of concrete detail: "add a golden file at `testdata/golden/control_session_accept.bin`, generator in `golden_test.rs` line 120 area, verifier case in `golden_test.go` table at line 57".
- Cross-reference to existing tech debt if the gap is already tracked.

### 4.3 Out of audit scope

Explicitly list: what was checked and confirmed covered, to avoid a reader re-running the audit on passing areas.

### 4.4 Recommended plan

Produce a plan file under `.claude/plans/` following the project convention. Break into phases by effort (A: ~1–2 days, B: ~4–6 days, C: multi-week). Each phase must stand on its own and have explicit done-when criteria.

### 4.5 Gate criteria

The audit **does not** fail CI. It produces a plan for the user to approve or redirect. Never implement fixes in this skill — implementation is a follow-up step that the user explicitly authorizes.

---

## Section 5 — Common Pitfalls (Lessons from Prior Audits)

These are documented root causes of incorrect findings. Review every finding against this list before publishing.

1. **Missing the integration-test directory.** Assume `server/tests/integration/` exists and is populated. Search for it explicitly. Integration tests in OpenGate do **not** live next to source files.

2. **Skipping `.github/workflows/ci.yml`.** The workflow defines enforcement. Any finding that claims "CI doesn't check X" without grepping the workflow is likely wrong. Read the workflow first.

3. **Trusting subagent negative claims.** Explore agents produce confident reports like "`handlers_install.go` has zero tests." Verify by grepping the feature keyword across all test files, not the expected test filename. `TestGetInstallScript` lives in `handlers_enrollment_test.go`, not `handlers_install_test.go`.

4. **Conflating `make ci` with CI.** `make ci` is a local dev convenience. The real CI pipeline invokes specific targets (`make test`, `go test ./tests/integration`, etc.) independently. Do not conclude "CI doesn't run X" from `make ci`'s target list.

5. **Counting Rust-only variants as Go goldens gaps.** Some `ControlMessage` variants (`MouseMove`, `KeyPress`, etc.) only flow agent↔browser over WebRTC data channels and never cross Go. Compute the Rust ∩ Go intersection before reporting missing goldens.

6. **Double-counting known tech debt.** If a gap is already in [`techdebt.md`](../../../.claude/techdebt.md), frame it as "still open" — not as a new discovery. Reference the existing entry.

7. **Recommending fixes already implemented.** Before recommending "add CI job X", grep the workflow for it. The golden-verification job already exists.

---

## Section 6 — Audit Hygiene

- The audit must take < 30 minutes of clock time and produce < 3 subagent invocations. If it's running longer, the scope is wrong — narrow to one test category and iterate.
- Subagents must be briefed with: specific directories to inventory, CI jobs to reference, and an explicit instruction to verify every negative claim before including it.
- Every claim in the final report must cite a file path and either a line number or a grep invocation that reproduces it.
- Do not paraphrase numbers. If a suite has 22 tests, say 22, not "about 20". Grep-verify before quoting.
- When in doubt, read the file. The cost of a 5-second `Read` call is less than the cost of a wrong finding.
