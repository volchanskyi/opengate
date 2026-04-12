# Infrastructure

OpenGate uses Infrastructure as Code (Terraform) to provision cloud resources and Docker Compose for the production runtime stack.

## Cloud Provider

Oracle Cloud

## Directory Layout

```
deploy/
├── terraform/
│   ├── main.tf              # OCI provider, VCN, subnet, security list, compute
│   ├── variables.tf          # All configurable inputs
│   ├── outputs.tf            # Instance IP, resource OCIDs
│   ├── cloud-init.yaml       # Docker + UFW bootstrap on first boot
│   ├── terraform.tfvars.example  # Template for credentials and sizing
│   └── .gitignore            # Excludes state files and credentials
├── caddy/
│   ├── Caddyfile             # Production reverse proxy (auto-TLS, security headers)
│   └── Caddyfile.staging     # Staging (plain HTTP, port 80)
├── docker-compose.yml        # Production stack
├── docker-compose.staging.yml  # Persistent staging overrides
└── .env.example              # Environment variable template
```

## Terraform Resources

The Terraform configuration provisions:

**Security list** with ingress rules:
   - TCP 22 (SSH) — restricted to operator IP
   - TCP 80 (HTTP redirect)
   - TCP 443 (HTTPS)
   - UDP 443 (HTTP/3 — Caddy QUIC)
   - TCP 4433 (MPS — Intel AMT CIRA)
   - UDP 9090 (QUIC agent connections)

### Provisioning

```bash
cd deploy/terraform
cp terraform.tfvars.example terraform.tfvars  # fill in OCI credentials
terraform init
terraform plan    # review resources
terraform apply   # provision
```

### Cloud-Init Bootstrap

On first boot the instance automatically:
- Installs Docker CE + Compose plugin
- Configures UFW firewall (same ports as security list — defense in depth)
- Creates `/opt/opengate/` data directories

## Docker Compose Stack

### Production

```bash
cd deploy
cp .env.example .env   # fill in secrets (JWT_SECRET, AMT_PASS, DOMAIN)
docker compose up -d
```

Services:
- **postgres** — PostgreSQL 17 (Alpine), internal-only, health-checked via `pg_isready`. The server connects via `DATABASE_URL` over the Docker bridge network (`sslmode=disable` — same-host traffic).
- **postgres-backup** — Daily `pg_dump` sidecar (`prodrigestivill/postgres-backup-local`), 7-day local retention in a `postgres-backups` volume.
- **server** — OpenGate Go server (GHCR image), depends on `postgres` (waits for healthy), exposes ports 9090/UDP (QUIC) and 4433 (MPS) directly
- **web-init** — One-shot init container that copies web assets from the server image into a shared `web-assets` volume (runs once per deploy, `restart: "no"`)
- **caddy** — Reverse proxy + SPA file server on ports 80/443, auto-TLS via Let's Encrypt, HTTP/3

#### Container Resource Limits

All production containers have memory and CPU limits to prevent runaway processes from starving the VPS:

| Container | Memory Limit | CPU Limit |
|-----------|-------------|-----------|
| postgres | 512 MB | 1.0 |
| postgres-backup | 64 MB | 0.25 |
| server | 512 MB | 1.0 |
| caddy | 256 MB | 0.5 |
| web-init | 128 MB | — |

The server's HTTP port (8080) is only exposed to the Caddy container, not the host. Caddy serves the React SPA from `/srv/web` (mounted read-only from the `web-assets` volume) with `try_files` fallback to `index.html` for client-side routing.

### Staging

```bash
docker compose -f docker-compose.yml -f docker-compose.staging.yml up -d
```

Staging uses offset ports (18080, 18443, 19090, 14433) and a separate `.env.staging` file with secrets from GitHub environment configuration. Staging is persistent — it stays running between deployments, just like production. Access staging via SSH tunnel (`ssh -L 18080:127.0.0.1:18080 ubuntu@<VPS>`).

**Note:** The staging compose file uses the `!override` YAML tag, which requires Docker Compose v2.24+.

## VPS

