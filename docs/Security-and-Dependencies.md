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
│  CI triggers catch changed code; Dependabot and audits   │
│  catch newly disclosed dependency issues through PRs     │
└─────────────────────────────────────────────────────────┘
```

### CodeQL

Static analysis for Go, TypeScript, and Rust with `security-and-quality` queries. The current [`ci.yml`](../.github/workflows/ci.yml) trigger set runs CodeQL on pushes, pull requests, and manual dispatch; it does not define a separate CodeQL schedule.

### Vulnerability Scanners

- `govulncheck` (Go) — checks against the Go vulnerability database
- `cargo audit` (Rust) — checks against RustSec advisory database
- `npm audit` (Web) — checks against the npm advisory database

### Secrets scanning

[gitleaks](https://github.com/gitleaks/gitleaks) runs against the **full git history** on every CI trigger via the `config-lint` job. Config lives in [`.gitleaks.toml`](../.gitleaks.toml); allowlists are categorical (paths/regexes), never per-fingerprint, so the gate stays meaningful as new commits land.

Local invocations:

| Where | Command | What |
|---|---|---|
| Full repo scan | `make secrets-scan` | History + working tree (mirrors CI exactly) |
| Pre-commit guard | `gitleaks protect --staged --config .gitleaks.toml` | Scans only staged hunks — the trip wire in the [`/precommit` skill](../.claude/skills/precommit/SKILL.md) step 6.1 |

Test fixtures with deliberate fake credentials (e.g. [`deploy/tests/fixtures/leaked-secret.txt`](../deploy/tests/fixtures/leaked-secret.txt)) prove the scanner's wiring without leaking real values: if the canary stops triggering, the scanner has regressed.

### Dependabot

[Dependabot](../.github/dependabot.yml) checks all four ecosystems (Go, Cargo, npm, GitHub Actions) daily. PRs target `dev` directly — same target a human contributor would use.

Updates are **grouped per ecosystem** — one PR per ecosystem rather than one per package — reducing noise.

The [auto-merge workflow](../.github/workflows/dependabot-auto-merge.yml) classifies each PR via `dependabot/fetch-metadata` and squash-merges patch + minor updates as soon as CI is green; major-version bumps stay open with a comment requesting human review. The full propagation path is:

```
dependabot/* PR → dev → (CI) → main
```

The existing `merge-to-main` job in [`ci.yml`](../.github/workflows/ci.yml) forwards `dev` → `main` after the same gate any human commit clears. No separate integration branch; no nightly sync workflow.

## Adversarial Pen-Test Gate

[ADR-027](adr/ADR-027-adversarial-pentest-precommit-gate.md) adds a fail-closed
adversarial gate that runs custom [Semgrep](https://semgrep.dev) rules plus an
OpenAPI spec-drift check over the diff. It is enforced in three places sharing
one runner ([`scripts/pentest-review.sh`](../scripts/pentest-review.sh)): the
commit hook ([`pretooluse-pentest-gate.sh`](../.claude/hooks/pretooluse-pentest-gate.sh)),
the precommit gauntlet, and a blocking `pentest-review` job in
[`ci.yml`](../.github/workflows/ci.yml) (a required `merge-to-main` check — the
only gate covering Dependabot PRs and machines without the local hooks).

Rules live in [`policy/semgrep/`](../policy/semgrep/): missing authorization on
mutating handlers, unchecked path traversal, secret-in-log, plaintext secret
columns, IDOR (advisory), and OpenAPI mutating ops shipped without a `security:`
block. HIGH findings block; MEDIUM are advisory. Diff-only scanning
(`--baseline-commit`) grandfathers pre-existing findings — only new ones block.
Semgrep is provisioned by [`scripts/install-semgrep.sh`](../scripts/install-semgrep.sh)
(pinned, idempotent). False positives are handled via per-rule `paths.exclude:`
or [`policy/semgrep/.semgrepignore`](../policy/semgrep/.semgrepignore), never
inline suppressions (banned per [`.claude/rules/sonarcloud.md`](../.claude/rules/sonarcloud.md)).
The [`/pentest-review`](../.claude/skills/pentest-review/SKILL.md) skill runs the
same check on demand.

## Supply Chain Security

Container images are signed and attested to ensure artifact integrity from build to deploy:

| Layer | Tool | What it provides |
|-------|------|-----------------|
| Image signing | Cosign (keyless, Sigstore OIDC) | Proves the image was built by the GitHub Actions workflow, not tampered with in the registry |
| SLSA provenance | `docker/build-push-action` (`provenance: true`) | SLSA Build Level 2 attestation — links image to source commit, build instructions, and builder identity |
| SBOM | `anchore/sbom-action` (SPDX JSON) + `cosign attest` | Software Bill of Materials attached as a signed attestation — enables dependency tracking and vulnerability correlation |
| Deploy-time verification | `cosign verify` in [`cd.yml`](../.github/workflows/cd.yml) | Blocks deployment before the Helm rollout starts |

See [[Container-Images#supply-chain-security]] for verification commands.

## API Security Hardening

### Access Control

**Tenant is the visibility boundary. `is_admin` is the mutation
boundary.** Every member of a tenant sees the same fleet and may act on
any device in it; only configuration and secret-bearing reads are gated on
admin. See [ADR-062](adr/ADR-062-tenant-scoped-reads-and-fleet-summary.md).

| Class | Authorization rule | Endpoints |
|---|---|---|
| Fleet reads | Tenant membership | Device list/detail/summary, metrics, correlate, history, inventory, hardware, group list/detail, session list |
| Device commands | Tenant membership | Session create/delete, restart, maintenance, AMT power |
| Configuration | Admin | Device delete, device group move, group create/delete, enrollment tokens, updates, users, security groups, tenant purge |
| Secret-bearing reads | Admin | Device logs, enrollment tokens, update signing key, audit log, user list |

Visibility is enforced in the repository, not in a handler branch: every read
runs inside a transaction that sets `app.current_tenant`, so a resource in another
tenant resolves to "not found" rather than to a forbidden response. The
mutation boundary is the `denyIfNotAdmin` helper, applied at the top of each
configuration handler.

An Intel AMT power command is the one case where the underlying resource has no
tenant of its own — the CIRA connection map is keyed by AMT UUID alone. The
handler therefore resolves the managed device behind that UUID through the
tenant-scoped repository first, so a UUID outside the caller's tenant is
refused before any command is dispatched.

Admins keep cross-tenant reads. The dashboard summary is the one
deliberate exception: it always describes the caller's own tenant, so its
tiles and health bands cover a single device set.

### Tenant Isolation

PostgreSQL Row-Level Security is enabled and forced on tenant-owned tables. JWT
tokens carry the active `tenant` claim; authenticated middleware stores it in
context, and repository methods execute inside transactions that set
`app.current_tenant` and `app.is_admin` with `SET LOCAL`. Policies permit only rows
for the current tenant, with an admin policy bypass controlled by `app.is_admin`.
The Helm runtime role is created by
[`zz-app-role.sh`](../deploy/helm/opengate/files/zz-app-role.sh) and checked by
[`cd.yml`](../.github/workflows/cd.yml) as non-superuser and non-`BYPASSRLS`, so
missing tenant context fails closed instead of leaking rows across tenants.

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

At the edge, the app Helm chart applies the same headers (plus CSP and
Permissions-Policy) controller-side through the ingress-nginx `add-headers`
ConfigMap rendered by
[`custom-headers-configmap.yaml`](../deploy/helm/opengate/templates/custom-headers-configmap.yaml)
from the values in [`values.yaml`](../deploy/helm/opengate/values.yaml). This
replaces the former per-ingress `configuration-snippet` annotation, so the
controller runs with snippet annotations disabled.

## Logging and Token Redaction

Session tokens are sensitive routing credentials. All log and audit entries redact tokens to their first 8 characters (e.g., `abcdef12...`) using the `redactToken()` helper. This applies to:

- Agent connection logs (`agentapi/conn.go`) — session accept/reject
- Relay handler logs (`api/handlers_relay.go`) — registration and peer wait errors
- Audit logs (`api/handlers_sessions.go`) — session deletion events

Kubernetes deploys create or reuse Secrets via [`cd.yml`](../.github/workflows/cd.yml).

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
| `jackc/pgx/v5` | Pure-Go PostgreSQL driver (stdlib adapter used by `database/sql`) |
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
