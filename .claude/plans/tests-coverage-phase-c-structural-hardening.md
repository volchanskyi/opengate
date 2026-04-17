# Test Coverage — Phase C: Structural Hardening

## Context

Phases [A](tests-coverage-phase-a-targeted-gaps.md) and [B](tests-coverage-phase-b-coverage-depth.md) close specific gaps in the existing test architecture. Phase C changes the test architecture itself. The items below address risks that will compound over time if left alone:

- **Golden file asymmetry** — CI verifies Rust → Go drift but not Go → Rust. Listed in [`techdebt.md`](../techdebt.md) as "Golden File Tests Are One-Directional" since Phase 1.
- **Protocol versioning** — no mechanism marks goldens as version-pinned. A future protocol bump is all-or-nothing.
- **Single-browser E2E** — [`playwright.config.ts`](../../web/playwright.config.ts) runs Chromium only. Firefox and WebKit bugs can reach production unnoticed.
- **No visual regression baseline** — CSS drift, layout breakage, and component-level UI regressions evade behavioral tests.
- **No accessibility gate** — WCAG regressions can ship without detection.
- **`time.Sleep` in async tests** — ~10 sites use fixed-delay sleeps. Latent flake risk and slow runtime.
- **No `t.Parallel()` usage** — CI integration runtime is longer than it needs to be; opting in where isolation allows cuts roughly 30–40%.

Phase C is multi-week and should compete with Phase-level priorities rather than be squeezed in. Items are independent and can be cherry-picked.

## Approach

### 10. Reverse goldens (Go → Rust)

Resolves the tech-debt item. Mirror the existing Rust → Go mechanism:

