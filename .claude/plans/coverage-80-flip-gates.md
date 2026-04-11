# Close Go/Web Coverage to 80% and Flip Quality Gates

## Context

SonarCloud quality gates are wired into `merge-to-main` but currently soft-fail. Go CI threshold is 70%, Web has no CI threshold. Rust is already at 80% hard-fail — no work needed there.

Actual coverage: Go ~78.4% (1.6pp gap), Web ~80.1% (already there but fragile).

**User decisions:**
- Write margin tests now for buffer
- Real tests only (no CI exclusion alignment) — earn coverage honestly
- 80% threshold for both Go and Web
- Add 80% coverage enforcement to `/precommit` skill

---

## Phase 1: Go Coverage to 80% (tests first, then threshold bump)

### 1a. Protocol codec tests — 6 untested functions in `codec.go`

**File:** new `server/internal/protocol/codec_extra_test.go`

Table-driven round-trip tests:
- `TestEncodeDecodeTerminalFrame` — encode/decode round-trip
- `TestEncodeDecodeFileFrame` — encode/decode round-trip
- `TestEncodeHandshake` / `TestDecodeHandshakeType` — known types
- Invalid data error paths for both frame types

~18 statements covered.

### 1b. DB store tests — 4 untested functions in `sqlite.go`

**File:** modify `server/internal/db/sqlite_test.go`

- `TestDB` — call `store.DB()`, assert non-nil
- `TestListAllDevices` — seed 2 devices in different groups, assert both returned
- `TestUpdateDeviceGroup` — seed device, update group, verify
- `TestListAllWebPushSubscriptions` — seed subscriptions, assert all returned

~12 statements covered.

### 1c. API converter tests

**File:** new `server/internal/api/converters_test.go`

- `TestIceServersToAPI` — convert signaling ICE servers to API type
- `TestDerefStr_nil` / `TestDerefInt_nil` — nil pointer edge cases

~5 statements covered.

### 1d. Bump Go CI threshold

**File:** `.github/workflows/ci.yml` line 167
Change `THRESHOLD=70` to `THRESHOLD=80`

---

## Phase 2: Web Margin Tests + CI Gate (80%)

### 2a. Write margin tests to push coverage to ~83-84%

| File | Lines | Tests |
|------|-------|-------|
| `web/src/features/agent-setup/InstallInstructions.tsx` | 130 | 2-3 render tests |
| `web/src/features/profile/ProfilePage.tsx` | 63 | 2 render tests |
| `web/src/features/admin/NotificationCenter.tsx` | 66 | 2-3 tests |
| `web/src/state/toast-store.ts` | 31 | 2 tests (add/dismiss) |
| `web/src/components/Breadcrumbs.tsx` | 72 | 2 render tests |

~10-12 tests, new test files for each.

### 2b. Add coverage threshold to `web-unit` CI job

**File:** `.github/workflows/ci.yml`, after vitest run step in `web-unit` job

```yaml
- name: Enforce coverage threshold
  working-directory: web
  run: |
    LINES=$(node -e "const s=require('./coverage/coverage-summary.json');console.log(s.total.lines.pct)")
    echo "Web line coverage: $LINES%"
    THRESHOLD=80
    if [ "$(echo "$LINES < $THRESHOLD" | bc)" -eq 1 ]; then
      echo "::error::Web coverage $LINES% is below $THRESHOLD%"
      exit 1
    fi
```

---

## Phase 3: Add 80% Coverage to `/precommit` Skill

Update the precommit skill to enforce 80% coverage locally before commit/push for both Go and Web:
- Go: run `go test -coverprofile` with filtered threshold check at 80%
- Web: run `vitest --coverage` and parse `coverage-summary.json` at 80%
- Fail precommit if either language is below threshold

---

## Phase 4: Coverage Badges for All Languages

Currently only Go has a coverage badge (via `schneegans/dynamic-badges-action` → gist → shields.io endpoint). Add matching badges for Rust and Web.

### 4a. Add coverage output to `rust-test` job

**File:** `.github/workflows/ci.yml`, `rust-test` job

- Add `outputs: coverage: ${{ steps.coverage.outputs.percentage }}` to job definition
- After `cargo llvm-cov`, extract percentage and write to `$GITHUB_OUTPUT`:
  ```yaml
  - name: Extract Rust coverage
    id: coverage
    run: |
      PCT=$(cargo llvm-cov --workspace --quiet 2>&1 | grep 'TOTAL' | awk '{print $NF}' | tr -d '%')
      echo "percentage=$PCT" >> "$GITHUB_OUTPUT"
  ```

