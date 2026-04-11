# Security and Dependencies

## Security Scanning

Three layers of automated security analysis run on every CI trigger:

```
┌─────────────────────────────────────────────────────────┐
│                    Security Pipeline                     │
├───────────────────┬──────────────────┬──────────────────┤
│     CodeQL        │  Vuln Scanners   │   Dependabot     │
│                   │                  │                  │
│  Go               │  govulncheck     │  Daily updates   │
│  TypeScript       │  cargo audit     │  Auto-merge      │
│  Rust             │  npm audit       │  after CI pass   │
│                   │                  │                  │
│  security-and-    │  Known CVE       │  Go, Cargo,      │
│  quality queries  │  databases       │  npm, Actions    │
├───────────────────┴──────────────────┴──────────────────┤
│  Daily schedule (0 6 * * *) catches newly disclosed     │
│  patterns even without code changes                     │
└─────────────────────────────────────────────────────────┘
```

### CodeQL

Static analysis for Go, TypeScript, and Rust with `security-and-quality` queries. Also runs on a daily schedule to catch newly disclosed patterns.

### Vulnerability Scanners

- `govulncheck` (Go) — checks against the Go vulnerability database
- `cargo audit` (Rust) — checks against RustSec advisory database
- `npm audit` (Web) — checks against the npm advisory database

### Dependabot

[Dependabot](../../.github/dependabot.yml) checks all four ecosystems (Go, Cargo, npm, GitHub Actions) daily. PRs target the `dependabot-dev` integration branch (not `dev` directly) to isolate dependency updates from feature development.

Updates are **grouped per ecosystem** — one PR per ecosystem rather than one per package — reducing noise.

A companion [auto-merge workflow](../../.github/workflows/dependabot-auto-merge.yml) approves and squash-merges Dependabot PRs automatically once CI passes. The full propagation path is:

```
dependabot/* PR → dependabot-dev → (CI) → main ──► dev (auto-sync)
```

The `merge-to-main` job merges `dependabot-dev` directly into `main` (same gate as `dev`), then immediately syncs `main` back into `dev`. A nightly `sync-from-main` workflow (04:00 UTC) also syncs `main` → both `dev` and `dependabot-dev` as a safety net.

## Supply Chain Security

Container images are signed and attested to ensure artifact integrity from build to deploy:

| Layer | Tool | What it provides |
|-------|------|-----------------|
| Image signing | Cosign (keyless, Sigstore OIDC) | Proves the image was built by the GitHub Actions workflow, not tampered with in the registry |
| SLSA provenance | `docker/build-push-action` (`provenance: true`) | SLSA Build Level 2 attestation — links image to source commit, build instructions, and builder identity |
| SBOM | `anchore/sbom-action` (SPDX JSON) + `cosign attest` | Software Bill of Materials attached as a signed attestation — enables dependency tracking and vulnerability correlation |
| Deploy-time verification | `cosign verify` in CD workflow + VPS `redeploy()` | Blocks deployment of unsigned or tampered images at both the CI and host level |

