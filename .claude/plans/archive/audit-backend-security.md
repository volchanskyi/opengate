# Audit Plan — Backend Security Sweep (Go server + Rust agent)

**Skill:** `/backend-audit` (run in diagnostic/plan mode — no in-place fixes applied).
**Branch:** `dev`. **Owner:** engineer (Go + Rust). **TDD:** every fix below ships a failing test first.
**Date:** 2026-06-27. **Status:** Ready for review.

## Scope & method

Ran the `/backend-audit` OWASP checklist read-only over `server/` and
`agent/crates/`. Findings are de-duplicated against the existing gates that
already cover whole categories: the ADR-027 Semgrep pen-test gate (IDOR, auth
bypass, token-in-log, path traversal, plaintext secrets), `gosec` (`make
taint-go`), `govulncheck`/`cargo audit`, and the protocol fuzz/property suites.
A category already gated is recorded as clean, not re-reported.

## Confirmed clean (evidence)

- **IDOR / broken access control (§1):** device handlers enforce ownership
  pervasively — 403 paths at
  [`handlers_devices.go`](../../../server/internal/api/handlers_devices.go) lines
  22/55/88/167/184/223, list via `ListForOwner(ContextUserID(ctx))` with an
  `isAdmin(ctx)` branch. Backed by the ADR-027 IDOR Semgrep rule.
