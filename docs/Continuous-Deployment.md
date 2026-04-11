# Continuous Deployment

## Pipeline Architecture

```
dev push → CI (19 gates) → merge-to-main → main push
  → build-image.yml (multi-arch image → GHCR)
    → cd.yml:
        resolve-tag → deploy-staging (manual approval) → smoke-test → [rollback on failure]
                    → deploy-production (manual approval) → health-check → [rollback on failure]
```

The CD pipeline runs automatically when a new container image is built and pushed to GHCR. It can also be triggered manually via `workflow_dispatch`.

## Deployment Strategy

**Rolling replace** — `docker compose pull && docker compose up -d` recreates the server container while Caddy's health check detects the new container. Downtime is 5-15 seconds (container start + healthcheck interval). True blue-green deployment is deferred — current scale doesn't justify the complexity.

**Same VPS for staging and production** — staging uses offset ports and separate container names with Docker Compose project name isolation. Both environments are persistent and run side-by-side:

| Component | Production | Staging |
|-----------|-----------|---------|
| Server container | `opengate-server` | `opengate-server-staging` |
| Web-init container | `opengate-web-init` | `opengate-web-init-staging` |
| Caddy container | `opengate-caddy` | `opengate-caddy-staging` |
| HTTP port | 80 | 18080 |
| HTTPS port | 443 | 18443 |
| QUIC port | 9090 | 19090 |
| MPS port | 4433 | 14433 |
| Compose project | `opengate` | `opengate-staging` |
| Env file | `.env` | `.env.staging` |
| Previous tag file | `.previous-tag` | `.previous-tag-staging` |
| TLS | Auto (Let's Encrypt) | None (plain HTTP) |

Staging ports are not exposed publicly — access staging via SSH tunnel:

```bash
ssh -L 18080:127.0.0.1:18080 ubuntu@<VPS>
# Then open http://localhost:18080 in your browser
```

## Workflow Jobs

| Job | Needs | Environment | Purpose |
|-----|-------|-------------|---------|
| `resolve-tag` | — | — | Determine image tag from workflow_run SHA or dispatch input; verify image exists in GHCR; verify cosign signature |
| `deploy-staging` | resolve-tag | staging (manual approval) | scp scripts + configs to VPS, deploy staging, run full smoke tests, PageSpeed Insights audit (informational), rollback on failure |
| `deploy-production` | resolve-tag, deploy-staging | production (manual approval) | scp scripts + configs to VPS, deploy production, health check, rollback on failure |
| `notify-failure` | all | — | Create GitHub Issue with `cd-failure` label on any failure |

## Triggers

| Event | Staging | Production |
|-------|---------|------------|
| Image build completes on `main` | Manual approval | Manual approval (after staging passes) |
| `workflow_dispatch` with `image_tag` | Manual approval | Manual approval (after staging passes) |

## Deploy Scripts

All scripts live in `deploy/scripts/` and run on the VPS via SSH. Pure bash, no external dependencies beyond Docker.

| Script | Purpose |
|--------|---------|
| `common.sh` | Shared functions: `log()`, `fail()`, `wait_healthy()`, `compose_cmd()`, `env_file()`, `prev_tag_file()`, `set_env_var()`, `validate_mode()`, `verify_image()`, `redeploy()`, `container_name()` |
| `deploy.sh` | Save previous tag, update compose, verify cosign signature, pull image, wait for health. Usage: `deploy.sh --mode <staging\|production> --tag <tag>` |
| `smoke-test.sh` | Post-deploy validation. Usage: `smoke-test.sh --mode <mode> --domain <domain>` (production) or `smoke-test.sh --mode <mode> --host <host> --port <port>` (staging) |
| `rollback.sh` | Revert to previous image tag. Usage: `rollback.sh --mode <staging\|production>` |

### Environment Isolation

Staging and production use separate `.env` files to prevent overwriting each other's configuration:
- Production: `/opt/opengate/.env`
- Staging: `/opt/opengate/.env.staging`

Similarly, rollback tags are stored separately:
- Production: `/opt/opengate/.previous-tag`
- Staging: `/opt/opengate/.previous-tag-staging`

### Smoke Test Matrix

| Endpoint | Staging | Production | Expected |
|----------|---------|-----------|----------|
| `GET /api/v1/health` | Yes | Yes | 200, `{"status":"ok"}` |
| `GET /metrics` | Yes | No | 200, Prometheus text exposition format scraped by VictoriaMetrics (reachable inside the Docker network, not via Caddy) |
| `GET /` | Yes | Yes | 200, HTML with `<div id="root">` |
| `GET /devices` | Yes | Yes | 200 (SPA fallback) |
| `GET /vite.svg` | Yes | Yes | 200 (static file) |
| `POST /api/v1/auth/register` | Yes | No | 201 + JWT |
| `GET /api/v1/groups` (with JWT) | Yes | No | 200 |
| `GET /ws/relay/test-token?side=browser` | Yes | No | non-404 (route exists) |

Production uses health + web UI checks because the same image was already validated in staging, and running auth tests would create test users in the production database.

## Image Signature Verification

Image signatures are verified at two points in the deployment pipeline:

1. **CD workflow (`resolve-tag` job)** — `cosign verify` runs in GitHub Actions after confirming the image exists in GHCR. This gates the entire deployment — if the signature is invalid or missing, staging and production deploys are skipped.
2. **VPS (`redeploy()` in `common.sh`)** — `verify_image()` runs cosign verification on the VPS before `docker compose pull`. This ensures the image pulled onto the host matches what was signed in the build pipeline.

Both checks use keyless verification against the Sigstore transparency log:
- **Certificate identity**: `https://github.com/volchanskyi/.*`
- **OIDC issuer**: `https://token.actions.githubusercontent.com`

VPS verification requires `cosign` installed on the host. Set `COSIGN_VERIFY=false` in the environment to disable (not recommended for production).

## Rollback

Before each deploy (staging and production), the current image tag is saved to a mode-specific previous-tag file. If the deploy or smoke tests fail, the `rollback.sh` script automatically redeploys the previous tag.

The rollback file is cleared after a successful rollback to prevent double-rollback loops.

### Emergency Manual Rollback

```bash
# Production
ssh ubuntu@<VPS> "bash /opt/opengate/scripts/rollback.sh --mode production"

# Staging
ssh ubuntu@<VPS> "bash /opt/opengate/scripts/rollback.sh --mode staging"
```

## GitHub Environments

### staging

- Required reviewer: volchanskyi (manual approval gate)
- Environment secrets: `DEPLOY_STAGING_JWT_SECRET`, `DEPLOY_STAGING_AMT_PASS`, `DEPLOY_STAGING_VAPID_CONTACT`, `DEPLOY_STAGING_DOMAIN`

### production

- Required reviewer: volchanskyi (manual approval gate)
- Deployment branches: `main` only
- Environment secrets: `DEPLOY_JWT_SECRET`, `DEPLOY_AMT_PASS`, `DEPLOY_VAPID_CONTACT`, `DEPLOY_DOMAIN`

## Required Secrets

| Secret | Scope | Purpose |
|--------|-------|---------|
| `DEPLOY_SSH_PRIVATE_KEY` | repository | SSH key for VPS access |
| `DEPLOY_HOST` | repository | VPS public IP or hostname |
| `OCI_TENANCY_OCID` | repository | Oracle Cloud tenancy OCID |
| `OCI_USER_OCID` | repository | Oracle Cloud user OCID |
| `OCI_FINGERPRINT` | repository | Oracle Cloud API key fingerprint |
| `OCI_PRIVATE_KEY` | repository | Oracle Cloud API private key (PEM) |
| `OCI_REGION` | repository | Oracle Cloud region identifier |
| `OCI_CD_NSG_ID` | repository | NSG OCID for just-in-time SSH firewall |
| `DEPLOY_JWT_SECRET` | production env | Production JWT signing key |
| `DEPLOY_AMT_PASS` | production env | Intel AMT password |
| `DEPLOY_DOMAIN` | production env | Caddy auto-TLS domain |
| `DEPLOY_VAPID_CONTACT` | production env | Web Push contact email |
| `DEPLOY_STAGING_JWT_SECRET` | staging env | Staging JWT signing key |
| `DEPLOY_STAGING_AMT_PASS` | staging env | Intel AMT password (staging) |
| `DEPLOY_STAGING_DOMAIN` | staging env | Staging domain (`localhost`) |
| `DEPLOY_STAGING_VAPID_CONTACT` | staging env | Web Push contact (staging) |
| `PSI_API_KEY` | repository | Google PageSpeed Insights API key (optional — PSI step skips if absent) |

`OPENGATE_GITHUB_REPO` is derived from `${{ github.repository }}` automatically — no secret required. It enables the server to auto-sync agent manifests from GitHub Releases on startup.

## Manual Deploy

```bash
# Deploy a specific image tag (staging runs first, then production)
gh workflow run cd.yml -f image_tag=sha-abc1234
```

## Concurrency

The workflow uses `concurrency: { group: deploy, cancel-in-progress: false }`. This means deployments queue rather than cancel each other — if two images are built in quick succession, the second deployment waits for the first to complete.

## Failure Notifications

CD failures create GitHub Issues labeled `cd-failure` using the same `notify_failure.py` script as CI. Issues include the failed job name, error log excerpt, and links to the workflow run. Existing open issues for the same job/branch receive a comment instead of a duplicate issue.

## Config Sync

Every deploy copies compose files, Caddy configs, and monitoring configs (VictoriaMetrics, Loki, Promtail, Grafana provisioning) from the repo to the VPS via `scp`. This ensures the infrastructure configuration on the VPS always matches what's in version control — no configuration drift.

The monitoring stack (`docker-compose.monitoring.yml`) is deployed during production deploys only. See [[Monitoring]] for details.
