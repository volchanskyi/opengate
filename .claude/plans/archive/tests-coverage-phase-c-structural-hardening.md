# Test Coverage — Phase C: Structural Hardening (revised 2026-05-14)

## Context

This plan supersedes the April 16 draft after re-evaluation against the 20 commits that landed between then and 2026-05-14. Phases [A](archive/tests-coverage-phase-a-targeted-gaps.md) and [B](archive/tests-coverage-phase-b-coverage-depth.md) are now complete; the structural-testing pyramid (mutation observability, gosec, eslint security, dead-code) and CI/CD additions (Claude Hooks, Postgres test DB, terraform `tftest` + drift, IaC security pyramid) have landed in parallel. None of those have touched the Phase C surface.

Re-audit findings against the original 7 items (10–16):
- Items 10–14 (reverse goldens, protocol version sidecars, cross-browser, visual regression, accessibility) — **NOT STARTED**.
- Item 15 (`time.Sleep` → `require.Eventually`) — **PARTIAL**: 17 fixed-sleep sites remain across 8 test files; 23 `require.Eventually` sites are already migrated.
- Item 16 (`t.Parallel()`) — **NOT STARTED**: 0/140 server tests opt in.

Three new structural gaps surfaced during Phase B and the post-April rollouts. They were not visible when the April 16 draft was written:

