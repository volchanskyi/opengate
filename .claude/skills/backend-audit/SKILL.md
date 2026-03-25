---
name: backend-audit
description: |
  Audit backend application code for OWASP Top 10 vulnerabilities, broken access
  control, injection flaws, insecure deserialization, security misconfigurations,
  and missing hardening. Fixes issues in-place and reports findings.
---

# Backend Security Audit

Systematically audit every backend endpoint, handler, middleware, database query, serialization boundary, and security control. Fix every issue you find in-place. Write tests for every fix. Report a summary at the end.

**Severity levels:** CRITICAL (exploitable vulnerability), HIGH (missing security control), MEDIUM (defense-in-depth gap), LOW (best-practice improvement).

**Reference:** OWASP Secure Coding Practices, OWASP Top 10 (2021), CWE/SANS Top 25.

---

## 1. Broken Access Control / IDOR (OWASP A01)

The most critical category. Every endpoint that operates on a resource MUST verify the requesting user is authorized to access that specific resource.

### 1a. Resource ownership checks

For each handler in `server/internal/api/handlers_*.go`, verify:

| Resource | Who may access | How to verify |
|----------|---------------|---------------|
| Device | Owner of the device's group | Query device → get group → check group.OwnerID == ContextUserID |
| Group | Owner of the group | Query group → check group.OwnerID == ContextUserID |
| Session | User who created the session | Query session → check session.UserID == ContextUserID |
| User (self) | The user themselves | Check request.Id == ContextUserID |
| User (admin) | Admins only | Check isAdmin(ctx) |
| Audit log | Admins only | Check isAdmin(ctx) |
| AMT devices | Admins only | Check isAdmin(ctx) |

For every handler, trace the full path from request parameter to database query. If the handler accepts an ID (device ID, group ID, session token, user ID) and queries the database without filtering by the authenticated user, it is an **IDOR vulnerability — CRITICAL**.

### 1b. Horizontal privilege escalation

Check that:
- A non-admin user CANNOT list/read/modify/delete another user's devices
- A non-admin user CANNOT list/read/modify/delete another user's groups
- A non-admin user CANNOT delete/create sessions for devices they don't own
- A non-admin user CANNOT access another user's audit log entries
- Self-modification endpoints (e.g., change own password) validate `request.Id == ContextUserID`

### 1c. Vertical privilege escalation

Check that:
- Non-admin users CANNOT promote themselves to admin
- The `UpdateUser` handler validates that only admins can set `IsAdmin`
- Registration does NOT allow setting `IsAdmin` in the request body
- No endpoint allows bypassing the admin check via request manipulation

### 1d. Missing authentication

Verify that ONLY these endpoints are accessible without authentication:
- `POST /api/v1/auth/register`
- `POST /api/v1/auth/login`
- `GET /api/v1/health`
- `GET /api/v1/push/vapid-key` (public key, safe)

ALL other endpoints MUST require a valid JWT via the auth middleware. Check the OpenAPI spec (`api/openapi.yaml`) and verify every endpoint has a `security: [bearerAuth: []]` block unless it is in the public list above.

### 1e. Fix pattern

For missing ownership checks, add a helper or inline check:

```go
// Example: verify device belongs to the requesting user's group
device, err := s.store.GetDevice(ctx, request.Id)
if err != nil { ... }
group, err := s.store.GetGroup(ctx, device.GroupID)
if err != nil { ... }
if group.OwnerID != ContextUserID(ctx) && !isAdmin(ctx) {
    return nil, ErrForbidden
}
```

Add a table-driven test for each fix with cases: owner access (pass), non-owner access (403), admin access (pass).

---

## 2. Injection (OWASP A03)

### 2a. SQL injection

Scan ALL queries in `server/internal/db/sqlite.go` and any other `*.go` files that import `database/sql`:

```bash
grep -rn 'db\.\(Exec\|Query\|QueryRow\)' server/ --include='*.go' | grep -v '_test.go'
```

For EVERY query, verify:
- All user-supplied values use `?` parameter placeholders
- NO string concatenation or `fmt.Sprintf` builds query strings with user input
- Dynamic query builders (e.g., audit log filters) use parameterized conditions

Flag any query that uses string interpolation for values as **CRITICAL**.

### 2b. Command injection

Search for any use of `os/exec`, `syscall`, or shell execution:

```bash
grep -rn 'exec\.Command\|os/exec\|syscall\.' server/ --include='*.go' | grep -v '_test.go'
```

