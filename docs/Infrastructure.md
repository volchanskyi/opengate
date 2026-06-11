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
   - TCP 22 (SSH) — break-glass `var.ssh_allowed_cidr` (typically `127.0.0.1/32` to disable) + public subnet CIDR for the [OCI Bastion service endpoint](#operator-access-via-oci-bastion)
   - TCP 80 (HTTP redirect)
   - TCP 443 (HTTPS)
   - UDP 443 (HTTP/3 — Caddy QUIC)
   - TCP 4433 (MPS — Intel AMT CIRA)
   - UDP 9090 (QUIC agent connections)

### Provisioning

```bash
cd deploy/terraform
cp terraform.tfvars.example terraform.tfvars     # fill in OCI credentials
cp backend.tfbackend.example backend.tfbackend   # fill in OCI namespace
terraform init -backend-config=backend.tfbackend
terraform plan    # review resources
terraform apply   # provision
```

### State Backend

State lives in an OCI Object Storage bucket (`opengate-tfstate`) accessed through the S3-compatible API, **not** on the operator's laptop. This eliminates the laptop-SPOF and gives us versioned rollback for free.

#### One-time bucket and IAM setup (operator)

1. Create the bucket in the same region as the rest of the infrastructure (`us-sanjose-1`):
   - **Versioning ON** — every tfstate write keeps a prior version for rollback.
   - **Public access OFF** — bucket is private.
2. Create a dedicated IAM user `tf-state-writer` whose only privilege is `manage object-family` on `opengate-tfstate`. This user is **distinct** from the OCI user the deploy pipeline runs as — least privilege, blast radius confined to the state file.
3. Generate a Customer Secret Key for that user (Identity → Users → `tf-state-writer` → Customer Secret Keys). This yields an S3-compatible access key + secret pair.
4. Save the pair locally as an AWS-style INI at `~/.oci/terraform-credentials` (file mode 0600, gitignored by `.gitignore` line 1 of the repo root):
   ```ini
   [default]
   aws_access_key_id     = <S3-compat access key>
   aws_secret_access_key = <S3-compat secret>
   ```
   Add the secret key to the operator's password manager as backup — OCI does not let you retrieve it after creation.

#### Operator backend config

Copy [`backend.tfbackend.example`](../deploy/terraform/backend.tfbackend.example) to `backend.tfbackend` (gitignored) and substitute the OCI namespace (find it with `oci os ns get --query data --raw-output`). The endpoint becomes `https://<namespace>.compat.objectstorage.us-sanjose-1.oraclecloud.com`.

Then run `terraform init -backend-config=backend.tfbackend` once — Terraform writes the resolved backend config into `.terraform/terraform.tfstate` (gitignored).

#### Required env var on every terraform invocation

The AWS SDK v2 that backs Terraform's `s3` backend defaults to a flexible-checksum body that uses streaming chunked encoding for `PutObject`. OCI Object Storage's S3-compat rejects it with `501 NotImplemented: AWS chunked encoding not supported`. Set this env var on every `terraform init`/`plan`/`apply` against the remote backend:

```bash
export AWS_REQUEST_CHECKSUM_CALCULATION=when_required
```

`backend "s3" { skip_s3_checksum = true }` in [`deploy/terraform/main.tf`](../deploy/terraform/main.tf) handles response-side checksum verification; this env var handles the request side. Both are needed. The `terraform-drift` workflow ([`.github/workflows/terraform-drift.yml`](../.github/workflows/terraform-drift.yml)) sets this env var on its `init` and `plan` steps automatically.

#### Migrating an existing local state (one-time)

If the working copy still has `terraform.tfstate` on disk, run:

```bash
cd deploy/terraform
terraform init -backend-config=backend.tfbackend -migrate-state   # copies local → bucket
terraform state list                                              # verify list matches pre-migration
terraform plan                                                    # must report no resource changes
```

Then move the local `terraform.tfstate*` to an offline encrypted backup and delete from the working tree. Keep the offline copy until at least one successful `plan`/`apply` cycle against the bucket confirms it works — that is the rollback path if the bucket is misconfigured.

#### Locking caveat

OCI Object Storage's S3 emulation has **no DynamoDB-equivalent locking primitive**, so Terraform cannot acquire a state lock the way it would against real S3. As long as OpenGate stays single-operator and applies are infrequent, this is acceptable. **Do not** run two simultaneous `apply`s against the same state — there is no protection from interleaved writes.

#### Rollback (restore a prior tfstate)

Bucket versioning is the rollback mechanism. To restore an earlier version of `terraform.tfstate`:

```bash
# List all versions of the state object
oci os object list-object-versions --bucket-name opengate-tfstate --prefix terraform.tfstate

# Download the version you want
oci os object get --bucket-name opengate-tfstate \
  --name terraform.tfstate \
  --version-id <version-id> \
  --file terraform.tfstate.restore

# Push it back as the new current version
oci os object put --bucket-name opengate-tfstate \
  --name terraform.tfstate \
  --file terraform.tfstate.restore --force
```

Always run `terraform plan` after a restore to confirm the chosen version still matches the live infrastructure.

#### Credential rotation

Generate a new Customer Secret Key for `tf-state-writer`, update `~/.oci/terraform-credentials`, then delete the old key from OCI Console. No Terraform code or state changes required.

### Custom IaC policies

Project-specific invariants (Always-Free shape, required tags, image pinning, action SHA-pinning) live in [`policy/`](../policy/) and are enforced via [Conftest](https://www.conftest.dev/) (OPA Rego). Run with `make iac-policy-custom`; full per-policy listing in the directory READMEs. The compute and tag rules ALSO run inside `terraform test` ([`modules/networking/tests/`](../deploy/terraform/modules/networking/tests/), [`modules/compute/tests/`](../deploy/terraform/modules/compute/tests/)) — overlap is deliberate per [ADR-015](adr/ADR-015-iac-defense-in-depth.md).

The terraform Rego check requires a plan-file because conftest's HCL2 parser leaves `${var.X}` references unresolved. Operator runs:

```bash
terraform -chdir=deploy/terraform plan -out=/tmp/tfplan.binary
terraform -chdir=deploy/terraform show -json /tmp/tfplan.binary > /tmp/tfplan.json
make iac-policy-custom   # picks up /tmp/tfplan.json automatically
```

The compose and workflow Rego checks need no plan-file and run unconditionally in CI.

### IaC plan + destroy-blocklist gate

The `iac-gate` job in [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) runs `terraform plan` against the remote backend on every commit or PR that touches `deploy/terraform/**` (path-filtered inside the job; non-terraform commits skip the terraform steps and complete in ~10 s). It posts a markdown summary — sticky PR comment on PRs, GitHub Job Summary on direct pushes — and **blocks merge** if the plan destroys a protected resource type:

| Protected types (current set) |
|---|
| `oci_core_vcn` |
| `oci_core_subnet` |
| `oci_core_security_list` |
| `oci_core_network_security_group` |
| `oci_objectstorage_bucket` |

Bypass policy (intentionally narrow):

- **On a pull request:** add the `iac:approve-destroy` label, then re-run the workflow. The bypass is auditable — the sticky PR comment records that the override was active.
- **On a direct push to `dev`:** no bypass exists. Destructive terraform changes must go through a labelled PR. This is the strongest discipline available without admin overrides.

The job is wired into `merge-to-main.needs:` so the auto-merge to `main` cannot run until the gate passes (or correctly skips when no terraform files changed). Drift detection (next section) is the complementary control that catches anything that landed outside the gate.

Authentication: reuses the read-only `tf-drift-reader` IAM user provisioned for nightly drift detection (same `OCI_DRIFT_*` + `TFSTATE_S3_*` + `OCI_TFSTATE_NAMESPACE` secrets). No new IAM principal is created for the gate — the permissions are identical (inspect + state read). `terraform init` retries 3× with backoff to absorb the same transient OCI S3-compat DNS flake that drift detection handles.

The parser script [`deploy/scripts/parse-tfplan.sh`](../deploy/scripts/parse-tfplan.sh) is testable in isolation via `make test-parse-tfplan` (three canned fixtures cover the gate-decision matrix).

### Drift detection

Out-of-band changes — operator clicks in the OCI Console, `cd.yml`'s runtime NSG mutations, manual security-list edits — silently desync the tfstate from reality. [`.github/workflows/terraform-drift.yml`](../.github/workflows/terraform-drift.yml) runs nightly at 03:00 UTC, executes `terraform plan -refresh-only -detailed-exitcode` against the remote backend, and alerts on any diff. Same audit pattern as [`.github/workflows/mutation.yml`](../.github/workflows/mutation.yml).

Local mirror: `make terraform-drift` (uses the operator's local OCI creds).

#### What happens on drift

When `plan -refresh-only` returns exit code 2, the workflow:

1. Generates a canonical drift summary via [`scripts/terraform-drift-summarize.sh`](../scripts/terraform-drift-summarize.sh) (`drift_count`, per-resource `address`/`actions`/`type`).
2. Uploads `drift.txt` (raw plan output) + `drift.json` + `drift-summary.json` as a 30-day workflow artifact.
3. Posts the truncated plan output to Telegram via the existing `DEPLOY_TELEGRAM_BOT_TOKEN`/`DEPLOY_TELEGRAM_CHAT_ID` secrets.
4. Pushes the summary record to Loki on the production VPS via [`scripts/terraform-drift-loki-push.sh`](../scripts/terraform-drift-loki-push.sh) (stream label `{app="opengate", source="terraform-drift", env="ci"}`), reusing the SSH+docker pattern from `scripts/mutation-loki-push.sh`.
5. Exits red for audit-trail visibility.

There is **no auto-remediation**. Drift is investigated by the operator. If the legitimate cause was an operator-side action (e.g. a console click that should become Terraform code), the resolution is to update the config and `apply`; if it was an injection by `cd.yml`, see "Known interactions" below.

#### IAM (one-time, operator)

The workflow authenticates as a separate read-only IAM user `tf-drift-reader` — distinct from both `tf-state-writer` (T1) and the CD-deploy user. Provision via OCI Console or CLI:

1. Create group `tf-drift-readers`.
2. Create user `tf-drift-reader`; add to the group; generate an API signing key pair (save the fingerprint and `.pem`).
3. Policies (least privilege):
   - `Allow group tf-drift-readers to inspect all-resources in compartment opengate`
   - `Allow group tf-drift-readers to read object-family in bucket opengate-tfstate`
4. Add repo-level GitHub Secrets:
   - `OCI_DRIFT_USER_OCID` — the user OCID
   - `OCI_DRIFT_FINGERPRINT` — the API key fingerprint
   - `OCI_DRIFT_PRIVATE_KEY` — the API private-key PEM contents (multiline secret)
   - `OCI_TFSTATE_NAMESPACE` — the OCI Object Storage namespace used to construct the S3 endpoint
   - `TFSTATE_S3_ACCESS_KEY` / `TFSTATE_S3_SECRET_KEY` — the S3-compat key pair for the `tf-state-writer` user from the State Backend section (the drift workflow only needs read, but reuses the existing pair)

`OCI_TENANCY_OCID`, `OCI_REGION`, `OCI_USER_OCID`, `OCI_PRIVATE_KEY`, `OCI_FINGERPRINT`, `OCI_CD_NSG_ID`, `DEPLOY_SSH_PRIVATE_KEY`, `DEPLOY_HOST` are reused from the existing CD pipeline. The drift workflow uses `tf-drift-reader` for the OCI provider during `plan` and the existing CD user only for the firewall opener that brackets the Loki push (the drift user has no NSG-write permission, by design).

Quarterly: audit that the `tf-drift-readers` policy document has not been broadened.

#### Known interactions

`.github/workflows/cd.yml` mutates the `cd_deploy` NSG's ingress rules at deploy time for just-in-time SSH (per the [stale NSG rule cleanup commit](https://github.com/volchanskyi/opengate/commit/bd80684)). If those mutations surface as drift every night, options:

- (a) Add `ignore_changes = [ingress_security_rules]` on `oci_core_network_security_group.cd_deploy` in [`deploy/terraform/modules/networking/main.tf`](../deploy/terraform/modules/networking/main.tf) — loses tfstate tracking of those rules but stops alerting.
- (b) Split the ingress rules into separate `oci_core_network_security_group_security_rule` resources and `ignore_changes` on those — preserves NSG-level tracking.

Decide after one week of soak. If the cleanup composite always restores the NSG to a clean baseline at the end of each CD run, the drift workflow may stay quiet without either option.

#### Grafana

The Loki stream feeds the existing monitoring stack. Recommended panels (provisioned via [`deploy/grafana/`](../deploy/grafana/) if applicable, otherwise a one-shot dashboard JSON):

- **Stat**: days since last drift event.
- **Time series**: drift events per week (rolling 90-day window).
- **Table**: most-recent drift summary — resource address, action, run ID, timestamp.

### Operator access via OCI Bastion

Operator SSH access to the **OKE worker node** goes through the OCI Bastion service, not the static `ssh_allowed_cidr` rule. The dev machine sits on a dynamic ISP-issued IP and updating the CIDR after every ISP rebind was the original pain point — bastion sessions are gated by OCI IAM instead of L4 CIDR, so the dev-machine IP is irrelevant. (Before the Phase 13b cutover the bastion fronted the compose VM; that VM was decommissioned and the bastion was repointed at the worker-node subnet.)

Grafana is a ClusterIP service, reached with `kubectl port-forward` (`make tunnel`), not an SSH tunnel. Uptime monitoring is an external SaaS — no in-cluster status UI to tunnel to (see [ADR-035](adr/ADR-035-oke-free-tier-block-volume-remediation.md)).

CI keeps the just-in-time NSG-rule pattern in [`.github/actions/oci-ssh-setup`](../.github/actions/oci-ssh-setup/) — the bastion is for **human** access only. See [ADR-018](adr/ADR-018-oci-bastion-operator-access.md) for the decision rationale.

#### Daily flow

```bash
make tunnel   # kubectl port-forward: Grafana :3000 → localhost (Ctrl-C to stop)
make ssh      # Managed SSH shell on the OKE worker node
```

Browse to `http://localhost:3000` (Grafana) once `make tunnel` is up. `make ssh` resolves the node + bastion automatically and caches the session at `~/.cache/opengate/bastion-session.json` (5–10 s on first create, instant within the 3 h TTL).

> **Node-SSH prerequisite:** the OCI Cloud Agent **Bastion plugin** must be `RUNNING` on the worker node for `make ssh`. On OKE *managed* nodes it is not enabled by default — until it is (tracked as a follow-up), use the break-glass path: direct `ssh opc@<node-public-ip>` (the node NSG allows TCP 22 from `ssh_allowed_cidr`).

#### One-time operator onboarding

Per-user — repeat once per new team member.

1. **OCI IAM** (admin-side, one-time per operator):
   - Create an IAM user; add to a `bastion-users` group.
   - Attach policies (least privilege):
     - `Allow group bastion-users to manage bastion-session in compartment opengate`
     - `Allow group bastion-users to read instance in compartment opengate`
     - `Allow group bastion-users to read instance-agent-plugins in compartment opengate`
2. **Local OCI CLI** (operator-side):
   - Install the OCI CLI: <https://docs.oracle.com/iaas/Content/API/SDKDocs/cliinstall.htm>
   - Generate an API signing key pair: `oci setup config`
   - Verify with `oci iam region list`.
3. **SSH key** (operator-side):
   - `~/.ssh/id_ed25519` + `.pub` (default path used by the wrapper; override via `BASTION_SSH_KEY` if your key lives elsewhere).
4. **Run** `make ssh` from a fresh checkout. The wrapper resolves the bastion OCID via `terraform output` and the worker-node OCID + private IP from the cluster node pool (`oci ce node-pool get`); no hand-copied identifiers.

#### Plumbing

| File | Purpose |
|---|---|
| [`deploy/terraform/modules/bastion/`](../deploy/terraform/modules/bastion/) | Provisions `oci_bastion_bastion.opengate` targeting the **OKE worker-node subnet** (`module.networking.oke_node_subnet_id`). STANDARD type, `client_cidr_block_allow_list = ["0.0.0.0/0"]`, `max_session_ttl_in_seconds = 10800`. |
| [`deploy/terraform/modules/networking/oke.tf`](../deploy/terraform/modules/networking/oke.tf) | The worker-node NSG rule `node_ingress_ssh` allows TCP 22 from `var.ssh_allowed_cidr` (operator break-glass — set to `127.0.0.1/32` in `terraform.tfvars` to disable). The bastion's /28 service endpoint reaches the node intra-subnet. |
| OCI Cloud Agent **Bastion plugin** (on the node) | Managed SSH rides the node agent's outbound tunnel, so the plugin must be `RUNNING`. On OKE managed nodes it is not enabled by default — see the node-SSH prerequisite above. |
| [`deploy/scripts/bastion-session.sh`](../deploy/scripts/bastion-session.sh) | Pure-bash + OCI CLI wrapper (`ssh` / `diagnose` / `purge`). Resolves the node from the node pool; caches the active session at `~/.cache/opengate/bastion-session.json` with a 5-min headroom over the 3 h TTL. |
| `Makefile` `ssh` target | Shells into the wrapper. `make tunnel` is separate — `kubectl port-forward` of the in-cluster monitoring services. |

#### Verification (after `terraform apply`)

```bash
# 1. Bastion is ACTIVE
oci bastion bastion get --bastion-id "$(terraform -chdir=deploy/terraform output -raw bastion_id)" \
  | jq -r '.data."lifecycle-state"'  # → ACTIVE

# 2. Cloud Agent Bastion plugin status on the worker node
NODE_OCID=$(oci ce node-pool get \
  --node-pool-id "$(terraform -chdir=deploy/terraform output -raw oke_node_pool_id)" \
  --query 'data.nodes[0].id' --raw-output)
oci instance-agent plugin get --instanceagent-id "$NODE_OCID" \
  --plugin-name Bastion --compartment-id "$OCI_COMPARTMENT_OCID" \
  | jq -r '.data.status'  # → RUNNING (if STOPPED, see the node-SSH prerequisite)

# 3. Grafana UI via kubectl port-forward
make tunnel &                                # backgrounds the forward
curl -sf http://localhost:3000 | head -1     # Grafana HTML
kill %1                                       # stop the forward

# 4. IAM audit trail — confirms the session is attributed to YOUR user
oci audit event list --compartment-id "$OCI_COMPARTMENT_OCID" \
  --start-time "$(date -u -d '1 hour ago' +%FT%TZ)" \
  --end-time   "$(date -u +%FT%TZ)" \
  | jq -r '.data[] | select(."event-name" == "CreateManagedSshSession") | ."identity"."principal-name"'
```

#### Failure modes

| Symptom | Likely cause | Resolution |
|---|---|---|
| `make ssh` exits with `oci bastion session create-managed-ssh failed` | IAM policies not attached to your group | Re-check the three policies in step 1 above. |
| Session creates, but `ssh` hangs | Bastion plugin not RUNNING on the node | `deploy/scripts/bastion-session.sh diagnose` (or verification step 2). If `STOPPED`, enable it on the node via OCI Console → Instance → Oracle Cloud Agent, or use the direct-SSH break-glass path. |
| `terraform output -raw bastion_id` returns empty | `terraform apply` not run since the bastion module landed | Re-run `terraform apply`. |
| `make tunnel` works but Grafana returns 502 | Monitoring pod not Ready | `kubectl -n monitoring get pods`; `kubectl -n monitoring logs deploy/monitoring-grafana`. |

#### Risks (and what we accepted)

- **3 h session TTL** is an OCI service cap. Long interactive debug sessions must accept a one-time mid-session reconnect — the cache wrapper handles it transparently on the next `make ssh`.
- **Bastion plugin reliability** — if the plugin stops, Managed SSH fails. Monitor via the existing infrastructure health check; the static `ssh_allowed_cidr` rule remains as a break-glass.

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
| 22 | TCP | `var.ssh_allowed_cidr` (break-glass — typically `127.0.0.1/32` to disable) | SSH |
| 22 | TCP | Public subnet CIDR (`10.0.1.0/24`) | SSH via OCI Bastion's /28 service endpoint (see [ADR-018](adr/ADR-018-oci-bastion-operator-access.md)) |
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
| `OCI_TFSTATE_NAMESPACE` | OCI Object Storage namespace, used to construct the S3 endpoint for the remote tfstate backend (terraform-drift workflow) |
| `TFSTATE_S3_ACCESS_KEY` | S3-compatible access key for the `tf-state-writer` user (terraform-drift workflow reads tfstate) |
| `TFSTATE_S3_SECRET_KEY` | S3-compatible secret key paired with `TFSTATE_S3_ACCESS_KEY` |
| `OCI_DRIFT_USER_OCID` | OCID of the read-only `tf-drift-reader` IAM user (terraform-drift workflow) |
| `OCI_DRIFT_FINGERPRINT` | API key fingerprint for `tf-drift-reader` |
| `OCI_DRIFT_PRIVATE_KEY` | API private key (PEM contents) for `tf-drift-reader` |

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