How staging and production coexist on one VPS

  VPS (single ARM64 instance)
  ├── /opt/opengate/
  │   ├── .env                    ← production secrets
  │   ├── .env.staging            ← staging secrets
  │   ├── docker-compose.yml      ← base config (shared)
  │   ├── docker-compose.staging.yml ← staging overrides
  │   ├── scripts/                ← deploy, rollback, smoke-test, common
  │   └── caddy/
  │       ├── Caddyfile           ← production (HTTPS, auto-TLS)
  │       └── Caddyfile.staging   ← staging (HTTP only)
  │
  ├── Docker project: "opengate" (production)
  │   ├── opengate-postgres   → port 5432 (internal)
  │   ├── opengate-postgres-backup → daily pg_dump, 7-day retention
  │   ├── opengate-server     → port 8080 (internal), depends_on postgres
  │   ├── opengate-web-init   → copies /srv/web to shared volume (exits)
  │   └── opengate-caddy      → ports 80, 443
  │
  └── Docker project: "opengate-staging" (staging)
      ├── opengate-postgres-staging → port 5432 (internal, separate volume)
      ├── opengate-postgres-backup-staging → daily pg_dump
      ├── opengate-server-staging  → port 8080 (internal)
      ├── opengate-web-init-staging → copies /srv/web to shared volume (exits)
      └── opengate-caddy-staging   → ports 18080, 18443

## Deployment Strategy

**Rolling replace** — `docker compose pull && docker compose up -d` recreates the server container while Caddy's health check detects the new container. Downtime is 5-15 seconds (container start + healthcheck interval). See [[Continuous Deployment]] for full pipeline details.

QUIC (port 9090/UDP): agents reconnect within seconds via QUIC connection migration — no special handling needed.

## Caddyfile

