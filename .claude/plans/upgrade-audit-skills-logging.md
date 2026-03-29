# Upgrade All Audit Skills with Logging Best Practices

## Context

The three audit skills (`backend-audit`, `infra-audit`, `frontend-audit`) have shallow logging coverage despite the project having a mature logging stack (Go `slog` JSON, Rust `tracing`, Loki/Promtail/Grafana). Confirmed gaps include: failed logins not logged, 403/429 responses invisible, request IDs not propagated to log messages, Rust agent has no redaction, audit trail uses fire-and-forget goroutines, and frontend errors are only visible in browser DevTools. These are real security blind spots that audits should catch.

**What NOT to add (and why):**
- OpenTelemetry/distributed tracing — single-server deploy, request ID correlation is sufficient
- External SaaS (Sentry, Datadog, Splunk) — self-hosted project on OCI Always Free tier
- ELK stack checks — project uses Loki, not ELK
- Log rotation config — Docker JSON driver + Loki 14-day retention already handles this
- Separate audit database — SQLite `audit_events` table exists and works
- Log sampling — not at scale needing it
- Rust `tracing-opentelemetry` — overkill for single-binary agent

---

## Skill 1: Backend Audit

**File:** `/home/ivan/opengate/.claude/skills/backend-audit/SKILL.md`

### 1A. Split and expand section 5 "Sensitive Data Exposure"

Current section 5 has subsections 5a-5d. Keep 5a (response filtering), 5b (error sanitization), 5c (logging hygiene), 5d (security event logging) intact but expand 5c/5d and add 5e-5h.

### 1B. Expand section 5c "Logging hygiene" — add log injection prevention

After the existing grep pattern and verification bullets, add a new **Log injection (CWE-117)** block:

- Scan for user-controlled strings passed as slog field values (email, hostname, display name, group name, OS) that could embed newlines or fake JSON keys
- Grep: `grep -rn 'slog\.\(Info\|Warn\|Error\)' server/ --include='*.go' | grep -v '_test.go'` — inspect each hit for user-supplied field values
- Verify `slog.NewJSONHandler` is used (not text handler) in `server/cmd/meshserver/main.go` — JSON encoding auto-escapes newlines, providing partial mitigation
- Verify `RequestLogger` in `server/internal/api/middleware.go:157` does NOT log raw `r.URL.RawQuery` (query strings can contain tokens/credentials)
- Verify `r.Header.Get("Authorization")` is never passed to any slog call
- Severity: MEDIUM (JSON handler partially mitigates), HIGH if text handler is used
- Fix pattern: use `slog.String(key, sanitized)` where sanitized strips control chars for user-facing fields; for JSON handler, verify escaping is not disabled

### 1C. Expand section 5d "Security event logging" — comprehensive event table

Replace the current incomplete bullet list with a verification table:

| Security event | Required level | Where to check | Currently logged? |
|---|---|---|---|
| Failed login (wrong password) | WARN | `handlers_auth.go:89-90` | NO — returns 401, no slog/auditLog |
| Failed login (unknown email) | WARN | `handlers_auth.go:83-84` | NO — returns 401, no slog/auditLog |
| Successful login | INFO | `handlers_auth.go:99` | auditLog only (DB), not slog |
| User registration | INFO | `handlers_auth.go:69` | auditLog only |
| JWT validation failure | WARN | `middleware.go:64-66` | NO — returns 401 silently |
| Authorization denied (403) | WARN | `middleware.go` denyIfNotAdmin | NO — returns 403 silently |
| Rate limit violation (429) | WARN | `ratelimit.go:70-72` | NO — returns 429 silently |
| Password change | INFO | Check if endpoint exists | Verify |
| Admin privilege change | WARN | handlers_users.go | Verify |

Add grep commands to find gaps:
```bash
# All auditLog call sites
grep -rn 'auditLog(' server/internal/api/ --include='*.go' | grep -v '_test.go'
# Auth failure paths lacking logging
grep -rn '401\|403\|429' server/internal/api/ --include='*.go' | grep -v '_test.go'
```