- **No Go fuzzing.** Rust `mesh-protocol` is now mutation-hardened to 76.6% (Structural Testing PR 6), but the symmetric Go wire codec at [`server/internal/protocol/codec.go`](../../server/internal/protocol/codec.go) has zero fuzz coverage. The codec is an untrusted-input boundary.
- **CI verifies Go codegen drift** (`oapi-codegen` + `git diff --exit-code -- server/internal/api/`, [`.github/workflows/ci.yml:127–135`](../../.github/workflows/ci.yml#L127-L135)) **but has no equivalent for `web/src/types/api.d.ts`**. Schema drift between server and client can land silently — this is asymmetric with the Go side.
- **WebSocket relay has no fault-injection tests.** The equivalent control-stream fault tests landed in Phase B / B5 ([`server/tests/integration/control_stream_faults_test.go`](../../server/tests/integration/control_stream_faults_test.go)). The relay is a stateful broker between browser and agent; its failure modes (partial writes, mid-frame disconnect, full buffer) are unexercised.
- **`AgentConn.sendControl` has no write mutex** ([techdebt.md entry](../techdebt.md), identified 2026-05-14 during B5). The deferred concurrent-send test that would expose it is still skipped. Phase C closes both the production bug and the test gap together.

Phase C re-bundles into **3 clustered PRs (C1, C2, C3)**. Each PR is independently mergeable; clusters are organized by file-locality so reviewers stay in one area at a time.

**Explicitly out of scope (deferred):** Production-side clock injection (`time.Now()` → `Clock` interface). 81 call sites across `server/internal/` make this too invasive for Phase C; it will be added to [`techdebt.md`](../techdebt.md) as a new entry when this plan is approved.

---

## Approach

### C1 — Cross-language protocol safety

Three items, all touching `testdata/golden/`, `agent/crates/mesh-protocol/`, and `server/internal/protocol/`.

**(10) Reverse goldens — Go → Rust**

Resolves the "Golden File Tests Are One-Directional" tech-debt entry (open since Phase 1). Mirrors the existing Rust → Go mechanism:

- Add a `GENERATE_GOLDEN` env-var branch to [`server/internal/protocol/golden_test.go`](../../server/internal/protocol/golden_test.go) (currently read-only). When set, each test **writes** `testdata/golden/go_<variant>.bin` instead of asserting equality. Use the existing encoder at [`server/internal/protocol/codec.go`](../../server/internal/protocol/codec.go).
- New test file `agent/crates/mesh-protocol/tests/reverse_golden_test.rs` reads `testdata/golden/go_<variant>.bin` and decodes via the existing Rust decoder, asserting field equality against canonical fixtures.
- Extend the `golden` Makefile target ([`Makefile:164–166`](../../Makefile)) to run Go-generate + Rust-verify in addition to the existing Rust-generate + Go-verify.
- New CI job `reverse-golden` in [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) mirroring the existing `golden` job. Generates `go_*.bin` as an artifact, downloads in a Rust-verify step, asserts `git diff --exit-code -- testdata/golden/` is clean.

**(11) Protocol version + golden sidecars**

Lays groundwork for backward-compatibility testing without forcing an immediate version bump.

- Add `protocol_version: u8` (current value = `0`) to the Rust `AgentProof` handshake message and the Go counterpart in [`server/internal/protocol/control.go`](../../server/internal/protocol/control.go).
- For each `testdata/golden/*.bin`, create a `.meta.json` sidecar: `{ "variant": "...", "protocol_version": 0, "format": "msgpack", "created": "2026-05-14" }`. Both the Rust forward generator and the Go reverse generator (from C1-10) emit sidecars alongside the `.bin`.
- Verification asserts sidecar fields match the expected variant and version. When the protocol bumps to v1, v0 goldens remain in place as `v0_*.bin` and both versions are verified — incremental migration replaces forklift rewrites.

**(NEW) Go native fuzzing for the wire codec**

The symmetric attack surface to mutation-hardened Rust `mesh-protocol`.

- Add `FuzzReadFrame` and `FuzzDecodeControl` under `server/internal/protocol/codec_fuzz_test.go` using Go's native `testing.F`.
- Seed corpus from `testdata/golden/*.bin` — the same fixtures both sides already verify.
- Wire `go test -fuzz=FuzzReadFrame -fuzztime=30s` into [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml) as a fourth dimension on the existing nightly cadence (03:00 UTC). Failures append to `docs/mutation-history.jsonl` and trigger the existing Telegram regression alert.

### C2 — Web QA cross-cutting

Four items, all under `web/`. Adds two new CI jobs and one CI step.

**(12) Playwright Firefox + WebKit on a separate schedule**

- [`web/playwright.config.ts`](../../web/playwright.config.ts) currently registers only Chromium. Add `firefox` + `webkit` projects gated behind `process.env.PLAYWRIGHT_ALL_BROWSERS === '1'` so PR CI stays Chromium-only.
- New CI job `e2e-cross-browser` in [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) on `workflow_dispatch` + nightly `cron: '0 3 * * *'` (same slot as `mutation.yml` and `terraform-drift.yml`). Does NOT gate merges.
- Bump `retries: 0 → 1` for the cross-browser path only — WebKit occasionally flakes under Docker. Single retry keeps signal without hiding real failures.

**(13) Visual regression baselines (Chromium only)**

- 6 baseline screens via `await expect(page).toHaveScreenshot()`: Login, Device list (empty + populated), Session view (desktop capability set), Admin user management, Device logs (filtered), File manager (directory listing).
- Commit baselines as PNG under `web/e2e/__screenshots__/`. `maxDiffPixelRatio: 0.01` tolerates font-rendering differences.
- Chromium only initially (per-browser baselines explode the diff set when combined with cross-browser). Drift requires explicit `npx playwright test --update-snapshots` + PR review.

**(14) Accessibility gate**

- Add `@axe-core/playwright` to [`web/package.json`](../../web/package.json) devDeps.
- Wrap the final state in 5 specs (login, device list, session view, admin, device logs) with:
  ```ts
  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations).toEqual([]);
  ```
- Block merge on any new WCAG 2.1 A/AA violation. Existing violations inventoried in `web/e2e/a11y-baseline.json` and explicitly waived.

**(NEW) Web API codegen drift gate**

Mirrors the existing Go-side drift check at [`.github/workflows/ci.yml:127–135`](../../.github/workflows/ci.yml#L127-L135) for the web client.

- New step in the web CI job: `cd web && npm run generate:api && git diff --exit-code -- src/types/api.d.ts`. Fails CI with a clear "run `npm run generate:api` and commit" message.

### C3 — Backend test hygiene + fault injection

Four items, all under `server/`. The only production-API change is the `AgentConn` mutex (closes a tech-debt entry).

**(15) Replace `time.Sleep` with `require.Eventually`**

Audit the 17 remaining sites:
- `server/internal/api/handlers_restart_test.go` (1), `relay_handler_test.go` (3), `admin_test.go` (3), `agentapi_test.go` (2), `middleware_ws_test.go` (1), `relay_data_test.go` (1), `security_groups_test.go` (1), `session_test.go` (5).

For each: identify the condition being waited for (audit row written, goroutine observable, channel drained) and replace with `require.Eventually(t, fn, timeout, interval, ...)`. Pure hygiene — doesn't change what's tested; unblocks C3-16.

**(16) `t.Parallel()` audit**

After C3-15, mark every isolation-safe Go test parallel. Candidates:
- Tests using per-schema Postgres isolation via [`server/internal/testutil/`](../../server/internal/testutil/) (safe — Phase 13a/B4 infra).
- Tests constructing their own `httptest.Server` (safe).
- Tests using shared-pool patterns — leave sequential or refactor in a follow-up.

Add `t.Parallel()` at the top of each safe `t.Run` subtest in addition to the top-level. Measure CI runtime delta — target a 30–40% drop on `go-integration`. Record the before/after numbers in the PR description.

**(NEW) `AgentConn.sendControl` write mutex + deferred B5 case**

Closes the [tech-debt entry](../techdebt.md) identified during B5. Two-part change:

- **Production fix**: add `sync.Mutex` to `AgentConn` in [`server/internal/agentapi/conn.go`](../../server/internal/agentapi/conn.go), lock around the `WriteFrame` call in `sendControl` so concurrent (header, payload) writes cannot interleave on the same QUIC stream.
- **Test**: add the concurrent-server-initiated-sends case to [`server/tests/integration/control_stream_faults_test.go`](../../server/tests/integration/control_stream_faults_test.go), currently skipped pending the production fix. Two goroutines fire `RequestHardwareReport` + `RequestDeviceLogs` simultaneously; assert the agent receives both decoded cleanly.

Retire the tech-debt entry on completion.

**(NEW) WebSocket relay fault injection**

Extends the [B5 fault-injection pattern](../../server/tests/integration/control_stream_faults_test.go) to the relay path. The relay is a stateful broker — its failure modes are unexercised.

Add `server/tests/integration/relay_faults_test.go` with 3 specs:
- Browser closes connection mid-frame transmission → agent's relay loop reconciles cleanly (no goroutine leak).
- Agent closes connection while relay buffer non-empty → buffered bytes drop, no `nil` writes attempted post-close.
- Concurrent browser + agent writes on the same session → ordering preserved or explicitly documented as best-effort.

Reuse the `setupOnlineAgent` helper extracted in commit [`33f1f59`](../../).

---

## Critical Files

**New:**
- `agent/crates/mesh-protocol/tests/reverse_golden_test.rs` (C1)
- `testdata/golden/go_*.bin` (~17 files, one per existing Rust-side variant) (C1)
- `testdata/golden/*.meta.json` sidecars for all goldens (C1)
- `server/internal/protocol/codec_fuzz_test.go` (C1)
- `web/e2e/__screenshots__/` (PNG baselines) (C2)
- `web/e2e/a11y-baseline.json` (a11y violations allowlist) (C2)
- `server/tests/integration/relay_faults_test.go` (C3)
- New ADR under [`docs/adr/`](../../docs/adr/) — reverse-golden mechanism + sidecar convention

**Modified:**
- [`server/internal/protocol/golden_test.go`](../../server/internal/protocol/golden_test.go) — add `GENERATE_GOLDEN` write mode (C1)
- [`server/internal/protocol/control.go`](../../server/internal/protocol/control.go) + Rust counterpart in `agent/crates/mesh-protocol/src/control.rs` — add `protocol_version` field (C1)
- [`Makefile`](../../Makefile) — extend `golden` target for bidirectional run (C1)
- [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) — add `reverse-golden` job (C1), `e2e-cross-browser` job (C2), web-codegen-drift step (C2)
- [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml) — add Go fuzz dimension to the matrix (C1)
- [`web/playwright.config.ts`](../../web/playwright.config.ts) — add Firefox + WebKit projects, bump retries for cross-browser path (C2)
- [`web/package.json`](../../web/package.json) — add `@axe-core/playwright` devDep (C2)
- [`server/internal/agentapi/conn.go`](../../server/internal/agentapi/conn.go) — add `sync.Mutex` to `AgentConn.sendControl` (C3)
- 8 test files for `time.Sleep` → `require.Eventually` cleanup (C3-15)
- All isolation-safe `*_test.go` files in `server/internal/api/` and `server/tests/integration/` for `t.Parallel()` (C3-16)
- [`server/tests/integration/control_stream_faults_test.go`](../../server/tests/integration/control_stream_faults_test.go) — un-skip the concurrent-send case (C3)
- [`.claude/techdebt.md`](../techdebt.md) — retire "Golden File Tests Are One-Directional" (C1) and "AgentConn.sendControl Lacks Write Mutex" (C3); add new "Clock injection deferred" entry
- [`.claude/phases.md`](../phases.md) — add the 3 completed C-cluster phase rows

**Patterns to reuse:**
- Rust golden generator macros in [`agent/crates/mesh-protocol/tests/golden_test.rs`](../../agent/crates/mesh-protocol/tests/golden_test.rs) (C1)
- Go table-driven verifier in [`server/internal/protocol/golden_test.go`](../../server/internal/protocol/golden_test.go) (C1)
- B5 fault-injection scaffolding in [`control_stream_faults_test.go`](../../server/tests/integration/control_stream_faults_test.go) + `setupOnlineAgent` helper (C3)
- Per-schema Postgres isolation in [`server/internal/testutil/`](../../server/internal/testutil/) for safe `t.Parallel()` (C3-16)
- Existing Go drift-check pattern at [`ci.yml:127–135`](../../.github/workflows/ci.yml#L127-L135) for the web mirror (C2)
- Existing mutation.yml matrix + Telegram alert wiring for the Go fuzz dimension (C1)

---

## Verification

**C1 — Cross-language protocol safety**
1. `make golden` runs both directions (Rust→Go and Go→Rust); both pass with `git diff --exit-code -- testdata/golden/` clean.
2. Deliberately mutate a Go encoder field; push to a branch; confirm the new `reverse-golden` CI job fails red.
3. `find testdata/golden -name '*.meta.json' | wc -l` equals `find testdata/golden -name '*.bin' | wc -l`.
4. `go test -fuzz=FuzzReadFrame -fuzztime=10s ./server/internal/protocol/` runs and exits 0 after the corpus seeds successfully.

**C2 — Web QA cross-cutting**
5. `PLAYWRIGHT_ALL_BROWSERS=1 npx playwright test --project=firefox` passes locally.
6. `PLAYWRIGHT_ALL_BROWSERS=1 npx playwright test --project=webkit` passes locally.
7. Visual regression: run baselines; deliberately change a button color in `theme.ts`; assert snapshot diff fails CI.
8. Accessibility: remove an `aria-label` deliberately in a test branch; assert the `@axe-core` assertion fails.
9. Deliberately edit `api/openapi.yaml` without running `npm run generate:api`; push; confirm the new web-codegen-drift CI step fails.

**C3 — Backend test hygiene + fault injection**
10. `go test -race -count=10 ./server/...` passes after Sleep→Eventually with no new flakes across 10 runs.
11. `go-integration` job runtime in CI drops measurably after `t.Parallel()` audit. Record before/after in the PR description.
12. The un-skipped concurrent-send fault test passes (B5 deferred case): two concurrent server→agent control sends both arrive cleanly.
13. The three relay-fault specs pass under `go test -count=1 -run TestRelayFaults` with `-race`; no goroutine leaks reported.

**Overall**
14. [`.claude/techdebt.md`](../techdebt.md) entries "Golden File Tests Are One-Directional" and "AgentConn.sendControl Lacks Write Mutex" removed.
15. New ADR landed under [`docs/adr/`](../../docs/adr/) documenting reverse-golden + sidecar convention.
16. SonarCloud quality gate green on each PR ([`make sonar`](../../Makefile)).
17. `/precommit` + `/refactor` run before each push per [`CLAUDE.md`](../../CLAUDE.md) (enforced by hooks).

---

## Done-When

- CI enforces byte-match symmetrically in both directions for the wire protocol.
- Every golden has a `.meta.json` sidecar declaring its `protocol_version`.
- Go native fuzzing wired into the nightly cadence.
- Nightly cross-browser job exists, runs green against Firefox + WebKit + Chromium.
- Visual regression baselines committed and enforced under Chromium.
- Accessibility gate in place for 5 key specs.
- Web API codegen drift CI step fails on intentional drift.
- Zero `time.Sleep` remaining in `server/internal/api/` and `server/tests/integration/`.
- `go-integration` CI runtime demonstrably lower after `t.Parallel()` adoption.
- `AgentConn.sendControl` write-mutex landed; the deferred B5 concurrent-send case un-skipped and green.
- WebSocket relay fault-injection suite green.
- [`techdebt.md`](../techdebt.md) and [`phases.md`](../phases.md) updated; new "Clock injection deferred" tech-debt entry added.
