# Plan: Fix E2E Tests Locally + Fix Install Script "No Binary Found"

## Context

Two issues to solve:

1. **E2E tests fail locally** (22/22) but pass in CI and staging. Root causes: stale Docker state between runs (no `down -v` before `up`), and no `serviceWorkers: 'block'` in Playwright config.

2. **Install script fails** with "No agent binary found for linux/amd64". Root cause: the install script's two fallback strategies both fail:
   - **Strategy 1 (GitHub)**: `OPENGATE_GITHUB_REPO` env var is not available in the `curl | sudo bash` client environment
   - **Strategy 2 (Server manifests)**: `GET /api/v1/updates/manifests` requires `bearerAuth` (OpenAPI spec line 1172), so the unauthenticated `curl` in the script gets 401

---

## Part 1: Fix Local E2E Tests

### Step 1: Add `webServer` lifecycle to `playwright.config.ts`

**File**: `web/playwright.config.ts`

Add `webServer` config that manages docker-compose lifecycle:

```ts
webServer: {
  command: 'cd ../deploy && docker compose -f docker-compose.test.yml down -v 2>/dev/null; docker compose -f docker-compose.test.yml up --build --wait',
  url: 'http://localhost:8080/api/v1/health',
  reuseExistingServer: !!process.env.CI,
  timeout: 180_000,
},
```

- **Locally** (`CI` unset): runs `down -v` (fresh DB) then `up --build --wait`
- **In CI** (`CI=true`): skips command, uses the server already started by the workflow

### Step 2: Add `globalTeardown` to clean up docker-compose

**File**: `web/e2e/global-teardown.ts` (new)

```ts
import { execSync } from 'child_process';
export default async function globalTeardown() {
  if (!process.env.CI) {
    execSync('docker compose -f docker-compose.test.yml down -v', {
      cwd: new URL('../../deploy', import.meta.url).pathname,
      stdio: 'inherit',
    });
  }
}
```

Register in `playwright.config.ts`:
```ts
globalTeardown: './e2e/global-teardown.ts',
```

### Step 3: Block service workers in Playwright

**File**: `web/playwright.config.ts`

Add to the `use` block:
```ts
serviceWorkers: 'block',
```

This prevents any cached service worker from intercepting requests during tests.

### Step 4: Harden `global-setup.ts` with admin verification

**File**: `web/e2e/global-setup.ts`

After obtaining the bootstrap token, verify the user is actually admin:
```ts
const meResp = await ctx.get('/api/v1/users/me', {
  headers: { Authorization: `Bearer ${process.env.BOOTSTRAP_ADMIN_TOKEN}` },
});
if (meResp.ok()) {
  const me = await meResp.json();
  if (!me.is_admin) {
    throw new Error(
      'Bootstrap admin is not admin — DB may be stale. Run: cd deploy && docker compose -f docker-compose.test.yml down -v'
    );
  }
}
```

### Step 5: Add `test:e2e` npm script

**File**: `web/package.json`

Add to `scripts`:
```json
"test:e2e": "playwright test"
```

---

## Part 2: Fix Install Script "No Binary Found"

### Step 6: Inject `OPENGATE_GITHUB_REPO` into install script

**File**: `server/internal/api/handlers_install.go`

Same pattern as `OPENGATE_SERVER` injection. When the server has `githubRepo` configured, inject it:

```go
if s.githubRepo != "" {
    repoLine := []byte(fmt.Sprintf("export OPENGATE_GITHUB_REPO=%q\n", s.githubRepo))
    prefix = append(prefix, repoLine...)
}
```

This makes Strategy 1 (GitHub releases) work because the script now has the repo name.

### Step 7: Make manifests endpoint publicly accessible (optional alternative)

**File**: `api/openapi.yaml` (line 1172)

Change `GET /api/v1/updates/manifests` from:
```yaml
security:
  - bearerAuth: []
```
to:
```yaml
security: []
```

This makes Strategy 2 also work. The manifest list is non-sensitive data (version, OS, arch, download URL, SHA256). Keeping it behind auth blocks the install script's fallback path.

> **Recommendation**: Do Step 6 (inject repo) AND Step 7 (public manifests). Belt and suspenders — if GitHub API is down, the server manifests fallback works.

### Step 8: Add tests for install script GITHUB_REPO injection

**File**: `server/internal/api/handlers_enrollment_test.go`

Add test case in `TestGetInstallScript`:
```go
t.Run("injects OPENGATE_GITHUB_REPO when configured", func(t *testing.T) {
    srv, _ := newTestServerWithCert(t)
    srv.githubRepo = "volchanskyi/opengate"
    // ... assert script contains: export OPENGATE_GITHUB_REPO="volchanskyi/opengate"
})
```

### Step 9: Regenerate OpenAPI types (if Step 7 done)

```bash
cd server && go generate ./...
```

---

## Files to Modify

| File | Change |
|------|--------|
| `web/playwright.config.ts` | Add `webServer`, `globalTeardown`, `serviceWorkers: 'block'` |
| `web/e2e/global-teardown.ts` | **New** — docker-compose cleanup after local runs |
| `web/e2e/global-setup.ts` | Add admin verification, fail-fast on stale DB |
| `web/package.json` | Add `test:e2e` script |
| `server/internal/api/handlers_install.go` | Inject `OPENGATE_GITHUB_REPO` into script |
| `server/internal/api/handlers_enrollment_test.go` | Test for GITHUB_REPO injection |
| `api/openapi.yaml` | Make `GET /updates/manifests` public (remove bearerAuth) |
| `server/internal/api/oapi_gen.go` | Regenerate after openapi.yaml change |

## Verification

### E2E Tests
1. `cd web && npx playwright test` — should start docker-compose, run all 22 tests, tear down
2. Run again immediately — should pass again (fresh DB each time)
3. Push to `dev` — CI e2e job should still pass (uses `reuseExistingServer`)

### Install Script
1. Deploy to staging
2. Create enrollment token via admin UI
3. Run: `curl -sL https://opengate.cloudisland.net/api/v1/server/install.sh | head -5` — verify `OPENGATE_GITHUB_REPO` is injected
4. Run full install on a test machine
