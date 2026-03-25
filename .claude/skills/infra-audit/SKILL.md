---
name: infra-audit
description: |
  Audit all infrastructure, deploy configs, CI/CD workflows, and application code
  for hardcoded secrets, credentials, PII, insecure endpoints, and missing
  sensitivity annotations. Fixes issues in-place and reports findings.
---

# Infrastructure Security Audit

Systematically audit every file that touches secrets, credentials, infrastructure keys, endpoints, or PII. Fix every issue you find in-place. Report a summary at the end.

**Severity levels:** CRITICAL (credential exposure), HIGH (missing sensitivity guard), MEDIUM (best-practice gap), LOW (informational).

---

## 1. Git-tracked secrets scan

Check that NO sensitive files are tracked in the git index. Run:

```bash
git ls-files -- '*.tfvars' '*.tfstate' '*.tfstate.backup' 'tfplan' '**/.env' '**/.env.*' '!**/.env.example' '*.pem' '*.key' '*.p12' '*.jks' '*.keystore'
```

If ANY file appears, it is **CRITICAL** — remove it from tracking with `git rm --cached` and verify the corresponding `.gitignore` rule exists.

Also verify these `.gitignore` rules exist (root and `deploy/terraform/`):

| Pattern | Protects |
|---------|----------|
| `.env` / `.env.*` (except `.env.example`) | Runtime secrets |
| `*.tfstate` / `*.tfstate.backup` | Terraform state (contains resource attributes) |
| `terraform.tfvars` / `*.auto.tfvars` | Terraform variable values |
| `*.pem` / `*.key` / `ca.crt` / `ca.key` | Certificates and private keys |
| `*.db` / `*.db-journal` / `*.db-wal` | Database files |
| `tfplan` | Terraform plan output (may contain secrets) |
| `crash.log*` | Terraform crash logs (may contain provider output) |

---

## 2. Terraform sensitivity audit

### 2a. Variables (`deploy/terraform/variables.tf`)

Every variable that holds an OCID, key, path to a key, fingerprint, IP/CIDR, password, token, or secret **MUST** have `sensitive = true`. Audit each variable and fix any that are missing it.

Known sensitive variables (non-exhaustive):
- `tenancy_ocid`, `user_ocid`, `compartment_ocid` — cloud identity
- `fingerprint` — API key fingerprint
- `private_key_path` — path to private key file
- `ssh_allowed_cidr` — operator IP (reveals network location)

### 2b. Outputs (`deploy/terraform/outputs.tf`)

Any output that exposes an OCID or resource identifier that is also stored as a GitHub Secret **MUST** have `sensitive = true`. Check:
- `instance_id` — compute OCID
- `cd_nsg_id` — NSG OCID (stored as `OCI_CD_NSG_ID` secret)

Public-facing values like `instance_public_ip` or structural IDs (`vcn_id`, `subnet_id`) that are not stored as secrets may remain non-sensitive.

### 2c. State & plan files

Verify `deploy/terraform/.gitignore` blocks `*.tfstate*`, `terraform.tfvars`, `*.auto.tfvars`, `override.tf*`, `crash.log*`, and `tfplan`.

---

## 3. Docker Compose & environment audit

### 3a. Hardcoded secrets in compose files

Scan `deploy/docker-compose.yml` and `deploy/docker-compose.staging.yml` for:
- Literal passwords, tokens, or API keys (not wrapped in `${VAR}` syntax)
- Any value that should be in `.env` but is inlined

All secrets **MUST** use `${VAR_NAME}` interpolation from the `.env` file, with the `:?` required-variable syntax for mandatory ones (e.g., `${JWT_SECRET:?JWT_SECRET is required}`).

### 3b. `.env.example` completeness

Every `${VAR}` referenced in any compose file **MUST** be documented in `deploy/.env.example` with a comment explaining its purpose. Placeholder values must be obviously fake (e.g., `changeme`, `example.com`).

### 3c. No `.env` files committed

Verify no `.env` file (other than `.env.example`) exists in the git index.

---

## 4. GitHub Actions workflow audit

Scan all files under `.github/workflows/`:

### 4a. Secret references

Every credential, token, password, private key, or host address **MUST** come from `${{ secrets.* }}` — never hardcoded. Check:
- SSH keys, host addresses
- OCI credentials (tenancy, user, fingerprint, private key, region, NSG ID)
- JWT secrets, AMT passwords, VAPID contacts, domains
- SONAR_TOKEN, GIST_SECRET, SYNC_TOKEN
- GITHUB_TOKEN (used correctly for GHCR login, issue creation, etc.)

### 4b. Secret masking

Ensure secrets are passed through `env:` blocks (which GitHub auto-masks in logs), not interpolated directly in `run:` scripts as `${{ secrets.FOO }}`. The pattern should be:

```yaml
env:
  MY_SECRET: ${{ secrets.MY_SECRET }}
run: echo "$MY_SECRET"  # masked in logs
```

NOT:

```yaml
run: echo "${{ secrets.MY_SECRET }}"  # may leak in error messages
```

### 4c. Credential cleanup

Every workflow step that writes secrets to disk (SSH keys, OCI config, .env files) **MUST** have a corresponding cleanup step with `if: always()` that removes those files. Verify:
- `~/.ssh/deploy_key` cleanup
- `~/.oci/` directory cleanup
- File permissions are 600 for all secret files

### 4d. Ephemeral firewall rules

If the workflow opens firewall rules (NSG/security group), verify the close step runs with `if: always()` and handles failure gracefully (e.g., `|| true`).

### 4e. Permissions block

Every workflow **SHOULD** have an explicit top-level `permissions:` block that follows least-privilege. Flag workflows without one.

---

## 5. Deploy scripts audit

Scan all files under `deploy/scripts/`:

- No hardcoded IPs, hostnames, passwords, tokens, or paths to key files
- All configurable values come from arguments (`$1`, `--flag`), environment variables, or sourced `.env` files
- Scripts that handle secrets use `set -euo pipefail` (fail-fast)
- No `echo` or `log` of secret values (check for `echo "$JWT_SECRET"` patterns)

---

## 6. Application code audit

Scan source code across all three codebases:

### 6a. Go (`server/`)

```bash
grep -rn 'password\|secret\|token\|api.key\|private.key' server/ --include='*.go' | grep -v '_test.go' | grep -v 'vendor/'
```

Flag any string literal that looks like a real credential. Acceptable: flag names (`"jwt-secret"`), env var names (`"JWT_SECRET"`), error messages, struct field names.

### 6b. Rust (`agent/`)

```bash
grep -rn 'password\|secret\|token\|api.key\|private.key' agent/ --include='*.rs' | grep -v 'target/'
```

Same criteria as Go.

### 6c. TypeScript (`web/`)

```bash
grep -rn 'password\|secret\|token\|api.key\|apiKey\|API_KEY' web/src/ --include='*.ts' --include='*.tsx' | grep -v 'node_modules/'
```

Additionally check for hardcoded API base URLs (should use environment variables or relative paths).

---

## 7. Cloud-init audit

Scan `deploy/terraform/cloud-init.yaml` for:
- Hardcoded passwords, SSH keys, tokens
- Inline scripts that embed secrets
- All secrets should be injected at deploy time, not baked into the template

---

## 8. Caddy configuration audit

Scan `deploy/caddy/Caddyfile` and `deploy/caddy/Caddyfile.staging`:
- No hardcoded domains (should use `{$DOMAIN}` or environment variables)
- No embedded TLS certificates or keys
- Security headers present (HSTS, X-Content-Type-Options, X-Frame-Options, Referrer-Policy, strip Server header)

---

## 9. PII exposure check

### 9a. Test data hygiene

Smoke test and integration test scripts must use obviously fake data:

```bash
# Look for real-looking email addresses in test files
grep -rn '@' server/tests/ --include='*.go' | grep -v '@test.local\|@example.com\|_test.go' | head -10

# Look for credentials in deploy scripts
grep -rn 'email\|password' deploy/scripts/ --include='*.sh' | grep -v 'changeme\|example\|placeholder'
```

Flag real-looking email addresses or passwords in test fixtures as **MEDIUM**.

### 9b. Application log PII scan

**Go server** — scan all slog calls for sensitive data:

```bash
grep -rn 'slog\.\(Info\|Debug\|Warn\|Error\)\|\.logger\.\(Info\|Debug\|Warn\|Error\)' server/ --include='*.go' | grep -v '_test.go'
```

Verify:
- Email addresses are logged only for auth events (login, register) at INFO level, never at DEBUG
- Passwords and password hashes are never logged at any level
- Full JWT tokens are never logged — only `token_prefix` via `protocol.RedactToken()`
- Session tokens use `RedactToken()` (first 8 chars only)
- Device IDs and user IDs are acceptable (internal identifiers, not PII)

**Rust agent** — scan all tracing calls:

```bash
grep -rn 'info!\|warn!\|error!\|debug!' agent/crates/ --include='*.rs' | grep -v 'target/'
```

Verify:
- Enrollment tokens are not logged in full
- File paths from user-requested file operations are logged at DEBUG only (may reveal sensitive filenames)
- Relay session tokens are redacted or truncated

Severity: **HIGH** for passwords or full tokens logged, **MEDIUM** for email at DEBUG.

### 9c. Error response leakage