If found, verify all arguments are validated against an allowlist, never constructed from user input.

### 2c. NoSQL / LDAP injection

Check for any non-SQL data access patterns. If the project adds Redis, MongoDB, or LDAP in the future, this section applies.

### 2d. MessagePack deserialization

Scan `server/internal/protocol/` for MessagePack decode operations:

```bash
grep -rn 'Decode\|Unmarshal\|msgpack' server/ --include='*.go' | grep -v '_test.go'
```

Verify:
- Decoded messages are validated after deserialization (type, field bounds, enum values)
- Unknown message types are rejected (not silently ignored)
- Frame size is bounded before reading payload (check `MaxFrameSize` or equivalent)
- No `interface{}` deserialization that could trigger type confusion

---

## 3. Input Validation (OWASP A03 / CWE-20)

### 3a. Request body validation

For EVERY handler that accepts a request body, verify validation exists for:

| Field type | Required validation |
|------------|-------------------|
| Email | Format (regex or `mail.ParseAddress`), max length (254 per RFC 5321) |
| Password | Minimum length (8+), maximum length (72 for bcrypt), complexity rules |
| Display name | Max length, no control characters |
| Group name | Max length, no control characters, trimmed whitespace |
| UUID/ID params | Format validation (UUID v4), reject malformed |
| Hostname | Max length (253), valid characters |
| OS string | Max length, allowlist or sanitize |
| Firmware version | Max length |
| Pagination (limit) | Min 1, max bounded (e.g., 200), default value |
| Pagination (offset) | Min 0 |

Check the OpenAPI spec (`api/openapi.yaml`) for `minLength`, `maxLength`, `pattern`, `minimum`, `maximum` constraints. If missing, add them to the spec AND regenerate the server code, or add manual validation in handlers.

### 3b. Request size limits

Verify the HTTP server enforces a maximum request body size:

```go
http.MaxBytesReader(w, r.Body, maxBytes)
```

Or via middleware. If missing, add a body size limit (recommended: 1 MB for API, 10 MB for file uploads).

### 3c. Header validation

Check that:
- `Content-Type` is validated (only `application/json` accepted for API endpoints)
- `Authorization` header format is strictly validated (must be `Bearer <token>`)
- No user-controlled headers are reflected in responses (header injection)

### 3d. Path parameter validation

All path parameters (device ID, group ID, user ID, session token) MUST be validated:
- UUIDs: validate format before database lookup
- Session tokens: validate hex format and length (64 chars)
- Reject early with 400 for malformed parameters — do NOT pass to database

### 3e. Query parameter validation

All query parameters (limit, offset, filter, action) MUST be validated:
- Numeric params: parse as integer, validate range
- String params: validate against allowlist of known values
- Reject unknown query parameters (or ignore them — never pass through)

---

## 4. Authentication Security (OWASP A07)

### 4a. Password policy

Verify the registration and password change endpoints enforce:
- Minimum length: 8 characters (NIST SP 800-63B)
- Maximum length: 72 characters (bcrypt limitation — truncates silently beyond 72)
- No common/breached password check (optional but recommended)

If the minimum length is < 8 or there is no maximum length check, fix it in the handler.

### 4b. JWT security

Verify:
- Signing algorithm is explicitly checked during validation (prevent `alg: none` attack)
- Token expiration is enforced (`exp` claim)
- Token issuer is validated (`iss` claim)
- Secret key is sufficiently long (>= 32 bytes for HS256)
- Secret key is NOT hardcoded (comes from env var or flag)
- Tokens are NOT logged at any log level

### 4c. Brute-force protection

Check for rate limiting on:
- `POST /api/v1/auth/login` — limit failed attempts per IP and per email
- `POST /api/v1/auth/register` — limit registrations per IP
- `DELETE /api/v1/sessions/{token}` — limit per user

If no rate limiting exists, implement it using a middleware (e.g., `golang.org/x/time/rate` or `chi` middleware). At minimum:
- 10 login attempts per minute per IP
- 5 registration attempts per hour per IP
- Log all failed authentication attempts with IP and email

### 4d. Account enumeration

Check that login and registration responses do NOT reveal whether an email exists:
- Login failure: "invalid credentials" (NOT "user not found" or "wrong password")
- Registration: if email already exists, return generic error (NOT "email already registered")

### 4e. Session management