- **Injection (§2):** no `err.Error()` leak (see error handlers below); SQL is
  parameterized (gosec G201/G202 in `make taint-go` + the Semgrep gate); SPA file
  serving uses `os.OpenRoot` (path-injection-safe,
  [`api.go:288`](../../../server/internal/api/api.go#L288)).
- **Auth — enumeration (§5d):** login returns a single `"invalid credentials"`
  for both unknown-email and wrong-password
  ([`handlers_auth.go:92,98`](../../../server/internal/api/handlers_auth.go#L92));
  register returns generic `"registration failed"` on duplicate email
  ([`handlers_auth.go:53`](../../../server/internal/api/handlers_auth.go#L53)).
- **Auth — JWT alg + secret (§5b):** `ValidateToken` rejects non-HMAC signing
  ([`auth.go:68`](../../../server/internal/auth/auth.go#L68)); startup fails closed
  if the secret is unset or `< 32` bytes
  ([`main.go:55,58`](../../../server/cmd/meshserver/main.go#L55)).
- **Rate limiting + DoS (§5c/§10):** global `RateLimiter(100,200)` +
  `AuthRateLimiter(10,20)`, `RequestTimeout(30s)`, `http.MaxBytesReader` body cap
  ([`api.go:269-276`](../../../server/internal/api/api.go#L269),
  [`middleware.go:134`](../../../server/internal/api/middleware.go#L134)).
- **Error sanitization + correlation (§6b/§6f):** the oapi
  `RequestErrorHandlerFunc`/`ResponseErrorHandlerFunc`/`ErrorHandlerFunc` log the
  full error server-side and return generic `"invalid request"`/`"internal error"`
  ([`api.go:256-282`](../../../server/internal/api/api.go#L256));
  `middleware.RequestID` + app-level `SecurityHeaders` are wired
  ([`api.go:234,238`](../../../server/internal/api/api.go#L234)).
- **Misconfig (§7):** no `pprof`/`expvar`/`/debug`; no CORS wildcard (same-origin).

## Findings

| # | Sev | Finding | Location | CI-caught? |
|---|-----|---------|----------|-----------|
| 1 | **HIGH** | Server `RequestLogger` logs `r.URL.Path` unconditionally and implements `Hijack()` so WS upgrades flow through it — every `/ws/relay/{token}` connection logs the **full relay token** in plaintext (ships to Loki). | [`middleware.go:161`](../../../server/internal/api/middleware.go#L161) + route [`api.go:286`](../../../server/internal/api/api.go#L286) | No (Semgrep matches token *vars*, not a path-param secret) |
| 2 | **HIGH** | Rust agent logs the **same relay token** unredacted at INFO (`token = %self.token.as_str()`). No `redact_token` helper exists agent-side. | [`session/mod.rs:76`](../../../agent/crates/mesh-agent-core/src/session/mod.rs#L76),81,136; [`connection.rs:150`](../../../agent/crates/mesh-agent-core/src/connection.rs#L150) | No (Rust; gosec/Semgrep are Go-only) |
| 3 | MEDIUM | `ValidateToken` never checks the `iss` claim (set at mint, [`auth.go:50`](../../../server/internal/auth/auth.go#L50)) and does not pin `ValidMethods`. Alg-confusion is already blocked by the keyfunc HMAC check — defense-in-depth. | [`auth.go:65-77`](../../../server/internal/auth/auth.go#L65) | No |
| 4 | LOW | `eprintln!` in agent production path bypasses structured `tracing` (acceptable only if it runs before subscriber init — verify). | [`mesh-agent/src/main.rs:276`](../../../agent/crates/mesh-agent/src/main.rs#L276) | No |
| 5 | LOW | Login throttle is per-IP only; no per-email failed-login counter (skill §5c). | [`ratelimit.go`](../../../server/internal/api/ratelimit.go) | No |

> F1 + F2 are the **same secret leaked on both ends** of the relay handshake —
> fix them together as a single "relay-token redaction" change.

## Remediation plan

### Phase A — relay-token redaction (HIGH; ~1 day, TDD)

1. **F1 (server):** in `RequestLogger`, redact the token segment for
   `/ws/relay/` paths before logging (log `/ws/relay/<redacted>`, reusing
   `protocol.RedactToken` on the last segment). Write the test first:
   `RequestLogger` over a relay request asserts the logged `path` field does not
   contain the full token. *(Done-when: no Loki log line carries a full relay
   token.)*
2. **F2 (agent):** add `redact_token(&str) -> String` (first 8 chars + `…`,
   `"***"` if `len <= 8`) mirroring Go's `protocol.RedactToken`; unit-test it
   first. Replace `%self.token.as_str()` / `%token.as_str()` at all four sites.
   *(Done-when: `grep -rn 'token = %' agent/crates` shows only redacted forms.)*

### Phase B — JWT hardening (MEDIUM; ~2 hrs, TDD)

3. **F3:** add `jwt.WithIssuer(c.Issuer)` + `jwt.WithValidMethods([]string{"HS256"})`
   to `ParseWithClaims`. Tests: wrong issuer → rejected; non-HS256 → rejected;
   valid → accepted (negative cases fail before, pass after).

### Phase C — log hygiene + throttle polish (LOW; ~half day)

4. **F4:** confirm whether `main.rs:276` runs before the `tracing` subscriber is
   installed; if mid-session, convert to `tracing::error!`, else annotate.
5. **F5:** add a per-email failed-login counter (reset on success) to the auth
   limiter; test that N failures for one email trip the limit across IPs.

## File inventory

**Create:** an agent util module for `redact_token` + its test.
**Modify:** `server/internal/api/middleware.go`,
`server/internal/api/middleware_test.go`,
`agent/crates/mesh-agent-core/src/session/mod.rs`,
`agent/crates/mesh-agent-core/src/connection.rs`,
`agent/crates/mesh-agent/src/main.rs`, `server/internal/auth/auth.go`,
`server/internal/auth/auth_test.go`, `server/internal/api/ratelimit.go`,
`server/internal/api/ratelimit_test.go`.

## Acceptance criteria

1. No relay token is logged in full on either end (server path log + agent
   tracing), covered by tests on both sides.
2. `ValidateToken` rejects wrong-issuer and non-HS256 tokens (tested).
3. `make taint-go`, `cargo test`, `go test ./...`, and the gauntlet stay green.

## Reviewer checklist

- [ ] `RequestLogger` redacts the relay token segment; test asserts it.
- [ ] All four agent token-log sites use the redactor; `redact_token` matches Go semantics.
- [ ] JWT issuer + ValidMethods enforced with negative tests.
- [ ] `eprintln!` decision documented (pre-init vs converted).
- [ ] Per-email throttle does not regress the per-IP limiter behavior.
