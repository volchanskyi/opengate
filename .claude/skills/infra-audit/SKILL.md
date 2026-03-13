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

- Smoke test scripts must not log real user data — verify test users use obvious fake data (`@test.local` emails)
- Application logs must not print passwords, tokens, or PII — check `slog`/`tracing` calls
- Error responses must not leak stack traces or internal paths to clients

---

## 10. Summary report

After completing all checks, print a table:

```
+----------+-------+------------------------------------------------+--------+
| Category | Count | Finding                                        | Status |
+----------+-------+------------------------------------------------+--------+
| 1        |   0   | No tracked secrets in git index                | PASS   |
| 2a       |   0   | All TF variables have sensitive = true          | PASS   |
| ...      |  ...  | ...                                            | ...    |
+----------+-------+------------------------------------------------+--------+
```

Status values: **PASS** (no issues), **FIXED** (issues found and remediated), **FAIL** (issues found, cannot auto-fix — explain why).

If ANY category is FAIL, list the specific findings with file paths, line numbers, and remediation instructions.
