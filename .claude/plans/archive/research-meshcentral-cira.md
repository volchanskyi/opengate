# OpenGate — Full Codebase Audit & Refactoring Plan

## Context

Comprehensive audit of the OpenGate codebase across infrastructure, backend, frontend, and cross-cutting concerns. The project is a production remote device management platform (Rust agent + Go server + React SPA). The audit found **34 actionable issues** ranging from missing rate limiting to absent error boundaries. Changes are split into 4 phases (separate commits) ordered lowest-risk-first so regressions can be caught early.

**Constraints:** No breaking API changes. No data migrations. All existing tests must pass. Branch: `dev` only. CLAUDE.md conventions apply (TDD, `/precommit`, `/refactor`).

---

## Phase 1: Infrastructure Hardening

**Risk: VERY LOW** — Configuration-only changes, no application code modified.

### Changes

#### 1.1 Add container resource limits — `deploy/docker-compose.yml`
Add `deploy.resources` to `server` and `caddy` services:
- server: `limits: { memory: 512M, cpus: '1.0' }`, `reservations: { memory: 128M }`
- caddy: `limits: { memory: 256M, cpus: '0.5' }`, `reservations: { memory: 64M }`
- web-init: `limits: { memory: 128M }`

#### 1.2 Add container resource limits — `deploy/docker-compose.monitoring.yml`
Add `deploy.resources` to all 6 monitoring services:
- victoriametrics: 512M / 0.5 cpu
- grafana: 256M / 0.5 cpu
- loki: 256M / 0.5 cpu
- promtail: 128M / 0.25 cpu
- node-exporter: 64M / 0.25 cpu
- uptime-kuma: 256M / 0.25 cpu

#### 1.3 Add Trivy image scan — `.github/workflows/build-image.yml`
After the "Build and push" step (line 64), add:
```yaml
      - name: Scan image for vulnerabilities
        uses: aquasecurity/trivy-action@0.28.0
        with:
          image-ref: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}:sha-${{ github.sha }}
          format: 'table'
          exit-code: '1'
          severity: 'CRITICAL,HIGH'
          ignore-unfixed: true
```

#### 1.4 Document Terraform remote backend — `deploy/terraform/main.tf`
Add a comment block at top documenting the recommended OCI Object Storage remote backend setup. Documentation only.

#### 1.5 Add compose version comment — `deploy/docker-compose.staging.yml`
Add comment at line 1: `# Requires Docker Compose v2.24+ for !override tag support.`

#### 1.6 Add alerting rules — `deploy/victoriametrics/alerts.yml` (NEW)
Basic VictoriaMetrics alerting rules: ServerDown, HighMemoryUsage, HighErrorRate, HighP95Latency. Mount into victoriametrics via compose volume. Grafana unified alerting is already enabled (line 32 of monitoring compose).

#### 1.7 Update GitHub Wiki
Document Phase 1 changes: container resource limits, Trivy scanning, alerting rules. Update relevant wiki pages (deployment, monitoring, CI/CD).

### Testing Phase 1
```bash
make lint-deploy                         # Validates all compose, terraform, caddy configs
yamllint deploy/victoriametrics/alerts.yml
actionlint .github/workflows/build-image.yml
```

---

## Phase 2: Backend Security & Reliability

**Risk: MEDIUM** — Behavioral changes to existing endpoints, no API contract changes.

### Changes

#### 2.1 Add rate limiting — `server/internal/api/ratelimit.go` (NEW)
Implement per-IP token bucket rate limiter using `golang.org/x/time/rate`:
- `ipLimiter` struct with `sync.Map` of `*rate.Limiter` per IP
- Background cleanup of stale entries (every 3 minutes, evict entries older than 5 min)
- Returns 429 with JSON `{"error":"rate limit exceeded"}` when limit hit
- `RateLimiter(rps float64, burst int) func(http.Handler) http.Handler`

**New dependency:** `golang.org/x/time` — run `go get golang.org/x/time`

#### 2.2 Add rate limiter tests — `server/internal/api/ratelimit_test.go` (NEW)
Table-driven tests:
- Requests under limit pass (200)
- Requests over limit return 429
- Different IPs get independent limits
- Cleanup goroutine evicts stale entries