- Session tokens must be cryptographically random (>= 32 bytes)
- Session tokens must expire (check if there's a TTL or cleanup mechanism)
- Deleted sessions must be immediately invalidated (no caching)

---

## 5. Sensitive Data Exposure (OWASP A02)

### 5a. Response field filtering

For EVERY API response type, verify sensitive fields are excluded:

| Model field | Should be in API response? |
|-------------|--------------------------|
| User.PasswordHash | NO — must have `json:"-"` |
| User.ID | Yes (public identifier) |
| Internal database IDs (auto-increment) | No — use UUIDs |
| Session.Token | Yes (needed by client for relay) |
| Device.GroupID | Yes (needed for UI grouping) |

Check `server/internal/api/converters.go` — every `*ToAPI()` function must explicitly map only safe fields. Never use `*` or reflect-based copying.

### 5b. Error message sanitization

For EVERY error response path, verify:
- Internal errors (database failures, I/O errors) return a generic message: `"internal error"`
- Validation errors return field-specific messages WITHOUT internal details
- Stack traces are NEVER included in responses
- File paths, SQL queries, and package names are NEVER leaked

Search for `err.Error()` in response paths:

```bash
grep -rn 'err\.Error()' server/internal/api/ --include='*.go' | grep -v '_test.go'
```

Any `err.Error()` that reaches the client is a potential information leak — replace with a safe message. Log the full error server-side at ERROR level.

### 5c. Logging hygiene — sensitive data

Verify that logs (via `slog`) NEVER contain:
- Passwords (plaintext or hashed)
- Full JWT tokens
- Full session tokens (use first 8 chars only via `protocol.RedactToken`)
- Email addresses at DEBUG level (acceptable at INFO for auth events)
- Request bodies containing credentials
- Database connection strings

Search pattern:

```bash
grep -rn 'slog\.\(Info\|Debug\|Warn\|Error\)\|\.logger\.\(Info\|Debug\|Warn\|Error\)' server/ --include='*.go' | grep -v '_test.go'
```

Verify `protocol.RedactToken()` is used everywhere a session token appears in logs:

```bash
grep -rn 'token' server/internal/api/ server/internal/agentapi/ server/internal/relay/ --include='*.go' | grep -v '_test.go' | grep -i 'slog\|log\.\|logger\.'
```

Every token in a log statement that does NOT use `RedactToken` is **HIGH**.

### 5d. Security event logging

Verify these security events ARE logged. For each event, check both slog output (for Loki/Promtail ingestion) and `auditLog()` (for the persistent audit trail in `audit_events` table). Both are required for security-critical events.

| Security event | Required slog level | auditLog required? | Where to check |
|----------------|--------------------|--------------------|----------------|
| Failed login (wrong password) | WARN | Yes | `handlers_auth.go` Login handler, after `CheckPassword` failure |
| Failed login (unknown email) | WARN | No (no user ID) | `handlers_auth.go` Login handler, after `ErrNotFound` |
| Successful login | INFO | Yes | `handlers_auth.go` Login handler, success path |
| User registration | INFO | Yes | `handlers_auth.go` Register handler |
| JWT validation failure | WARN | No (no user ID) | `middleware.go` AuthMiddleware |
| Authorization denied (403) | WARN | No (optional) | `middleware.go` denyIfNotAdmin |
| Rate limit violation (429) | WARN | No | `ratelimit.go` RateLimiter |
| Password change | INFO | Yes | Check if endpoint exists |
| Admin privilege change | WARN | Yes | `handlers_users.go` UpdateUser |
| Session creation | INFO | Yes | `handlers_sessions.go` |
| Session deletion | INFO | Yes | `handlers_sessions.go` |
| Enrollment token creation | INFO | Yes | `handlers_enrollment.go` |
| Agent update push | INFO | Yes | `handlers_updates.go` |

Verification steps:

```bash
# Find all auditLog call sites — cross-reference with the table above
grep -rn 's\.auditLog(' server/internal/api/ --include='*.go' | grep -v '_test.go'

# Find auth failure paths that lack logging (return 4xx with no slog/auditLog)
grep -rn 'Login401\|Login400\|Register400' server/internal/api/ --include='*.go' | grep -v '_test.go'

# Find 403/429 response paths that lack logging
grep -rn 'StatusForbidden\|StatusTooManyRequests\|writeError.*403\|writeError.*429' server/internal/api/ --include='*.go' | grep -v '_test.go'
```

For every missing event, the fix must include:
- `slog.Warn(...)` or `slog.Info(...)` with structured fields: `"event"`, `"ip"` (via `extractIP(r)` or `r.RemoteAddr`), and context-appropriate identifiers (redacted email for auth, path for authz)
- `s.auditLog(...)` where a user ID is available (not for unauthenticated failures)
- A test that verifies the log/audit entry is written

Severity: **HIGH** for missing failed login logging (enables undetected brute force), **MEDIUM** for others.

### 5e. Log injection prevention (CWE-117)

User-controlled strings passed as slog field values can embed malicious content. While Go's `slog.JSONHandler` auto-escapes newlines and quotes (preventing classic log injection), verify the following:

Verify the server uses JSON handler (not text handler):

```bash
grep -rn 'slog\.New' server/cmd/ --include='*.go'
```

If `slog.NewJSONHandler` is used, newline injection is mitigated. If `slog.NewTextHandler` is used, flag as **HIGH** — attacker-controlled fields can forge log lines.

Verify user-supplied values in log fields do not leak secrets:

```bash
# Check that raw query strings are NOT logged (may contain tokens in ?auth= params)
grep -rn 'RawQuery\|r\.URL\.String()' server/ --include='*.go' | grep -v '_test.go' | grep -i 'slog\|log\.\|logger\.'

# Check that Authorization header is never logged
grep -rn 'Authorization' server/ --include='*.go' | grep -v '_test.go' | grep -i 'slog\|log\.\|logger\.'
```

Check that `RequestLogger` middleware in `server/internal/api/middleware.go` logs `r.URL.Path` only (not `r.URL.String()` or `r.URL.RawQuery`), since query strings may contain authentication tokens passed via `?auth=` parameters.

Severity: **MEDIUM** with JSON handler (partial mitigation), **HIGH** with text handler.

### 5f. Request correlation

Every log message within a request lifecycle must include a correlation ID so operators can trace multi-step flows through Loki.

Verify `middleware.RequestID` is registered in the middleware chain:

```bash
grep -rn 'middleware\.RequestID' server/internal/api/ --include='*.go'
```

Then check that the generated request ID is propagated to all log calls:

```bash
# RequestLogger must include request_id
grep -A10 'func RequestLogger' server/internal/api/middleware.go
# Should contain: "request_id", middleware.GetReqID(r.Context())
```

Check that `auditLog()` in `server/internal/api/api.go` does NOT use `context.Background()` — this discards the request ID. It should accept and propagate the request context (with a separate timeout for the DB write):

```bash
grep -A15 'func.*auditLog' server/internal/api/api.go
# Look for context.Background() — loses request_id
# Should use a derived context that carries correlation values but has its own deadline
```

Check that error response handlers include request_id:

```bash
grep -B2 -A5 'RequestErrorHandlerFunc\|ResponseErrorHandlerFunc' server/internal/api/api.go
# slog.Warn and slog.Error calls should include "request_id"
```

Every `slog` call in `server/internal/api/` that does NOT include `"request_id"` is a gap. Grep to find all gaps:

```bash
grep -rn 'slog\.\(Info\|Warn\|Error\)\|\.logger\.\(Info\|Warn\|Error\)' server/internal/api/ --include='*.go' | grep -v '_test.go' | grep -v 'request_id'
```

Severity: **HIGH** — without correlation, multi-request security incidents (brute force across endpoints, session hijacking attempts) cannot be traced through Loki.

Fix pattern: add `"request_id", middleware.GetReqID(r.Context())` to `RequestLogger`, error handlers, and security event logs. For `auditLog`, pass request context values (not the full context) so the write outlives the request but retains correlation.

### 5g. Log level discipline

Verify log levels match the operational severity of the event. Misclassified levels cause operators to miss important signals or drown in noise.

| Event | Required level | Rationale |
|-------|---------------|-----------|
| Successful HTTP request | INFO | Normal operation, access log |
| Request validation error (400) | WARN | May indicate attack probing |
| Authentication failure (401) | WARN | Security event |
| Authorization denied (403) | WARN | Security event |
| Rate limit hit (429) | WARN | Possible brute force |
| Internal server error (500) | ERROR | Operational failure |
| Response write failure | WARN | Client disconnect or network issue |
| Database query error | ERROR | Operational failure |
| Frame parsing detail | DEBUG | Verbose, dev-only |

Scan for misclassified levels:

```bash
# Debug-level logs that should be Warn or higher (operational events invisible in production)
grep -rn 'slog\.Debug\|\.logger\.Debug\|\.Debug(' server/internal/api/ --include='*.go' | grep -v '_test.go'

# Verify each hit: is this a developer-only diagnostic (correct at Debug) or an operational event operators need (should be Warn/Error)?
```

Known issue: `server/internal/api/middleware.go` logs response write failures at `slog.Debug` — this should be `slog.Warn` since it indicates a client disconnect or network problem that may correlate with other issues. Severity: **MEDIUM**.

### 5h. Audit trail reliability

The `auditLog()` function in `server/internal/api/api.go` uses a fire-and-forget pattern. Verify the implementation is robust:

```bash
grep -A15 'func.*auditLog' server/internal/api/api.go
```

Check for these risks:

| Risk | What to look for | Severity |
|------|-----------------|----------|
| Unbounded goroutines | `go func()` with no semaphore or channel — 1000 concurrent requests spawn 1000 goroutines | MEDIUM |
| Silent event loss | Write error is logged but event is permanently lost — no retry, no queue | MEDIUM |
| Shutdown race | On `SIGTERM`, in-flight `go func()` goroutines may be killed before completing DB writes | LOW |
| Context loss | `context.Background()` discards request correlation (request_id, authenticated user context) | MEDIUM |
| Timeout under contention | 5-second timeout with `MaxOpenConns(1)` SQLite — serial writes queue up under burst | LOW |

Verify:
- Write errors ARE logged at ERROR level (should already be the case)
- The timeout (5 seconds) is documented as acceptable for expected load
- If any of the above risks are realized in production (visible in Loki as "audit log write failed"), recommend a bounded worker pool or channel-based queue

Fix pattern: replace `go func()` with a buffered channel + single writer goroutine, OR document accepted risk with a comment explaining the trade-off.

### 5i. Rust agent logging standards

The Rust agent is part of the backend system and its logs may feed into the same Loki pipeline. Verify logging hygiene in the agent codebase.

Verify `tracing` crate is used consistently (no raw `println!`/`eprintln!` in production paths):

```bash
grep -rn 'println!\|eprintln!' agent/crates/ --include='*.rs' | grep -v 'target/' | grep -v '#\[test\]\|#\[cfg(test)\]\|mod tests'
```

Any `println!`/`eprintln!` in non-test code is **MEDIUM** — bypasses structured logging and tracing filters.

Verify no session tokens or enrollment tokens are logged in full:

```bash
grep -rn 'token\|Token' agent/crates/ --include='*.rs' | grep -v 'target/' | grep -v 'test\|Test\|_test\|mod tests' | grep -i 'info!\|warn!\|error!\|debug!'
```

If tokens appear in log macros without truncation/redaction, flag as **HIGH**. Fix pattern: add a `redact_token()` utility mirroring Go's `RedactToken()`:

```rust
/// Returns the first 8 characters of a token for safe logging.
fn redact_token(token: &str) -> String {
    if token.len() <= 8 { "***".to_string() }
    else { format!("{}...", &token[..8]) }
}
```

Check output format — text vs JSON:

```bash
grep -rn 'tracing_subscriber' agent/crates/ --include='*.rs'
```

If the agent uses `tracing_subscriber::fmt()` with default text format and runs inside Docker with Promtail JSON parsing, logs will not be parsed correctly. Flag as **MEDIUM** if containerized (recommend `tracing_subscriber::fmt().json()` for Docker deployments), **LOW** if running as a systemd service.

Verify `RUST_LOG` env-filter is configured for runtime log level control:

```bash
grep -rn 'EnvFilter\|env_filter\|RUST_LOG' agent/crates/ --include='*.rs'
```

---

## 6. Security Misconfiguration (OWASP A05)

### 6a. HTTP security headers

Check if the server sets these headers (either directly or via reverse proxy):

| Header | Required value | Purpose |
|--------|---------------|---------|
| `Strict-Transport-Security` | `max-age=63072000; includeSubDomains; preload` | Force HTTPS |
| `X-Content-Type-Options` | `nosniff` | Prevent MIME sniffing |
| `X-Frame-Options` | `DENY` | Prevent clickjacking |
| `Content-Security-Policy` | `default-src 'self'` (minimum) | Prevent XSS |
| `Referrer-Policy` | `strict-origin-when-cross-origin` | Privacy |
| `X-XSS-Protection` | `0` (CSP supersedes, disable legacy) | Legacy XSS filter |
| `Cache-Control` | `no-store` on auth endpoints | Prevent caching of sensitive data |

If headers are set by the reverse proxy (Caddy), verify the Caddyfile. If the app is ever run without a proxy, add a middleware.

### 6b. CORS configuration

If the API serves cross-origin requests (browser clients on different domains):
- Define an explicit CORS policy using `chi/cors` middleware
- Allow ONLY the production domain origin
- Allow only required methods: `GET, POST, PUT, PATCH, DELETE, OPTIONS`
- Allow only required headers: `Authorization, Content-Type`
- Set `AllowCredentials: false` (JWT uses Authorization header, not cookies)
- NEVER use `Access-Control-Allow-Origin: *` with credentials

If the API is same-origin only (served behind reverse proxy), verify no wildcard CORS headers exist.

### 6c. Server information leakage

Verify:
- HTTP `Server` header is stripped or set to a generic value (Caddy strips it with `-Server`)
- Error responses do not include framework version, Go version, or OS details
- Health endpoint does not expose internal service names, versions, or dependency status
- `/debug/pprof` and other debug endpoints are NOT registered in production

Search for debug/diagnostic registrations:

```bash
grep -rn 'pprof\|debug\|expvar' server/ --include='*.go' | grep -v '_test.go'
```

### 6d. Default credentials and configurations

Verify:
- No default JWT secret (fail startup if not set)
- No default admin password
- No default AMT password
- `.env.example` uses obviously fake values (`changeme`, etc.)
- Database uses secure defaults (WAL mode, foreign keys, busy timeout)

### 6e. TLS configuration

If the server handles TLS directly (MPS, QUIC):
- Minimum TLS version: 1.2 for MPS (AMT compatibility), 1.3 for QUIC
- Strong cipher suites only (no RC4, DES, 3DES, export ciphers)
- Certificate validation enabled (mTLS for agents)

---

## 7. Cross-Site Scripting — Backend Prevention (OWASP A03)

### 7a. Stored XSS vectors

Identify every field that stores user-generated text and is later rendered in a browser:

| Field | Stored in | Rendered where | Risk |
|-------|-----------|----------------|------|
| User.DisplayName | users table | Web UI user list | HIGH if unescaped |
| User.Email | users table | Web UI, admin panel | MEDIUM |
| Device.Hostname | devices table | Web UI device list | HIGH if unescaped |
| Device.OS | devices table | Web UI device details | MEDIUM |
| Group.Name | groups table | Web UI sidebar | HIGH if unescaped |
| Group.Description | groups table | Web UI group details | HIGH if unescaped |
| AuditEvent.Details | audit_log table | Admin audit panel | HIGH if unescaped |

### 7b. Backend sanitization

For each field above, verify at the INPUT stage (handler or database layer):
- HTML entities are escaped OR the field is validated against a strict pattern
- Control characters (U+0000 to U+001F) are stripped
- Script-injection patterns (`<script>`, `javascript:`, `on*=`) cannot pass validation

Recommended: validate with a strict regex allowlist (alphanumeric + limited punctuation) rather than trying to blocklist dangerous patterns.

### 7c. Output encoding

Verify API responses set `Content-Type: application/json` — JSON encoding by Go's `encoding/json` automatically escapes `<`, `>`, `&` in string values. Verify this is not overridden.

---

## 8. Insecure Deserialization (OWASP A08)

### 8a. JSON deserialization

Go's `encoding/json` is safe against RCE-style deserialization attacks. However, verify:
- No `json.Unmarshal` into `interface{}` or `map[string]interface{}` with user input
- Decoded structs use typed fields (not `json.RawMessage` passed to unsafe operations)
- Unknown fields are ignored (default Go behavior — acceptable)

### 8b. MessagePack deserialization

Check `server/internal/protocol/codec.go`:
- Frame size is bounded before reading (prevent memory exhaustion)
- Message type is validated after decode (reject unknown types)
- String fields have length limits enforced after decode
- Nested structures have depth limits

### 8c. WebSocket message handling

Check `server/internal/api/wsconn.go` and relay code:
- Message size limits enforced
- Binary messages are passed through without interpretation (relay model — safe)
- No deserialization of WebSocket payloads into executable structures

---

## 9. Rate Limiting and DoS Protection (OWASP A05)

### 9a. Authentication endpoints

Add rate limiting to:
- `POST /api/v1/auth/login` — 10/min per IP, 5/min per email
- `POST /api/v1/auth/register` — 5/hour per IP

### 9b. Resource-intensive endpoints

Add rate limiting to:
- `GET /api/v1/audit` — 30/min per user (large query potential)
- `POST /api/v1/sessions` — 10/min per user (creates relay sessions)
- WebSocket upgrades — 30/min per IP

### 9c. Request body size

Enforce maximum body sizes:
- API endpoints: 1 MB
- File upload endpoints: configurable (e.g., 100 MB)
- WebSocket messages: existing frame size limits (check `MaxFrameSize`)

### 9d. Pagination limits

Verify ALL list endpoints enforce:
- Maximum `limit` parameter (e.g., 200)
- Default `limit` if not specified
- Valid `offset` (>= 0)

---

## 10. Dependency and Configuration Security (OWASP A06)

### 10a. Known vulnerabilities

Run vulnerability scanners:

```bash
cd server && govulncheck ./...
cd agent && cargo audit
cd web && npm audit --audit-level=high
```

Flag any HIGH or CRITICAL vulnerabilities.

### 10b. Outdated dependencies

Check for significantly outdated dependencies:

```bash
cd server && go list -m -u all 2>/dev/null | grep '\[' | head -20
```

Flag any dependency more than 2 major versions behind.

### 10c. Unnecessary dependencies

Look for imported but unused packages, especially security-sensitive ones:
- Debug or profiling packages in production code
- Test-only packages imported in production code

---

## 11. WebSocket and Real-Time Security

### 11a. WebSocket authentication

Verify:
- Browser-side WebSocket connections require JWT authentication
- Agent-side connections are validated against the session store
- Token is validated BEFORE upgrading the connection (not after)
- Expired or invalid tokens result in connection rejection (not silent acceptance)

### 11b. WebSocket origin validation

Check `websocket.Accept` options:
- `InsecureSkipVerify` should be `false` in production
- If `true`, document why and add compensating controls (token auth)
- NEVER allow unauthenticated WebSocket connections from any origin

### 11c. Relay isolation

Verify:
- Each relay session is isolated (one browser ↔ one agent)
- A token cannot be reused after the session ends
- Relay does not buffer or log message contents
- Connection cleanup happens on disconnect (no resource leaks)

---

## 12. Summary Report

After completing all checks, print a table:

```
+----------+-------+-----------------------------------------------------------+--------+
| Section  | Count | Finding                                                   | Status |
+----------+-------+-----------------------------------------------------------+--------+
| 1 IDOR   |   0   | All endpoints verify resource ownership                   | PASS   |
| 2 SQLi   |   0   | All queries use parameterized statements                  | PASS   |
| 3 Input  |   0   | All inputs validated (type, format, length)               | PASS   |
| 4 Auth   |   0   | Password policy, JWT, brute-force, enumeration            | PASS   |
| 5 Data   |   0   | Response filtering, error sanitization, logging hygiene,  | PASS   |
|          |       | injection prevention, request correlation, log levels,    |        |
|          |       | audit trail reliability, Rust agent logging standards     |        |
| 6 Config |   0   | Headers, CORS, TLS, no debug endpoints                   | PASS   |
| 7 XSS    |   0   | User-generated fields sanitized on input                  | PASS   |
| 8 Deser  |   0   | Frame sizes bounded, types validated                      | PASS   |
| 9 DoS    |   0   | Rate limiting, body size, pagination limits               | PASS   |
| 10 Deps  |   0   | No known vulnerabilities, deps current                    | PASS   |
| 11 WS    |   0   | Auth before upgrade, origin check, relay isolation        | PASS   |
+----------+-------+-----------------------------------------------------------+--------+
```

Status values: **PASS** (no issues), **FIXED** (issues found and remediated in-place with tests), **FAIL** (issues found, cannot auto-fix — explain why and provide exact remediation steps).

If ANY section is FAIL or FIXED, list every finding with:
- File path and line number
- CWE identifier
- Severity (CRITICAL / HIGH / MEDIUM / LOW)
- Description of the vulnerability
- Proof of concept (how to exploit)
- Fix applied or recommended

### Gate criteria

The audit **FAILS** if any CRITICAL or HIGH finding remains unfixed. All fixes MUST include tests.