### 4b. Add coverage output to `web-unit` job

**File:** `.github/workflows/ci.yml`, `web-unit` job

- Add `outputs: coverage: ${{ steps.coverage.outputs.percentage }}` to job definition
- In the new threshold enforcement step (Phase 2b), also emit output:
  ```yaml
  echo "percentage=$LINES" >> "$GITHUB_OUTPUT"
  ```

### 4c. Add badge update steps to `merge-to-main` job

**File:** `.github/workflows/ci.yml`, after existing Go coverage badge step (line ~916)

```yaml
- name: Update Rust coverage badge
  if: github.ref == 'refs/heads/dev'
  uses: schneegans/dynamic-badges-action@v1.8.0
  with:
    auth: ${{ secrets.GIST_SECRET }}
    gistID: cf505c74b56eab52c9497af517b53222
    filename: opengate-rust-coverage.json
    label: Rust Agent Coverage
    message: ${{ needs.rust-test.outputs.coverage }}%
    valColorRange: ${{ needs.rust-test.outputs.coverage }}
    minColorRange: 75
    maxColorRange: 90

- name: Update Web coverage badge
  if: github.ref == 'refs/heads/dev'
  uses: schneegans/dynamic-badges-action@v1.8.0
  with:
    auth: ${{ secrets.GIST_SECRET }}
    gistID: cf505c74b56eab52c9497af517b53222
    filename: opengate-web-coverage.json
    label: Web Client Coverage
    message: ${{ needs.web-unit.outputs.coverage }}%
    valColorRange: ${{ needs.web-unit.outputs.coverage }}
    minColorRange: 75
    maxColorRange: 90
```

### 4d. Add badges to README.md

**File:** `README.md`, line 4 (after existing Go badge)

```markdown
[![Rust Agent Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/volchanskyi/cf505c74b56eab52c9497af517b53222/raw/opengate-rust-coverage.json)](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml)
[![Web Client Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/volchanskyi/cf505c74b56eab52c9497af517b53222/raw/opengate-web-coverage.json)](https://github.com/volchanskyi/opengate/actions/workflows/ci.yml)
```

---

## Phase 5: Verify and Harden SonarCloud Gate

### 5a. Verify quality gate in SonarCloud UI (manual)

Confirm `volchanskyi_opengate` project has the custom gate:
- New code coverage >= 80%, Overall >= 70%
- 0 new bugs/vulns/smells, A ratings, <3% duplication

### 5b. SonarCloud gate is already hard-wired

The `sonarcloud` job is already in `merge-to-main` needs list. `-Dsonar.qualitygate.wait=true` fails the job on gate failure. Just verify thresholds in UI.

### 5c. Document completion

- Update `.claude/phases.md` — mark complete
- Update `.claude/techdebt.md` if applicable

---

## Files to modify

| File | Change |
|------|--------|
| `server/internal/protocol/codec_extra_test.go` | **New** — codec round-trip tests |
| `server/internal/db/sqlite_test.go` | Add 4 store function tests |
| `server/internal/api/converters_test.go` | **New** — converter + deref tests |
| `.github/workflows/ci.yml` | Go threshold 70→80, Web threshold step, Rust/Web coverage outputs + badge steps |
| `web/src/features/agent-setup/InstallInstructions.test.tsx` | **New** — render tests |
| `web/src/features/profile/ProfilePage.test.tsx` | **New** — render tests |
| `web/src/features/admin/NotificationCenter.test.tsx` | **New** — render tests |
| `web/src/state/toast-store.test.ts` | **New** or extend — dismissal path |
| `web/src/components/Breadcrumbs.test.tsx` | **New** — render tests |
| `README.md` | Add Rust + Web coverage badges |
| Precommit skill config | Add Go/Web 80% coverage checks |

## Verification

1. `cd server && go test -race -coverprofile=coverage.out ./internal/... && go tool cover -func=coverage.out | tail -1` — verify >= 80%
2. `cd web && npx vitest run --coverage` — verify >= 80% with margin
3. `/precommit` passes (including new coverage checks)
4. Push to dev — confirm `sonarcloud` job passes, quality gate green
5. Check SonarCloud UI — overall coverage and new code coverage both meeting gates