#### 2.3 Wire rate limiting + request timeout — `server/internal/api/api.go`
In `routes()` function, after `RequestLogger` middleware (line 136), add:
```go
r.Use(RateLimiter(100, 200))  // 100 req/s per IP, burst 200
```
For auth endpoints specifically: add a `chi.Group` around `/api/v1/auth/*` with a tighter limiter `RateLimiter(5, 10)`. Since oapi-codegen controls route registration, we need to add a Chi middleware that matches auth paths.

Also add request timeout middleware:
```go
r.Use(RequestTimeout(30 * time.Second))
```

#### 2.4 Add RequestTimeout middleware — `server/internal/api/middleware.go`
After `SecurityHeaders` (line 122), add:
```go
func RequestTimeout(d time.Duration) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.TimeoutHandler(next, d, `{"error":"request timeout"}`)
    }
}
```
**Note:** `http.TimeoutHandler` does NOT implement `http.Hijacker`, so WebSocket upgrade will fail. The `/ws/relay/*` route is registered AFTER the oapi-codegen handler and is outside the middleware chain (line 166 in api.go). Need to ensure the timeout middleware is applied only to the API routes, not the WebSocket route. Solution: apply `RequestTimeout` inside the oapi-codegen `ChiServerOptions.Middlewares` list rather than as a top-level `r.Use()`, OR apply it only to the API path group.

**Revised approach:** Apply timeout to a sub-group:
```go
// In routes(), wrap the strict handler registration in a Group:
r.Group(func(apiRouter chi.Router) {
    apiRouter.Use(RequestTimeout(30 * time.Second))
    apiRouter.Use(RateLimiter(100, 200))
    HandlerWithOptions(strictHandler, ChiServerOptions{
        BaseRouter: apiRouter,
        ...
    })
})
// WebSocket route stays on r (no timeout)
r.Get("/ws/relay/{token}", s.handleRelayWebSocket)
```

#### 2.5 Add HSTS header — `server/internal/api/middleware.go`
In `SecurityHeaders` (line 119), add before `next.ServeHTTP`:
```go
w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
```
Defense-in-depth — Caddy already sets this but Go should too.

#### 2.6 Add email validation — `server/internal/api/handlers_auth.go`
In `Register()`, after the empty check (line 15) and before password validation, add:
```go
if _, err := mail.ParseAddress(email); err != nil {
    return Register400JSONResponse{Error: "invalid email format"}, nil
}
```
Uses `net/mail` stdlib — no new dependency. Add `"net/mail"` to imports.

#### 2.7 Add CSR signature verification — `server/internal/api/handlers_enrollment.go`
In `signCSR()` (line 186), after PEM decode and before `SignAgentCSR`, add:
```go
csr, err := x509.ParseCertificateRequest(block.Bytes)
if err != nil {
    return "", fmt.Errorf("parse CSR: %w", err)
}
if err := csr.CheckSignature(); err != nil {
    return "", fmt.Errorf("CSR signature invalid: %w", err)
}
```
Add `"crypto/x509"` to imports.

#### 2.8 Reduce WebSocket message size — `server/internal/api/wsconn.go`
Change line 19 from `16 << 20` to `4 << 20`. Update comment to match.
The agent protocol chunks data, so 4 MiB per message is sufficient.
Also update `wsconn_test.go` line 26 if it references the constant.

#### 2.9 JWT secret minimum length — `server/cmd/meshserver/main.go`
After line 53 (`if secret == ""`), add:
```go
if len(secret) < 32 {
    logger.Error("jwt secret must be at least 32 characters")
    os.Exit(1)
}
```

#### 2.10 Audit log level upgrade — `server/internal/api/api.go`
In `auditLog()` (line 224), change `s.logger.Warn` to `s.logger.Error`.

#### 2.11 Clean up orphaned session on agent send failure — `server/internal/api/handlers_sessions.go`
At line 73-75, replace the fire-and-forget error log with cleanup:
```go
if err := agentConn.SendSessionRequest(ctx, token, relayURL, perms); err != nil {
    s.logger.Error("send session request to agent", "error", err, "device_id", deviceID)
    _ = s.store.DeleteAgentSession(ctx, string(token))
    return CreateSession409JSONResponse{Error: "agent communication failed"}, nil
}
```
Uses existing `CreateSession409JSONResponse` (already defined for "agent not connected").