- Go: add a `GENERATE_GOLDEN` env var to [`server/internal/protocol/golden_test.go`](../../server/internal/protocol/golden_test.go); when set, each test **writes** `testdata/golden/go_*.bin` instead of verifying. Uses the existing encoder at [`server/internal/protocol/codec.go:24`](../../server/internal/protocol/codec.go#L24).
- Rust: add a new test file `agent/crates/mesh-protocol/tests/reverse_golden_test.rs` that reads `testdata/golden/go_*.bin` and decodes, asserting struct fidelity.
- Makefile: extend the `golden` target to run Go generation then Rust verification, in addition to the existing Rust generation + Go verification.
- CI: add a `reverse-golden` job mirroring the existing `golden` job — produces `go_*.bin` in the `go-unit` or a new dedicated job, uploads them, then `rust-test` (or a new `reverse-golden` job) downloads and decodes with `git diff --exit-code`.

Retire the tech-debt entry on completion.

### 11. Protocol version field + golden metadata sidecars

- Add `protocol_version: u8` to the handshake message (Rust enum + Go struct). Default = current version.
- For each golden `.bin`, create a `.meta.json` sidecar describing: `{ "variant": "...", "protocol_version": 0, "created": "..." }`. Commit both.
- On generation, the sidecar is regenerated alongside the `.bin`. Verification asserts sidecar matches expected variant/version.
- When the protocol bumps (e.g. to v1), goldens for v0 remain in place as `v0_*.bin` alongside new `v1_*.bin`, and both are verified. Enables incremental protocol migration rather than forklift rewrites.

This work lays groundwork for future backward-compatibility testing.

### 12. Playwright: Firefox + WebKit on a separate schedule

Today [`playwright.config.ts`](../../web/playwright.config.ts) registers only Chromium. Adding Firefox + WebKit to the same config would inflate PR CI time significantly.

Split the config:
- Keep Chromium as the PR-blocking project (no change to timing).
- Add Firefox + WebKit projects, gated behind `process.env.PLAYWRIGHT_ALL_BROWSERS === '1'`.
- Add a new CI job `e2e-cross-browser` scheduled on `workflow_dispatch` + nightly cron. Runs all three projects. Does not gate merges.
- Bump `retries: 0` → `retries: 1` (Phase C only) since WebKit occasionally flakes under Docker; the single retry keeps signal without hiding real failures.

If a regression appears in the nightly job, file an issue and quarantine the spec until fixed.

### 13. Visual regression baselines

Playwright supports snapshot testing via `await expect(page).toHaveScreenshot()`. Add baselines for ~6 key screens:
- Login
- Device list (empty + populated)
- Session view (desktop capability set)
- Admin user management
- Device logs (filtered)
- File manager (directory listing)

Commit baselines as PNG under `web/e2e/__screenshots__/`. Accept slight threshold (`maxDiffPixelRatio: 0.01`) to tolerate font rendering differences.

Baselines live per-browser — only run under Chromium initially to keep the diff set small. Drift requires an explicit `npx playwright test --update-snapshots` + PR review.

### 14. Accessibility gate

Integrate `@axe-core/playwright`. In 3–5 key specs, wrap the final state with:

```ts
const results = await new AxeBuilder({ page }).analyze();
expect(results.violations).toEqual([]);
```

Block merge on any new WCAG 2.1 Level A or AA violation. Existing violations inventory gets quarantined in a baseline file; new violations must be fixed or explicitly waived.

Target specs: login, device list, session view, admin, device logs.

### 15. Replace `time.Sleep` with `require.Eventually`

Audit the ~10 `time.Sleep` call sites flagged in the initial integration review. For each, determine the condition being waited for (audit log row written, goroutine observable, channel drained) and replace with `require.Eventually(t, fn, timeout, interval, ...)`. The testify helper polls the condition and fails with a clear message.

Pure hygiene. Doesn't change what's tested. Enables item 16.

### 16. `t.Parallel()` audit

After item 15, mark every isolation-safe Go test parallel. Candidates:
- Tests using unique per-schema Postgres isolation (already safe).
- Tests constructing their own `httptest.Server` (safe).
- Tests using the shared-pool pattern (**not** safe; leave sequential or refactor).

Add `t.Parallel()` at the top of each safe `t.Run` subtest in addition to the top-level. Measure CI runtime delta — expect 30–40% drop on `go-integration`.

## Critical Files

**New:**
- `agent/crates/mesh-protocol/tests/reverse_golden_test.rs`
- `testdata/golden/go_*.bin` (~20 files mirroring Rust-side goldens)
- `testdata/golden/*.meta.json` (sidecars for all goldens)
- `web/e2e/__screenshots__/` (PNG baselines)
- Accessibility baseline allowlist file under `web/e2e/`

**Modified:**
- [`server/internal/protocol/golden_test.go`](../../server/internal/protocol/golden_test.go) — add generation mode
- [`Makefile`](../../Makefile) — extend `golden` target
- [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) — add `reverse-golden` and `e2e-cross-browser` jobs
- [`web/playwright.config.ts`](../../web/playwright.config.ts) — add Firefox + WebKit projects, bump retries
- `agent/crates/mesh-protocol/src/control.rs` — add `protocol_version` field
- `server/internal/protocol/control.go` — add `ProtocolVersion` field
- `server/internal/api/*_test.go`, `server/tests/integration/*_test.go` — Sleep → Eventually, add `t.Parallel()`
- [`techdebt.md`](../techdebt.md) — retire "Golden File Tests Are One-Directional"

**Patterns to reuse:**
- Rust golden generator macros in [`golden_test.rs`](../../agent/crates/mesh-protocol/tests/golden_test.rs)
- Go table-driven verifier in [`golden_test.go:57–104`](../../server/internal/protocol/golden_test.go#L57-L104)
- Existing Playwright fixtures and helpers in `web/e2e/`

## Verification

1. `make golden` runs both directions (Rust→Go and Go→Rust) and both pass with `git diff --exit-code` clean.
2. Break a Go encoder field; push; confirm the new `reverse-golden` CI job fails.
3. `npx playwright test --project=firefox` passes under the nightly config.
4. `npx playwright test --project=webkit` passes under the nightly config.
5. Visual regression: run baselines; deliberately change a button color; assert snapshot diff fails.
6. Accessibility: remove an `aria-label` deliberately in a test branch; assert the `@axe-core` assertion fails.
7. `go test -race -count=10 ./server/...` passes after `time.Sleep` replacement — no flakes across 10 runs.
8. CI integration-job runtime drops measurably after `t.Parallel()` audit — record the before/after numbers in the PR description.
9. `techdebt.md` entry "Golden File Tests Are One-Directional" removed.
10. A new ADR added in [`docs/adr/`](../../docs/adr/) documenting the reverse-golden mechanism and the protocol-version sidecar convention.

## Done-When

- CI enforces byte-match symmetrically in both directions.
- Every golden has a sidecar declaring its protocol version.
- Nightly cross-browser job exists, runs green against Firefox + WebKit + Chromium.
- Visual regression baselines committed and enforced under Chromium.
- Accessibility gate in place for 3–5 key specs.
- Zero `time.Sleep` remaining in `server/internal/api/` and `server/tests/integration/`.
- `go-integration` CI runtime demonstrably lower with `-parallel` opt-ins.
- [`techdebt.md`](../techdebt.md) and [`phases.md`](../phases.md) updated.
