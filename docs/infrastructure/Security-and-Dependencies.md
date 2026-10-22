# Security and Dependencies

- [Scanning](#scanning)
  - [CodeQL](#codeql)
  - [Vulnerability scanners](#vulnerability-scanners)
  - [Secrets](#secrets)
  - [Dependabot](#dependabot)
- [Pen-test gate](#pen-test-gate)
- [Supply chain](#supply-chain)
- [API hardening](#api-hardening)
  - [Access control](#access-control)
  - [Console sign-in](#console-sign-in)
  - [Tenant isolation](#tenant-isolation)
  - [Rate limiting](#rate-limiting)
  - [Request timeout](#request-timeout)
  - [Input validation](#input-validation)
  - [Error messages](#error-messages)
  - [Security headers](#security-headers)
- [Tokens in logs](#tokens-in-logs)
- [Certificates](#certificates)
  - [Agent certificates](#agent-certificates)
  - [Who can read the master key](#who-can-read-the-master-key)
- [Main dependencies](#main-dependencies)
  - [Server (Go)](#server-go)
  - [Agent (Rust)](#agent-rust)
  - [Web client (TypeScript)](#web-client-typescript)

## Scanning

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

Static analysis for Go, TypeScript, and Rust with `security-and-quality` queries. The current [`ci.yml`](../../.github/workflows/ci.yml) trigger set runs CodeQL on pushes, pull requests, and manual dispatch; it does not define a separate CodeQL schedule.

### Vulnerability scanners

- `govulncheck` (Go) — checks against the Go vulnerability database.
  [`govulncheck-scan.sh`](../../scripts/govulncheck-scan.sh) fetches the
  database once, retrying only the fetch, and scans once against that copy —
  the same script in CI and in the gauntlet.
- `cargo audit` (Rust) — checks against RustSec advisory database
- `npm audit` (Web) — checks each lockfile against the npm advisory database
  through [`npm-audit.sh`](../../scripts/npm-audit.sh), the same script in CI
  and in the gauntlet. It fails on any high or critical advisory except one
  named in
  [`npm-audit-exceptions.json`](../../scripts/lib/npm-audit-exceptions.json),
  which holds only advisories with no fixed release. An exception carries a
  reason and a review date, and fails the audit once that date passes, once
  its package publishes another release, or once the audit stops reporting it
  ([ADR-126](../adr/ADR-126-advisory-exceptions-lapse.md)).

### Secrets

[gitleaks](https://github.com/gitleaks/gitleaks) runs against the **full git history** on every CI trigger via the `config-lint` job. Config lives in [`.gitleaks.toml`](../../.gitleaks.toml); allowlists are categorical (paths/regexes), never per-fingerprint, so the gate stays meaningful as new commits land.

Local invocations:

| Where | Command | What |
|---|---|---|
| Full repo scan | `make secrets-scan` | History + working tree (mirrors CI exactly) |
| Pre-commit guard | `gitleaks protect --staged --config .gitleaks.toml` | Scans only staged hunks — the trip wire in the [precommit gauntlet](../../scripts/precommit-gauntlet.sh) |

Test fixtures with deliberate fake credentials (e.g. [`deploy/tests/fixtures/leaked-secret.txt`](../../deploy/tests/fixtures/leaked-secret.txt)) prove the scanner's wiring without leaking real values: if the canary stops triggering, the scanner has regressed.

### Dependabot

[Dependabot](../../.github/dependabot.yml) checks all four ecosystems (Go, Cargo, npm, GitHub Actions) daily. PRs target `dev` directly — same target a human contributor would use.

Updates are **grouped per ecosystem** — one PR per ecosystem rather than one per package — reducing noise.

The [auto-merge workflow](../../.github/workflows/dependabot-auto-merge.yml) classifies each PR via `dependabot/fetch-metadata` and squash-merges patch + minor updates as soon as CI is green; major-version bumps stay open with a comment requesting human review. The full propagation path is:

```
dependabot/* PR → dev → (CI) → main
```

The existing `merge-to-main` job in [`ci.yml`](../../.github/workflows/ci.yml) forwards `dev` → `main` after the same gate any human commit clears. No separate integration branch; no nightly sync workflow.

## Pen-test gate

[ADR-027](../adr/ADR-027-pentest-gate.md) adds a fail-closed
adversarial gate that runs custom [Semgrep](https://semgrep.dev) rules plus an
OpenAPI spec-drift check over the diff. It is enforced in three places sharing
one runner ([`scripts/pentest-review.sh`](../../scripts/pentest-review.sh)): the
commit hook ([`pretooluse-pentest-gate.sh`](../../.claude/hooks/pretooluse-pentest-gate.sh)),
the precommit gauntlet, and a blocking `pentest-review` job in
[`ci.yml`](../../.github/workflows/ci.yml) (a required `merge-to-main` check — the
only gate covering Dependabot PRs and machines without the local hooks).

Rules live in [`policy/semgrep/`](../../policy/semgrep): missing authorization on
mutating handlers, unchecked path traversal, secret-in-log, plaintext secret
columns, IDOR (advisory), and OpenAPI mutating ops shipped without a `security:`
block. HIGH findings block; MEDIUM are advisory. Diff-only scanning
(`--baseline-commit`) grandfathers pre-existing findings — only new ones block.
Semgrep is provisioned by [`scripts/install-semgrep.sh`](../../scripts/install-semgrep.sh)
(pinned, idempotent). False positives are handled via per-rule `paths.exclude:`
or [`policy/semgrep/.semgrepignore`](../../policy/semgrep/.semgrepignore), never
inline suppressions (banned per [`.claude/rules/sonarcloud.md`](../../.claude/rules/sonarcloud.md)).
The [`/pentest-review`](../../.claude/skills/pentest-review/SKILL.md) skill runs the
same check on demand.

## Supply chain

Container images are signed and attested to ensure artifact integrity from build to deploy:

| Layer | Tool | What it provides |
|-------|------|-----------------|
| Image signing | Cosign (keyless, Sigstore OIDC) | Proves the image was built by the GitHub Actions workflow, not tampered with in the registry |
| SLSA provenance | `docker/build-push-action` (`provenance: true`) | SLSA Build Level 2 attestation — links image to source commit, build instructions, and builder identity |
| SBOM | `anchore/sbom-action` (SPDX JSON) + `cosign attest` | Software Bill of Materials attached as a signed attestation — enables dependency tracking and vulnerability correlation |
| Deploy-time verification | `cosign verify` in [`cd.yml`](../../.github/workflows/cd.yml) | Blocks deployment before the Helm rollout starts |

See [[Container-Images#supply-chain-security]] for verification commands.

## API hardening

### Access control

**Tenant is the visibility boundary. `is_admin` is the mutation
boundary.** Every member of a tenant sees the same fleet and may act on
any device in it; only configuration and secret-bearing reads are gated on
admin. See [ADR-064](../adr/ADR-064-tenancy.md).

| Class | Authorization rule | Endpoints |
|---|---|---|
| Fleet reads | Tenant membership | Device list/detail/summary, metrics, history, inventory, hardware, group list/detail, session list |
| Device commands | Tenant membership | Session create, restart, maintenance, AMT power |
| A session | Its holder, or an admin | Session delete, ending a device's session; reporting how its direct link ended (holder only) |
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

### Console sign-in

The console's sign-in is a server-side session in an `HttpOnly`,
`SameSite=Strict` cookie, which no page script can read; every request that
changes something carries the session's synchronizer value in `X-CSRF-Token`,
held in page memory and never in a cookie, and a bearer request needs none. The
session ends after 30 minutes idle or 8 hours in all, and sign-out deletes it
([`session_cookie.go`](../../server/internal/api/session_cookie.go)). The relay
upgrade takes the cookie or a bearer header, never a credential in its address,
and refuses an `Origin` other than the console's own. Open live sessions are
re-checked on an interval and closed once their sign-in has ended.

### Tenant isolation

PostgreSQL Row-Level Security is enabled and forced on tenant-owned tables. A
JWT or a console session carries the active tenant; authenticated middleware stores it in
context, and repository methods execute inside transactions that set
`app.current_tenant` and `app.is_admin` with `SET LOCAL`. Policies permit only rows
for the current tenant, with an admin policy bypass controlled by `app.is_admin`.
The Helm runtime role is created by
[`zz-app-role.sh`](../../deploy/helm/opengate/files/zz-app-role.sh) and checked by
[`cd.yml`](../../.github/workflows/cd.yml) as non-superuser and non-`BYPASSRLS`, so
missing tenant context fails closed instead of leaking rows across tenants.

### Rate limiting

Rate limiting is enforced at the middleware level
([`ratelimit.go`](../../server/internal/api/ratelimit.go)):

| Scope | Counted under | Rate | Burst |
|-------|---------------|------|-------|
| Global (all routes) | The account a valid bearer token names; the client address otherwise | 100 req/s | 200 |
| Auth endpoints (`/auth/login`, `/auth/register`) | The client address | 10 req/s | 20 |

A signed-in request is counted under its account, so forty technicians behind
one office connection each keep their own allowance; a request with no token, or
a token that fails validation, is counted under its address. A request bucket is
an identity, so whoever can choose the address a request is counted under can
mint a fresh allowance per request — which makes *whose* `X-Forwarded-For` is
believed part of the limit rather than a detail of it.

It is believed only from a peer the deployment has named as one of its proxies,
in `server.trustedProxies`
([values.yaml](../../deploy/helm/opengate/values.yaml)). An entry is either a
Kubernetes service, written `<service>.<namespace>` and matched against what the
cluster's own resolver says the peer answers for, or a range. A service is named
rather than an address because the address belongs to the controller's pod and
changes whenever that pod is replaced. Nothing named means nothing believed,
which is the right answer for a server reached directly.

Only the header's *last* entry is read, because that is the one the proxy
appended and every earlier one was supplied by the caller. See
[ADR-116](../adr/ADR-116-forwarded-addresses.md).

### Request timeout

All API routes have a 30-second request timeout enforced by `RequestTimeout` middleware. WebSocket relay routes are excluded from the timeout to allow long-lived connections.

### Input validation

- **Password length**: 8–72 characters enforced at registration (72 is the bcrypt truncation limit)
- **Email format**: Validated via `net/mail.ParseAddress` at registration — rejects malformed addresses
- **Request body size**: 1 MB limit via `MaxBodySize` middleware on all API endpoints
- **Duplicate email**: Registration checks for existing email before insert, returns generic "registration failed" to prevent account enumeration
- **CSR signature**: Agent CSR signatures are verified (`x509.ParseCertificateRequest` + `CheckSignature`) before the CA signs them, preventing submission of forged CSRs
- **JWT secret length**: Minimum 32 characters enforced at server startup — the server refuses to start with a weak secret

### Error messages

All API error responses return generic messages. Internal error details (SQL errors, stack traces, file paths) are logged server-side but never exposed to clients. Custom error handlers override the oapi-codegen defaults.

### Security headers

The API server sets every security header on every response, through the
`SecurityHeaders` middleware in
[`middleware.go`](../../server/internal/api/middleware.go): Strict-Transport-Security,
X-Content-Type-Options, X-Frame-Options, Referrer-Policy, a Content-Security-Policy
that allows scripts from the page's own origin alone, and a Permissions-Policy
denying the device APIs the console never uses. The server is the one source of
these headers, so they reach the browser the same way in every environment.

The ingress controller adds no header and runs with snippet annotations off, so
no Ingress in either environment can carry nginx configuration into a controller
whose account lists Secrets cluster-wide. Its settings live in
[`deploy/ingress-nginx/values.yaml`](../../deploy/ingress-nginx/values.yaml); the
approval-gated cluster job applies them and the nightly configuration job reads them back
([ADR-030](../adr/ADR-030-kubernetes-on-oke.md)).

## Tokens in logs

Session tokens are sensitive routing credentials. All log and audit entries redact tokens to their first 8 characters (e.g., `abcdef12...`) using the `redactToken()` helper. This applies to:

- Agent connection logs (`agentapi/conn.go`) — session accept/reject
- Relay handler logs (`api/handlers_relay.go`) — registration and peer wait errors
- Audit logs (`api/handlers_sessions.go`) — session deletion events

Kubernetes deploys create or reuse Secrets via [`cd.yml`](../../.github/workflows/cd.yml).

## Certificates

```
OpenGate master key (ECDSA P-256, self-signed, 10yr)
├── Server cert (ECDSA P-256, 90d, re-signed before expiry) — QUIC mTLS for agent connections
├── Agent certs (ECDSA P-256, 90d, renewed from day 60) — mTLS client certificates
└── MPS cert (RSA 2048, 90d, re-signed before expiry) — Intel AMT CIRA connections (TLS 1.2+)
```

No leaf outlives the master key that signed it. Lifetimes are the constants in
[`cert_issue.go`](../../server/internal/cert/cert_issue.go) and
[`cert_ca.go`](../../server/internal/cert/cert_ca.go); renewal, expired admission
and replacing the master key are [ADR-128](../adr/ADR-128-machine-identity-lifecycle.md).

### Agent certificates

The agent enrolls with the join code from the install note the installer leaves,
readable by root alone; the code is never written into the service and the agent
takes no flag for it.

1. **Key generation** — Agent generates an ECDSA P-256 key pair in memory
2. **CSR creation** — Agent creates a PKCS#10 CSR; the server ignores its subject
3. **Enrollment POST** — Agent sends the PEM-encoded CSR to `POST /api/v1/enroll/{token}` with `{"csr_pem": "..."}`
4. **Server signing** — Server validates the enrollment token (expiry, usage limits), verifies the CSR's signature and P-256 key, chooses a device number no device, deleted device or certificate has held, issues a certificate for it from a fresh template, and records it in the certificate ledger before sending it ([`issue_device.go`](../../server/internal/cert/issue_device.go), [`issuer.go`](../../server/internal/devicecert/issuer.go))
5. **Certificate storage** — Agent checks that the certificate carries the returned number and chains to the note's master keys, then saves the identity through its crash-safe store and deletes the note in the same commit
6. **mTLS connection** — Agent presents the certificate over QUIC; the server classifies it in `VerifyConnection` and admits only certificates its ledger lists

On subsequent restarts, the agent loads its saved identity and skips enrollment.
A join code therefore cannot obtain a certificate for an existing machine, and a
certificate signed by the master key that the server never issued is refused, so
a leaked master key alone mints nothing a server will admit.

The MPS certificate uses RSA 2048 because Intel AMT firmware does not support ECDSA. Despite using a different key algorithm, it is signed by an ECDSA master key — the MPS role file, which defaults to the issuing master key. The MPS TLS listener allows TLS 1.2+ (vs TLS 1.3 for QUIC) to support AMT 11.0+ firmware (2015+).

### Who can read the master key

Only the server. The master key's role files live in the `opengate-authority`
secret, copied at start into a memory volume readable by the server's user
alone; a cluster-wide admission rule refuses any other pod that mounts or reads
that secret; every step that writes it runs as a job inside the cluster that
prints no key; and the workflows sign in as a login that reads no secret at all
([Kubernetes](./Kubernetes.md#the-master-key)). What can still reach it — a change
to the server's own deployment, a pod mounting the node's filesystem, and the
ingress controller's cluster-wide read of secrets — is stated in
[ADR-128](../adr/ADR-128-machine-identity-lifecycle.md).

## Main dependencies

### Server (Go)

| Dependency | Purpose |
|-----------|---------|
| `chi/v5` | HTTP router |
| `golang-jwt/v5` | JWT authentication |
| `golang-migrate/v4` | Database migrations |
| `quic-go` v0.59 | QUIC transport for agent connections |
| `jackc/pgx/v5` | Pure-Go PostgreSQL driver (stdlib adapter used by `database/sql`) |
| `vmihailenco/msgpack/v5` | MessagePack codec |

### Agent (Rust)

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

### Web client (TypeScript)

| Dependency | Purpose |
|-----------|---------|
| React 19 | UI framework |
| Zustand | State management |
| Tailwind CSS 4 | Styling |