#### 2.12 Add auth rate limiter middleware tests — `server/internal/api/middleware_test.go`
Add test cases for:
- `RequestTimeout` returns 503 on slow handler
- `SecurityHeaders` includes HSTS
- Email validation (in auth handler tests)

#### 2.13 Update GitHub Wiki
Document Phase 2 changes: rate limiting (limits and behavior), request timeouts, HSTS, email validation, JWT secret requirements, reduced WebSocket message size.

### Testing Phase 2
```bash
cd server && go get golang.org/x/time   # Add new dependency
make test-go                             # All Go unit tests with race detector
make test-integration                    # Integration tests
make e2e                                 # Full E2E (validates WebSocket still works)

# Manual rate limit verification:
cd deploy && docker compose -f docker-compose.test.yml up -d --build --wait
for i in $(seq 1 15); do
  curl -s -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8080/api/v1/auth/login \
    -H "Content-Type: application/json" -d '{"email":"test@test.com","password":"wrong"}'
done
# Expect 429s after burst limit

# JWT secret length check:
docker compose -f deploy/docker-compose.test.yml run --rm -e JWT_SECRET=short server
# Should exit with "jwt secret must be at least 32 characters"
```

---

## Phase 3: Frontend Quality & Resilience

**Risk: MEDIUM** — UI behavioral changes, no API changes.

### Changes

#### 3.1 Create ErrorBoundary — `web/src/components/ErrorBoundary.tsx` (NEW)
React class component catching rendering errors. Shows "Something went wrong" with reload button. Dark-themed to match app.

#### 3.2 Create ErrorBoundary test — `web/src/components/ErrorBoundary.test.tsx` (NEW)
Tests: catches errors, renders fallback, renders children when no error.

#### 3.3 Create LoadingSpinner — `web/src/components/LoadingSpinner.tsx` (NEW)
Simple centered spinner component for Suspense fallbacks. Tailwind-only.

#### 3.4 Wrap App in ErrorBoundary — `web/src/App.tsx`
```tsx
import { ErrorBoundary } from './components/ErrorBoundary';
// ...
return (
  <ErrorBoundary>
    <RouterProvider router={router} />
  </ErrorBoundary>
);
```

#### 3.5 Add lazy loading to routes — `web/src/router.tsx`
Convert feature page imports to `lazy()`:
- `Dashboard`, `DeviceList`, `DeviceDetail`, `SessionView`, `AgentSetupPage`, `ProfilePage`
- `AdminLayout`, `UserManagement`, `AuditLog`, `AgentUpdates`, `Permissions`

Keep eagerly loaded: `LoginPage`, `RegisterPage`, `AuthGuard`, `AdminGuard`, `Layout`

Each route element wrapped in `<Suspense fallback={<LoadingSpinner />}>`.

**Note:** `createBrowserRouter` requires elements, not component references. The lazy routes need to be wrapped at the element level:
```tsx
const LazyDeviceList = lazy(() => import('./features/devices/DeviceList').then(m => ({ default: m.DeviceList })));
// ...
{ path: 'devices', element: <Suspense fallback={<LoadingSpinner />}><LazyDeviceList /></Suspense> },
```

#### 3.6 Fix login race condition — `web/src/features/auth/LoginPage.tsx`
Replace the `handleSubmit` post-await state check (lines 19-25) with:
```tsx
const handleSubmit = async (e: React.SyntheticEvent) => {
  e.preventDefault();
  await login(email, password);
};
```
Add a `useEffect` that watches `token` and `user` for navigation:
```tsx
useEffect(() => {
  if (token && user) {
    navigate('/devices', { replace: true });
  }
}, [token, user, navigate]);
```
The existing `if (token && user) return <Navigate>` (line 15) handles the already-logged-in case.

#### 3.7 Replace module-level counters — `web/src/state/toast-store.ts`
Replace lines 17-18 (`let nextId = 0` and `const id = String(++nextId)`) with:
```ts
const id = crypto.randomUUID();
```
Remove the `let nextId = 0` line entirely.

#### 3.8 Replace module-level counters — `web/src/state/chat-store.ts`
- Remove `let nextMessageId = 0` (line 4)
- Change `ChatMessage.id` type from `number` to `string` (line 7)
- Replace `id: nextMessageId++` with `id: crypto.randomUUID()`
- Update any consumers of `ChatMessage.id` that assume number type