For every missing event: add both `slog.Warn` with structured fields (`"event"`, `"ip"`, redacted email) AND `auditLog()` for persistent trail. Severity: HIGH for missing failed login/auth failure logging.

### 1D. New section 5e "Request correlation"

- Verify `middleware.RequestID` is registered (`api.go:130`) — confirmed present
- Check `RequestLogger` (`middleware.go:157-162`) includes `"request_id", middleware.GetReqID(r.Context())` — currently it does NOT
- Check `auditLog()` (`api.go:221-235`) propagates request ID — currently uses `context.Background()` which loses it entirely
- Check error response paths (`writeError`, handler error logging at `api.go:145,149`) include request_id — they don't
- Grep for all slog calls missing request_id: `grep -rn 'slog\.\(Info\|Warn\|Error\)' server/internal/api/ --include='*.go' | grep -v '_test.go' | grep -v 'request_id'`
- Severity: HIGH — without correlation, multi-request incidents cannot be traced through Loki
- Fix pattern: inject request_id into RequestLogger, pass request context (not `context.Background()`) to auditLog, add request_id to error handlers

### 1E. New section 5f "Log level discipline"

Table of required severity assignments:

| Event | Required level | Current level | File:line |
|---|---|---|---|
| Response write failure | WARN | Debug | `middleware.go:189` |
| Request validation error | WARN | Warn | `api.go:145` (correct) |
| Rate limit hit | WARN | (not logged) | `ratelimit.go:71` |
| Database error | ERROR | ERROR | `api.go:149` (correct) |
| Successful request | INFO | INFO | `middleware.go:157` (correct) |

Scan: `grep -rn 'slog\.Debug' server/internal/api/ --include='*.go' | grep -v '_test.go'` — any Debug-level log for an operational event that operators need to see is MEDIUM.

### 1F. New section 5g "Audit trail reliability"

Verify `auditLog()` in `server/internal/api/api.go:221-235`:
- Fire-and-forget `go func()` — unbounded goroutine spawning under load (1000 simultaneous audit events = 1000 goroutines). Severity: MEDIUM.
- `context.Background()` does NOT inherit request cancellation — this is intentional (audit write must outlive request). Verify this remains correct.
- 5-second timeout with `MaxOpenConns(1)` SQLite — serial writes could queue up. Verify timeout is sufficient under expected load.
- Write errors ARE logged at ERROR (`api.go:232`). Confirm.
- Graceful shutdown: `go func()` goroutines may be abandoned on `SIGTERM`. Severity: LOW.
- Fix pattern: document accepted risk, or add a bounded channel/worker pool for audit writes.

### 1G. New section 5h "Rust agent logging standards"

The backend audit currently only covers Go. The Rust agent is part of the backend system and its logs feed into the same Loki pipeline.

- Verify `tracing` crate is used (not `println!` or `eprintln!` in production paths): `grep -rn 'println!\|eprintln!' agent/crates/ --include='*.rs' | grep -v 'target/\|test'`
- Verify no session tokens or enrollment tokens are logged in full: `grep -rn 'token' agent/ --include='*.rs' | grep -v 'target/\|_test\|test_\|RedactToken'`
- Check for `#[instrument]` usage on key async functions (connection loop, session handler, update pipeline) — currently NONE used. Severity: LOW (nice-to-have, not security).
- Verify `tracing_subscriber` uses `env-filter` for runtime level control via `RUST_LOG` — confirmed in `main.rs:229-234`
- Check output format: currently text formatter, not JSON. If agent runs in Docker with Promtail JSON parsing, this breaks log ingestion. Severity: MEDIUM if containerized, LOW if systemd.
- Fix pattern for redaction: add a `redact_token()` utility in Rust mirroring Go's `RedactToken()` — first 8 chars + "..."

### 1H. Update summary table (section 12)