Production Caddyfile provides:
- Automatic HTTPS (Let's Encrypt, TLS 1.3)
- HTTP/3 support (UDP 443)
- Security headers (HSTS, X-Content-Type-Options, X-Frame-Options, Referrer-Policy)
- `handle /api/*` and `handle /ws/*` — reverse proxy to Go server (with health check)
- `handle` (catch-all) — serves React SPA from `/srv/web` with `try_files {path} /index.html` fallback
- Cache headers: Vite hashed assets (`/assets/*`) get `immutable` with 1-year max-age; `index.html` gets `no-cache`
- Gzip compression
- JSON access logs

A second virtual host — `status.{$DOMAIN:localhost}` — reverse-proxies to the Uptime Kuma container (`opengate-uptime-kuma:3001`) so the public status page is reachable at `https://status.<domain>` with auto-TLS.

## Firewall Rules

Two layers of firewall (defense in depth):

| Port | Protocol | Source | Purpose |
|------|----------|--------|---------|
| 22 | TCP | Operator IP only | SSH |
| 80 | TCP | 0.0.0.0/0 | HTTP → HTTPS redirect |
| 443 | TCP | 0.0.0.0/0 | HTTPS (Caddy) |
| 443 | UDP | 0.0.0.0/0 | HTTP/3 (Caddy) |
| 9090 | UDP | 0.0.0.0/0 | QUIC (agent connections, mTLS) |
| 4433 | TCP | 0.0.0.0/0 | MPS (Intel AMT CIRA, TLS) |

## TLS

- **HTTPS**: Caddy handles automatic cert provisioning (Let's Encrypt, TLS 1.3)
- **QUIC**: mTLS with ECDSA P-256 certificates (server's `cert.NewManager`)
- **MPS**: RSA 2048 TLS for Intel AMT compatibility
- No plaintext HTTP in production

## Secrets Management

No secrets are committed to the repository. All sensitive values are injected at runtime.

### Layers of Protection

1. **`.gitignore`** — `.env`, `.env.*`, `*.pem`, `terraform.tfvars`, `*.auto.tfvars`, `*.tfstate`, `tfplan` are all excluded from version control
2. **Terraform `sensitive = true`** — OCI credentials (`tenancy_ocid`, `user_ocid`, `fingerprint`, `private_key_path`, `compartment_ocid`), the SSH allowed CIDR, and the `cd_nsg_id` output (stored as GitHub Secret) are marked sensitive, preventing their values from appearing in `terraform plan/apply` output or logs
3. **Docker Compose env vars** — All secrets are parameterized via `${VAR}` references, sourced from `.env` (not committed) or the shell environment
4. **Example files only** — `.env.example` and `terraform.tfvars.example` contain placeholder values, never real credentials

### Runtime Secrets Inventory

| Secret | Source | Used By |
|--------|--------|---------|
| `JWT_SECRET` | `.env` or GitHub Secrets | Server (authentication) |
| `AMT_USER` | `.env` or GitHub Secrets | Server (Intel AMT WSMAN) |
| `AMT_PASS` | `.env` or GitHub Secrets | Server (Intel AMT WSMAN) |
| `VAPID_CONTACT` | `.env` or GitHub Secrets | Server (Web Push, RFC 8292) |
| `POSTGRES_PASSWORD` | `.env` or GitHub Secrets | PostgreSQL, Server (DATABASE_URL), Postgres Exporter |
| `DOMAIN` | `.env` | Caddy (auto-TLS domain) |
| `OPENGATE_QUIC_HOST` | `.env` | Server (QUIC advertised hostname in install.sh / agent enrollment response) |
| `tenancy_ocid` | `terraform.tfvars` | Terraform (OCI provider) |
| `user_ocid` | `terraform.tfvars` | Terraform (OCI provider) |
| `fingerprint` | `terraform.tfvars` | Terraform (OCI API key) |
| `private_key_path` | `terraform.tfvars` | Terraform (OCI API key PEM) |
| `ssh_allowed_cidr` | `terraform.tfvars` | Terraform (firewall rules) |

### GitHub Secrets (for CD pipeline)

The following secrets should be configured in GitHub repository settings (`Settings > Secrets and variables > Actions`):

| GitHub Secret | Purpose |
|---------------|---------|
| `DEPLOY_JWT_SECRET` | Production JWT signing key |
| `DEPLOY_AMT_PASS` | Intel AMT WSMAN password |
| `DEPLOY_VAPID_CONTACT` | Web Push contact email |
| `DEPLOY_DOMAIN` | Caddy auto-TLS domain |
| `DEPLOY_POSTGRES_PASSWORD` | Production PostgreSQL password |
| `DEPLOY_STAGING_JWT_SECRET` | Staging JWT signing key |
| `DEPLOY_STAGING_AMT_PASS` | Staging Intel AMT password |
| `DEPLOY_STAGING_VAPID_CONTACT` | Staging Web Push contact |
| `DEPLOY_STAGING_DOMAIN` | Staging domain (`localhost`) |
| `DEPLOY_STAGING_POSTGRES_PASSWORD` | Staging PostgreSQL password |
| `DEPLOY_HOST` | VPS public IP or hostname |
| `DEPLOY_SSH_PRIVATE_KEY` | SSH key for deploying to VPS |
| `OCI_TENANCY_OCID` | Oracle Cloud tenancy OCID |
| `OCI_USER_OCID` | Oracle Cloud user OCID |
| `OCI_FINGERPRINT` | Oracle Cloud API key fingerprint |
| `OCI_PRIVATE_KEY` | Oracle Cloud API private key (PEM contents) |
| `OCI_REGION` | Oracle Cloud region identifier |
| `OCI_CD_NSG_ID` | NSG OCID for just-in-time SSH firewall rules |

### Best Practices

- **Never commit `.env` or `terraform.tfvars`** — only `.env.example` and `terraform.tfvars.example` belong in version control
- **Rotate secrets regularly** — JWT secret, AMT credentials, OCI API keys
- **Use strong JWT secrets** — minimum 32 random characters (`openssl rand -base64 32`)
- **Restrict SSH access** — `ssh_allowed_cidr` should be a single IP (`x.x.x.x/32`), not a subnet
- **Terraform state** — if using remote state (S3, OCI Object Storage), enable server-side encryption

## Config Validation

All deploy configs are statically analyzed in CI (the `Config Lint` job) and locally via `make lint-deploy`.

| Tool | Target | What It Catches |
|------|--------|-----------------|
| `yamllint` | `deploy/**/*.yml` (cloud-init.yaml excluded) | YAML syntax, formatting, line length |
| `terraform fmt -check` | `*.tf` | HCL formatting drift |
| `terraform validate` | `*.tf` | Syntax errors, type mismatches, missing references |
| `tflint` | `*.tf` | Best practices: naming, docs, unused declarations |
| `docker compose config` | `docker-compose*.yml` | Compose schema, undefined services, env var refs |
| `caddy fmt --diff` | `Caddyfile*` | Caddyfile formatting |
| `caddy validate` | `Caddyfile*` | Directive validity, placeholder resolution |
| `trivy config` | `deploy/`, `Dockerfile` | Security misconfigs (open ports, Dockerfile antipatterns) |
| `validate-configs.sh` | All configs | Cross-file consistency (ports, env vars, tfvars completeness) |

The integration test script (`deploy/tests/validate-configs.sh`) verifies:
1. Every port in `docker-compose.yml` has a matching OCI security list rule AND UFW rule
2. Every `${VAR}` in `docker-compose.yml` and `docker-compose.staging.yml` has an entry in `.env.example`
3. Every required Terraform variable (no default) has an entry in `terraform.tfvars.example`
4. `cloud-init.yaml` has the `#cloud-config` magic header