#### 3.9 Add aria-live to toast container — `web/src/components/ToastContainer.tsx`
On the container div (line 16), add `aria-live="polite"` and `aria-label="Notifications"`.

#### 3.10 Update GitHub Wiki
Document Phase 3 changes: ErrorBoundary, lazy loading, login fix, accessibility improvements.

### Testing Phase 3
```bash
make test-web                           # All Vitest unit tests
cd web && npx eslint src/               # Lint check
cd web && npx vite build                # Verify build succeeds with lazy loading
ls -la web/dist/assets/*.js | wc -l     # Should be >5 (code-split chunks)
make e2e                                # Full E2E tests

# Verify ErrorBoundary:
cd web && npx vitest run src/components/ErrorBoundary.test.tsx

# Verify login flow:
cd web && npx vitest run src/features/auth/LoginPage.test.tsx  # if exists
```

---

## Phase 4: Cross-Cutting Refactoring & CI Gates

**Risk: VERY LOW** — Test/tooling changes only, no behavioral changes.

### Changes

#### 4.1 Add OpenAPI codegen sync check — `.github/workflows/ci.yml`
In the `go-lint` job (line 88), after `go vet` (line 102), add:
```yaml
      - name: Verify OpenAPI codegen is in sync
        run: |
          go generate ./internal/api/
          git diff --exit-code internal/api/ || {
            echo "::error::OpenAPI generated code is out of sync. Run 'go generate ./internal/api/' and commit."
            exit 1
          }
```

**Note:** The `golden` job (line 247) and `web-bundle-size` job (line 372) already exist in CI. No duplication needed.

#### 4.2 Add `verify-codegen` Makefile target — `Makefile`
After `golden:` target (line 63), add:
```makefile
verify-codegen:
	cd server && go generate ./internal/api/ && git diff --exit-code internal/api/
```

#### 4.3 Standardize test helpers — `server/internal/testutil/testutil.go`
Review existing helpers and add any missing ones. Key function: ensure `NewTestStore` exists and returns a properly configured in-memory SQLite store. If it already uses `t.TempDir()`, keep that approach for consistency.

#### 4.4 Add auth-specific rate limit tests — `server/internal/api/auth_handlers_test.go`
Add test case verifying that email validation rejects malformed emails but accepts valid ones (table-driven).

#### 4.5 Update GitHub Wiki
Document Phase 4 changes: codegen sync check, new Makefile targets, test helper improvements.

### Testing Phase 4
```bash
make verify-codegen                     # New target
make lint                               # Full lint (includes actionlint)
make test                               # All tests (Rust + Go + Web)
make golden                             # Cross-language compatibility
make ci                                 # Full CI simulation
```

---

## Commit Strategy

| Phase | Branch | Commit Message |
|-------|--------|---------------|
| 1 | `dev` | `infra: add container resource limits, Trivy scan, backup script, alerting rules` |
| 2 | `dev` | `security: add rate limiting, email validation, request timeout, HSTS, CSR verification` |
| 3 | `dev` | `frontend: add error boundaries, lazy loading, fix login race condition, improve a11y` |
| 4 | `dev` | `ci: add codegen sync check, standardize test helpers` |

Each commit: run `/precommit` → commit → push to `dev` → verify CI passes → proceed to next phase.

## Key Files Summary

| File | Phases |
|------|--------|
| `deploy/docker-compose.yml` | 1 |
| `deploy/docker-compose.monitoring.yml` | 1 |
| `.github/workflows/build-image.yml` | 1 |
| `.github/workflows/ci.yml` | 4 |
| `server/internal/api/middleware.go` | 2 |
| `server/internal/api/api.go` | 2 |
| `server/internal/api/handlers_auth.go` | 2 |
| `server/internal/api/handlers_enrollment.go` | 2 |
| `server/internal/api/handlers_sessions.go` | 2 |
| `server/internal/api/wsconn.go` | 2 |
| `server/cmd/meshserver/main.go` | 2 |
| `web/src/App.tsx` | 3 |
| `web/src/router.tsx` | 3 |
| `web/src/features/auth/LoginPage.tsx` | 3 |
| `web/src/state/toast-store.ts` | 3 |
| `web/src/state/chat-store.ts` | 3 |
| `web/src/components/ToastContainer.tsx` | 3 |
| `Makefile` | 4 |