Update the section 5 row to reflect expanded scope:
```
| 5 Data   |   0   | Response filtering, error sanitization, logging hygiene,  | PASS   |
|          |       | injection, correlation, audit trail, agent logging        |        |
```

**Estimated change: +140 lines (sections 5e-5h new, 5c-5d expanded). Total ~677 lines.**

---

## Skill 2: Infra Audit

**File:** `/home/ivan/opengate/.claude/skills/infra-audit/SKILL.md`

### 2A. Expand section 9 "PII exposure check" into proper subsections

Current content is 3 bullet points. Split into 9a/9b/9c:

**9a. Test data hygiene** (existing bullet, expanded):
```bash
grep -rn '@' server/tests/ --include='*.go' | grep -v '@test.local\|@example.com\|_test.go' | head -10
grep -rn 'email\|password' deploy/scripts/ --include='*.sh' | grep -v 'changeme\|example'
```
Flag real-looking emails/passwords in test fixtures as MEDIUM.

**9b. Application log PII scan** (existing bullet, expanded with concrete checks):

Go server scan:
```bash
grep -rn 'slog\.\(Info\|Debug\|Warn\|Error\)' server/ --include='*.go' | grep -v '_test.go'
```
Verify: emails only at INFO for auth events, passwords/hashes never logged, tokens use `RedactToken()`, session tokens show first 8 chars only.

Rust agent scan:
```bash
grep -rn 'info!\|warn!\|error!\|debug!' agent/ --include='*.rs' | grep -v 'target/'
```
Verify: enrollment tokens not logged in full, file paths from user operations at DEBUG only, relay session tokens redacted.

**9c. Error response leakage** (existing bullet, expanded):
```bash
grep -rn 'err\.Error()' server/internal/api/ --include='*.go' | grep -v '_test.go'
```
Verify error messages to clients contain no internal paths, SQL, or stack traces.

### 2B. New section 10 "Log infrastructure audit"

Renumber current section 10 (summary) to section 11. Insert new section 10:

**10a. Promtail configuration**
- Read `deploy/promtail/promtail-config.yml`
- Verify JSON parsing stages exist for each container emitting JSON logs (`opengate-server`, `opengate-caddy`)
- Check labels extracted: `level`, `msg`, `error`, `status`, `method` — the `error` field as a Loki label is risky if errors contain PII. Severity: MEDIUM.
- Verify positions file is on a persistent volume (not ephemeral)
```bash
cat deploy/promtail/promtail-config.yml
grep -A5 'labels:' deploy/promtail/promtail-config.yml
```

**10b. Loki configuration**
- Read `deploy/loki/loki-config.yml`
- Verify `auth_enabled` setting — `false` (single-tenant) is acceptable IF Loki is not exposed externally
- Verify `retention_period` is configured (currently 336h / 14 days)
- Verify `retention_enabled: true` in compactor
- Check ingestion rate limits are reasonable
```bash
cat deploy/loki/loki-config.yml
```

**10c. Log access control**
- Verify Grafana requires authentication (check `GF_SECURITY_ADMIN_PASSWORD` in compose env vars)
- Verify Loki API (port 3100) is NOT exposed to public internet (check Caddyfile, docker-compose port mappings)
- Verify Promtail API (port 9080) is NOT exposed externally
```bash
grep -rn '3100\|9080' deploy/caddy/ deploy/docker-compose*.yml 2>/dev/null
```
Severity: HIGH if Loki/Grafana publicly accessible without auth.

**10d. Log completeness**
- Go server: verify `slog.NewJSONHandler(os.Stdout, ...)` in `server/cmd/meshserver/main.go`
- Caddy: verify JSON log format (default)
- Rust agent: currently `tracing_subscriber::fmt()` with text format (NOT JSON) — Promtail JSON stages cannot parse this. Severity: MEDIUM if agent is containerized.
```bash
grep -rn 'JSONHandler\|TextHandler' server/ --include='*.go' | grep -v '_test.go'
grep -rn 'tracing_subscriber' agent/ --include='*.rs'
```