Verify error messages returned to clients do not contain internal details:

```bash
grep -rn 'err\.Error()' server/internal/api/ --include='*.go' | grep -v '_test.go'
```

Any `err.Error()` that reaches a client response (not just logged server-side) may leak internal paths, SQL fragments, or package names. Flag as **HIGH**.

---

## 10. Log infrastructure audit

The observability stack (Promtail → Loki → Grafana) must be correctly configured, secure, and complete. Misconfigured log ingestion creates blind spots; exposed log endpoints leak operational data.

### 10a. Promtail configuration

Read `deploy/promtail/promtail-config.yml` and verify:

```bash
cat deploy/promtail/promtail-config.yml
```

- JSON parsing stages exist for each container that emits JSON logs (`opengate-server`, `opengate-caddy`)
- Labels extracted from log content do NOT include sensitive fields. Check which fields are promoted to Loki labels (labels are indexed and visible in Grafana's label browser to all dashboard users):

```bash
grep -A5 'labels:' deploy/promtail/promtail-config.yml
```

If `error` is extracted as a label and error messages can contain user-supplied data (email, file paths), flag as **MEDIUM** — label values are highly visible and not redacted.

- Positions file path is on a persistent volume (not ephemeral tmpfs) — otherwise Promtail re-ingests all logs on restart
- All Docker containers with application logs have a scrape config entry — check for missing containers

### 10b. Loki configuration

Read `deploy/loki/loki-config.yml` and verify:

```bash
cat deploy/loki/loki-config.yml
```

| Setting | Expected value | Why |
|---------|---------------|-----|
| `auth_enabled` | `false` (single-tenant) acceptable only if Loki is not exposed externally | Prevents unauthorized log access |
| `retention_period` | Configured (e.g., `336h` / 14 days) | Prevents unbounded storage growth |
| `retention_enabled` | `true` in compactor section | Actually enforces retention |
| `ingestion_rate_mb` | Reasonable for expected log volume (e.g., 4 MB/s) | Prevents log loss under burst |

### 10c. Log access control

Verify monitoring services are NOT exposed to the public internet:

```bash
# Check if Loki port (3100) or Promtail port (9080) are published in Docker Compose
grep -rn '3100\|9080\|3000' deploy/docker-compose*.yml | grep -i 'port'

# Check Caddyfile for any reverse proxy to monitoring ports
grep -rn '3100\|9080\|grafana\|loki\|promtail' deploy/caddy/ 2>/dev/null
```

Verify:
- Loki API (`:3100`) is reachable only within the Docker network, NOT published to host
- Promtail API (`:9080`) is reachable only within the Docker network
- Grafana (`:3000`) requires authentication — check for `GF_SECURITY_ADMIN_PASSWORD` or equivalent in compose env vars
- If Grafana IS exposed externally (via Caddy proxy), verify it has authentication enabled and uses HTTPS

Severity: **HIGH** if Loki or Grafana is publicly accessible without authentication — exposes all application logs including auth events, IP addresses, and error details.

### 10d. Log format completeness

Verify every service emits logs in a format that Promtail can parse:

```bash
# Go server — must use JSON handler for Promtail compatibility
grep -rn 'slog\.New\|JSONHandler\|TextHandler' server/cmd/ --include='*.go'

# Rust agent — check output format
grep -rn 'tracing_subscriber' agent/crates/ --include='*.rs'
```

- Go server: must use `slog.NewJSONHandler(os.Stdout, ...)` — JSON to stdout is the expected format for Promtail's JSON parsing stages
- Rust agent: if it uses `tracing_subscriber::fmt()` with default text format and runs inside Docker alongside Promtail, the JSON parsing stage will fail silently (logs ingested as raw strings without extracted labels). Flag as **MEDIUM** if containerized, **LOW** if running as a systemd service outside Docker.
- Caddy: emits JSON access logs by default — verify not overridden

---

## 11. Summary report

After completing all checks, print a table:

```
+----------+-------+------------------------------------------------+--------+
| Category | Count | Finding                                        | Status |
+----------+-------+------------------------------------------------+--------+
| 1        |   0   | No tracked secrets in git index                | PASS   |
| 2a       |   0   | All TF variables have sensitive = true          | PASS   |
| ...      |  ...  | ...                                            | ...    |
| 9        |   0   | No PII in logs, test data fake, no error leaks | PASS   |
| 10       |   0   | Log infra: Promtail, Loki, access, completeness| PASS   |
+----------+-------+------------------------------------------------+--------+
```

Status values: **PASS** (no issues), **FIXED** (issues found and remediated), **FAIL** (issues found, cannot auto-fix — explain why).

If ANY category is FAIL, list the specific findings with file paths, line numbers, and remediation instructions.
