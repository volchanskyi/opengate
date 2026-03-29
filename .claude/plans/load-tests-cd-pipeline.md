# Load Tests Workflow + Staging E2E in CD

## Context

Two changes requested:
1. **Load tests**: Move out of CI into a dedicated scheduled workflow, split into two sequential jobs (k6, then Go QUIC harness), running against staging via SSH
2. **Staging E2E**: Add Playwright tests to the CD `deploy-staging` job, after smoke tests. A staging Playwright config already exists at `web/playwright.staging.config.ts` targeting `http://127.0.0.1:18080`

## Change 1: Load test workflow

### 1a. Remove from `ci.yml`
**File:** `.github/workflows/ci.yml`

- Delete `load-test` job (lines 370-397)
- Delete `schedule` trigger (lines 9-10) — only `load-test` uses it

### 1b. Create `.github/workflows/load-test.yml`

Single job with two phases (k6 then QUIC), sharing one SSH/firewall lifecycle on the same runner.

**Triggers:**
- `workflow_dispatch` with optional `agents` input (default `100`)
- `schedule: cron '0 6 * * *'` (daily 6am UTC)

**Job: `load-test`**

*Setup (shared):*
1. Checkout
2. Setup Go 1.26 (needed for QUIC harness compilation)
3. Install OCI CLI, configure OCI creds (cd.yml lines 81-103)
4. Cleanup stale firewall rules, open SSH, configure SSH (cd.yml lines 105-190)

*Phase 1 — k6:*
5. Install k6 on VPS via SSH: `curl | tar xz` to `/tmp/`
6. SCP k6 scenario files: `scp load/k6/scenarios/*.js deploy-target:/tmp/`
7. Run each scenario via SSH against `http://127.0.0.1:18080`:
   - `api-baseline.js`, `concurrent-agents.js`, `relay-throughput.js`
8. Cleanup remote k6 files

*Phase 2 — QUIC:*
9. Cross-compile on runner: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/loadtest ./tests/loadtest/`
10. SCP binary to VPS
11. Run via SSH: `/tmp/loadtest -agents=$AGENTS -addr=127.0.0.1:19090`
12. Cleanup remote binary

*Teardown (shared, always):*
13. Close SSH firewall + cleanup credentials

## Change 2: Staging E2E in CD

### Add Playwright steps to `deploy-staging` job in `cd.yml`

**File:** `.github/workflows/cd.yml` — `deploy-staging` job (lines 71-250)

Insert after the "Run smoke tests" step (line 226-229), before "Rollback on failure" (line 231):

1. **Setup Node.js** — `actions/setup-node@v6` with node 24, cache npm
2. **Install Playwright deps** — `cd web && npm ci && npx playwright install --with-deps chromium`
3. **Open SSH tunnel** — `ssh -fN -L 18080:127.0.0.1:18080 deploy-target` (background, forwards staging port to runner's localhost)
4. **Run Playwright** — `cd web && npx playwright test --config=playwright.staging.config.ts`
5. **Upload report on failure** — `actions/upload-artifact@v7` with `web/playwright-report/`

Smoke tests run on the VPS via SSH (`ssh deploy-target "bash smoke-test.sh"`), so they reach `127.0.0.1:18080` locally. Playwright needs a browser (Chromium) which isn't on the VPS, so it must run on the GHA runner. The SSH tunnel forwards the VPS staging port to the runner's localhost, matching `playwright.staging.config.ts`'s `baseURL`.

The rollback step (line 231, `if: failure()`) already covers Playwright failures — if E2E fails, staging gets rolled back.

## Files

| File | Action |
|------|--------|
| `.github/workflows/ci.yml` | Remove `load-test` job (lines 370-397) + `schedule` trigger (lines 9-10) |
| `.github/workflows/load-test.yml` | **Create** — single job, two phases |
| `.github/workflows/cd.yml` | Add Playwright E2E steps to `deploy-staging` job |

## Verification

1. `actionlint` passes on all three workflow files
2. CI push/PR triggers no longer include load test jobs
3. Manual `workflow_dispatch` of `load-test.yml` succeeds against staging
4. CD staging deploy runs Playwright after smoke tests