### 2C. Renumber summary to section 11, add row

```
| 10       |   0   | Log infra: Promtail, Loki, access control, completeness | PASS   |
```

**Estimated change: +85 lines (section 9 expanded +30, new section 10 ~55). Total ~302 lines.**

---

## Skill 3: Frontend Audit

**File:** `/home/ivan/opengate/.claude/skills/frontend-audit/SKILL.md`

### 3A. Expand section 7a — add console.log production discipline

Append to existing 7a content:

Check for `console.log` in production code:
```bash
grep -rn 'console\.log' web/src/ --include='*.ts' --include='*.tsx' | grep -v 'node_modules\|_test\.\|\.test\.'
```
- `console.log` in production code: LOW (noisy, may leak internal state to DevTools users)
- Verify no `console.log(token)`, `console.log(password)`, or `console.log(response)` where response may contain sensitive data
- `console.warn`/`console.error` in WebRTC/transport code (e.g., `connection-store.ts:45,54,158`) is acceptable if no tokens/PII

### 3B. New section 7d "Frontend error observability"

Check if production errors are reported beyond browser DevTools:
```bash
grep -rn 'reportError\|sendBeacon\|fetch.*error\|POST.*error\|navigator\.sendBeacon' web/src/ --include='*.ts' --include='*.tsx' | grep -v 'node_modules\|_test\.\|\.test\.'
```

Verify:
- `ErrorBoundary` (`web/src/components/ErrorBoundary.tsx:19`) currently only calls `console.error`. In production, invisible to operators. Severity: MEDIUM.
- Check for global `window.onerror` or `window.addEventListener('unhandledrejection', ...)` handler
- Check if service worker (`web/public/sw.js`) reports its own errors

Recommended fix pattern (self-hosted, no SaaS dependency):
```tsx
// BAD
console.error('ErrorBoundary caught:', error);

// GOOD — report to server for Loki ingestion
if (import.meta.env.PROD) {
  navigator.sendBeacon('/api/v1/client-errors', JSON.stringify({
    message: error.message,
    stack: error.stack?.slice(0, 500),
    url: window.location.pathname,
    timestamp: new Date().toISOString(),
  }));
}
```

If error reporting exists, verify:
- Does NOT include auth tokens, user email, or PII in payload
- Truncates stack traces (max 500 chars) to prevent log flooding
- Uses `navigator.sendBeacon` (fire-and-forget, works during page unload)
- Rate-limited client-side (max 10 reports/minute) to prevent self-DoS

**What NOT to recommend:** Sentry, LogRocket, or other SaaS. Project is self-hosted. The `sendBeacon` to a server endpoint pattern is sufficient.

### 3C. Update summary table row for section 7

```
| 7  Errors |   0   | No info leakage, boundaries present, errors observable  | PASS   |
```

**Estimated change: +45 lines (7a expansion +10, 7d new +35). Total ~860 lines.**

---

## Implementation Order

1. **Backend audit** — largest impact, security event gaps are real vulnerabilities
2. **Infra audit** — log infrastructure validation builds on backend logging understanding
3. **Frontend audit** — smallest change, lowest severity findings

## Verification

After editing all three skill files:
1. Run each audit skill in a dry-read mode (read-only scan of the codebase) to verify all grep commands work and reference correct file paths
2. Verify all referenced files still exist (`middleware.go`, `api.go`, `ratelimit.go`, `handlers_auth.go`, Promtail/Loki configs)
3. Verify the summary tables have correct row counts matching the actual section numbers
4. Confirm no duplicate checks exist between skills (backend 5h Rust agent vs infra 9b Rust agent — backend focuses on log content/security, infra focuses on log format/ingestion)

## Severity Distribution of New Checks

| Severity | Backend | Infra | Frontend | Total |
|----------|---------|-------|----------|-------|
| HIGH | 3 | 1 | 0 | 4 |
| MEDIUM | 5 | 3 | 2 | 10 |
| LOW | 2 | 0 | 1 | 3 |