See [[Container-Images#supply-chain-security]] for verification commands.

## API Security Hardening

### Access Control (IDOR Prevention)

All resource endpoints enforce ownership checks before granting access:

| Resource | Authorization rule |
|----------|-------------------|
| Device | User must own the device's group, or be admin |
| Group | User must own the group, or be admin |
| Session | User must have created the session, or be admin |
| AMT devices | Admin only |
| Enrollment tokens | Admin only |
| Audit log | Admin only |
| User management | Admin only (self-read allowed) |

The `isGroupOwner()` helper resolves ownership by querying `device → group → group.owner_id` and comparing against the authenticated user. Admins bypass all ownership checks.

Non-admin users listing devices without a `group_id` filter receive only devices belonging to their own groups via `ListDevicesForOwner()`.

### Rate Limiting

Per-IP rate limiting is enforced at the middleware level:

| Scope | Rate | Burst |
|-------|------|-------|
| Global (all routes) | 100 req/s | 200 |
| Auth endpoints (`/auth/login`, `/auth/register`) | 10 req/s | 20 |

Rate limits are tracked per client IP. When behind a reverse proxy, the real IP is extracted from the `X-Forwarded-For` header.

### Request Timeout

All API routes have a 30-second request timeout enforced by `RequestTimeout` middleware. WebSocket relay routes are excluded from the timeout to allow long-lived connections.

### Input Validation

- **Password length**: 8–72 characters enforced at registration (72 is the bcrypt truncation limit)
- **Email format**: Validated via `net/mail.ParseAddress` at registration — rejects malformed addresses
- **Request body size**: 1 MB limit via `MaxBodySize` middleware on all API endpoints
- **Duplicate email**: Registration checks for existing email before insert, returns generic "registration failed" to prevent account enumeration
- **CSR signature**: Agent CSR signatures are verified (`x509.ParseCertificateRequest` + `CheckSignature`) before the CA signs them, preventing submission of forged CSRs
- **JWT secret length**: Minimum 32 characters enforced at server startup — the server refuses to start with a weak secret

### Error Sanitization

All API error responses return generic messages. Internal error details (SQL errors, stack traces, file paths) are logged server-side but never exposed to clients. Custom error handlers override the oapi-codegen defaults.

### Security Headers

The API server adds defense-in-depth headers via `SecurityHeaders` middleware:

- `Strict-Transport-Security: max-age=63072000; includeSubDomains; preload`
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: strict-origin-when-cross-origin`

Caddy adds additional headers in production:

- `Strict-Transport-Security: max-age=63072000; includeSubDomains; preload`
- `Content-Security-Policy: default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self'; connect-src 'self' wss:; frame-ancestors 'none'`
- `Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=()`
- `-Server` (strip server identity)

## Logging and Token Redaction

Session tokens are sensitive routing credentials. All log and audit entries redact tokens to their first 8 characters (e.g., `abcdef12...`) using the `redactToken()` helper. This applies to:

- Agent connection logs (`agentapi/conn.go`) — session accept/reject
- Relay handler logs (`api/handlers_relay.go`) — registration and peer wait errors
- Audit logs (`api/handlers_sessions.go`) — session deletion events

Deploy scripts use `set_env_var()` with atomic grep+mv (not sed) to avoid regex injection when updating `.env` files on the VPS.

## Certificate Hierarchy

```
OpenGate CA (ECDSA P-256, self-signed, 10yr)
├── Server cert (ECDSA P-256, 1yr) — QUIC mTLS for agent connections
├── Agent certs (ECDSA P-256, 1yr) — mTLS client certificates (CSR-signed by CA)
└── MPS cert (RSA 2048, 1yr) — Intel AMT CIRA connections (TLS 1.2+)
```

### Agent Certificate Lifecycle (CSR-Based Enrollment)

On first boot, the agent obtains a CA-signed certificate via CSR-based enrollment:

1. **Key generation** — Agent generates an ECDSA P-256 key pair and saves `device_id.txt` + `agent.key` to its data directory
2. **CSR creation** — Agent creates a PKCS#10 CSR with the device UUID as Common Name (CN)
3. **Enrollment POST** — Agent sends the PEM-encoded CSR to `POST /api/v1/enroll/{token}` with `{"csr_pem": "..."}`
4. **Server signing** — Server validates the enrollment token (expiry, usage limits), parses the CSR, verifies its signature, and signs it with the CA key (`cert.Manager.SignAgentCSR()`)
5. **Certificate storage** — Agent receives the signed cert PEM + CA PEM in the response, saves the CA cert and the DER-decoded agent cert to disk
6. **mTLS connection** — Agent uses the CA-signed cert for QUIC mTLS; server verifies it against its CA trust pool

On subsequent restarts, the agent loads its saved identity (`device_id.txt`, `agent.crt`, `agent.key`) and skips enrollment. The `--enroll-url` and `--enroll-token` CLI flags (or `OPENGATE_ENROLL_URL`/`OPENGATE_ENROLL_TOKEN` env vars) provide enrollment parameters.

The MPS certificate uses RSA 2048 because Intel AMT firmware does not support ECDSA. Despite using a different key algorithm, it is signed by the same ECDSA CA. The MPS TLS listener allows TLS 1.2+ (vs TLS 1.3 for QUIC) to support AMT 11.0+ firmware (2015+).

## Key Dependencies

### Go (Server)

| Dependency | Purpose |
|-----------|---------|
| `chi/v5` | HTTP router |
| `golang-jwt/v5` | JWT authentication |
| `golang-migrate/v4` | Database migrations |
| `quic-go` v0.59 | QUIC transport for agent connections |
| `modernc.org/sqlite` | Pure-Go SQLite driver (no CGO) |
| `vmihailenco/msgpack/v5` | MessagePack codec |

### Rust (Agent)

| Dependency | Purpose |
|-----------|---------|
| `quinn` 0.11 | QUIC transport |
| `rustls` 0.23 | TLS implementation |
| `rcgen` 0.14 | Certificate and CSR generation |
| `reqwest` 0.12 | HTTP client for CSR enrollment |
| `pem` 3 | PEM parsing for enrollment response |
| `rmp-serde` 1 | MessagePack codec |
| `tokio` 1 | Async runtime |
| `async-trait` 0.1 | Object-safe async traits (`ScreenCapture`) |
| `criterion` 0.8 | Benchmarking (dev) |

### Web (Frontend)

| Dependency | Purpose |
|-----------|---------|
| React 19 | UI framework |
| Zustand | State management |
| Tailwind CSS 4 | Styling |
