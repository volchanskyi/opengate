# Infrastructure

OpenGate uses Terraform for OCI infrastructure and Helm for the current OKE runtime. Docker Compose files remain in the repo for local tests and dormant recovery paths, but production and staging deploy through Kubernetes.

## Cloud Provider

Oracle Cloud

## Directory Layout

```
deploy/
├── terraform/                # OCI networking, OKE, bastion, remote state
├── helm/
│   ├── opengate/             # Application chart + staging/production overlays
│   └── monitoring/           # VictoriaMetrics, Grafana, Loki, exporters
├── scripts/                  # Smoke tests, rollback helpers, bastion wrapper
├── docker-compose.test.yml   # Local/E2E test environment
├── docker-compose*.yml       # Dormant/local Compose artifacts
└── caddy/                    # Dormant Caddyfiles kept with the Compose path
```

## Terraform Resources

The Terraform configuration currently provisions the OKE substrate, networking,
the human operator access plane, and the off-cluster backup substrate. The
resource inventory is the Terraform root module plus `networking`, `oke`,
`bastion`, and `backups` modules in [`deploy/terraform`](../deploy/terraform/):

- VCN, route table, public subnets, security list, and OKE NSGs.
- OKE Basic cluster and node pool.
- OCI Bastion targeting the OKE worker-node subnet.
- Postgres backup bucket, its retention lifecycle rule, and the
  least-privilege lifecycle IAM policy ([`modules/backups`](../deploy/terraform/modules/backups/)).
- Remote state in OCI Object Storage through Terraform's S3-compatible backend.

The former compute VM is intentionally not instantiated by the root module. The
`compute` module remains in the tree as a tested recovery artifact.

The backup bucket, lifecycle rule, and lifecycle IAM policy were originally
created imperatively with the `oci` CLI and are reconciled into Terraform by
**importing** the live resources, never recreating them — see
[`modules/backups/README.md`](../deploy/terraform/modules/backups/README.md). The
write-only pre-authenticated request (PAR) the server uses to push dumps stays a
runtime credential in the Kubernetes Secret (`BACKUP_PAR_URL`), out of git and
Terraform state.

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

Out-of-band changes — operator clicks in the OCI Console, `cd.yml`'s runtime NSG mutations, manual security-list edits — silently desync the tfstate from reality. [`.github/workflows/terraform-drift.yml`](../.github/workflows/terraform-drift.yml) runs on a nightly cron, executes `terraform plan -refresh-only -detailed-exitcode` against the remote backend, and alerts on any diff. Same audit pattern as [`.github/workflows/mutation.yml`](../.github/workflows/mutation.yml).

Local mirror: `make terraform-drift` (uses the operator's local OCI creds).

#### What happens on drift

When `plan -refresh-only` returns exit code 2, the workflow:

1. Generates a canonical drift summary via [`scripts/terraform-drift-summarize.sh`](../scripts/terraform-drift-summarize.sh) (`drift_count`, per-resource `address`/`actions`/`type`).
2. Uploads `drift.txt` (raw plan output) + `drift.json` + `drift-summary.json` as a 30-day workflow artifact.
3. Posts the truncated plan output to Telegram via the existing `DEPLOY_TELEGRAM_BOT_TOKEN`/`DEPLOY_TELEGRAM_CHAT_ID` secrets.
4. Pushes the summary record to VictoriaMetrics via [`scripts/terraform-drift-vm-push.sh`](../scripts/terraform-drift-vm-push.sh), which uses the shared kubectl transport in [`scripts/lib/vm-push.sh`](../scripts/lib/vm-push.sh).
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

`OCI_TENANCY_OCID`, `OCI_REGION`, `OCI_USER_OCID`, `OCI_PRIVATE_KEY`, and `OCI_FINGERPRINT` are reused from the OKE-backed CD pipeline. The drift workflow uses `tf-drift-reader` for the OCI provider during `plan`; the trend push reaches the cluster through [`oci-kube-setup`](../.github/actions/oci-kube-setup/action.yml) rather than opening an SSH path.

Quarterly: audit that the `tf-drift-readers` policy document has not been broadened.

#### Known interactions

The historical CD path temporarily mutated the `cd_deploy` NSG for just-in-time
SSH, but current CD uses [`oci-kube-setup`](../.github/actions/oci-kube-setup/action.yml)
and the OKE API. A refresh-only Terraform drift now represents real OCI drift
rather than expected deploy-time SSH churn.

#### Grafana

The Prometheus series feeds the provisioned
[`terraform-drift-trend.json`](../deploy/grafana/provisioning/dashboards/terraform-drift-trend.json)
dashboard through VictoriaMetrics. Loki remains available for investigating the
application and cluster logs around a drift event.

### Operator access via OCI Bastion

Operator SSH access to the **OKE worker node** goes through the OCI Bastion service, not the static `ssh_allowed_cidr` rule. The dev machine sits on a dynamic ISP-issued IP and updating the CIDR after every ISP rebind was the original pain point — bastion sessions are gated by OCI IAM instead of L4 CIDR, so the dev-machine IP is irrelevant.

Grafana is a ClusterIP service, reached with `kubectl port-forward` (`make tunnel`), not an SSH tunnel. Uptime monitoring is an external SaaS — no in-cluster status UI to tunnel to (see [ADR-035](adr/ADR-035-oke-free-tier-block-volume-remediation.md)).

CI reaches the cluster through [`oci-kube-setup`](../.github/actions/oci-kube-setup/action.yml) and Kubernetes APIs. The bastion is for **human** node access only. See [ADR-018](adr/ADR-018-oci-bastion-operator-access.md) for the original access-plane rationale and the current OKE update.

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

## Runtime Stack

The current runtime is Kubernetes on OKE:

| Layer | Source of truth |
|---|---|
| Application chart | [`deploy/helm/opengate`](../deploy/helm/opengate/) |
| Staging overlay | [`values-staging.yaml`](../deploy/helm/opengate/values-staging.yaml) |
| Production overlay | [`values-production.yaml`](../deploy/helm/opengate/values-production.yaml) |
| Monitoring chart | [`deploy/helm/monitoring`](../deploy/helm/monitoring/) |
| CD workflow | [`.github/workflows/cd.yml`](../.github/workflows/cd.yml) |
| Load-test workflow | [`.github/workflows/load-test.yml`](../.github/workflows/load-test.yml) |

Live reconciliation on 2026-06-18 matched the intended OKE model: Helm releases
`opengate`, `opengate-staging`, and `monitoring` were deployed; app and
monitoring workloads were Ready; production and staging were running the same
server image tag from GHCR; ingress-nginx owned the public HTTP(S) load balancer;
and only the intended three block-backed PVCs existed.

### Application Releases

- **Production** runs in the `opengate` namespace with the production overlay.
  Shared server keys are mounted from the existing Secret, and the production
  Postgres StatefulSet keeps the persistent block volume.
- **Staging** runs in the `opengate-staging` namespace with the staging overlay.
  Staging disables L4 hostPorts and uses `emptyDir` for server `/data` and
  Postgres storage; the CD workflow reaches it through a temporary Service
  port-forward.
- **Monitoring** runs in the `monitoring` namespace. See
  [Monitoring.md](./Monitoring.md) for the component model and access path.

### Network Exposure

Exact port numbers and source ranges live in
[`deploy/terraform/modules/networking/oke.tf`](../deploy/terraform/modules/networking/oke.tf)
and the Helm values files. The high-level model is:

- HTTP(S) reaches ingress-nginx through the OCI load balancer subnet.
- QUIC agent transport and Intel AMT MPS are non-HTTP and are exposed from the
  production server pod through hostPorts on the worker node.
- Staging does not bind those hostPorts; staging validation uses port-forwarded
  HTTP.
- Operator node access uses OCI Bastion plus the break-glass SSH rule described
  above.

### TLS

- Public HTTP TLS is terminated by ingress-nginx with cert-manager issuing the
  certificates configured by the app chart.
- QUIC uses the server's mTLS certificate manager and advertises the host from
  the app chart values.
- MPS uses the server's Intel AMT-compatible TLS path.

## Dormant Compose / Caddy Artifacts

The Docker Compose and Caddy files under [`deploy/`](../deploy/) are no longer
invoked by the normal GitHub Actions CD path. They are still linted so the
recovery/local artifacts do not silently rot, but current staging and production
state comes from Helm. Do not document Compose as the production deployment
mechanism unless the root Terraform module re-instantiates the compute module
and CD is explicitly moved back to that path.

## Secrets Management

No secrets are committed to the repository. Runtime secrets enter through three
surfaces:

1. **Kubernetes Secrets** referenced by the Helm charts. The app chart expects
   the existing Secret described in
   [`secrets.example.yaml`](../deploy/helm/opengate/secrets.example.yaml); the
   monitoring chart expects the Secret described in its
   [`values.yaml`](../deploy/helm/monitoring/values.yaml) and
   [`NOTES.txt`](../deploy/helm/monitoring/templates/NOTES.txt).
2. **GitHub Actions secrets** consumed by [`cd.yml`](../.github/workflows/cd.yml),
   [`terraform-drift.yml`](../.github/workflows/terraform-drift.yml),
   [`load-test.yml`](../.github/workflows/load-test.yml), and the nightly trend
   workflows.
3. **Local operator files** such as `terraform.tfvars`, `backend.tfbackend`, and
   OCI CLI credentials. These stay gitignored.

The exact secret inventory is canonical in the workflow and chart sources rather
than duplicated here.

## Config Validation

All deploy configs are statically analyzed in CI by the `Config Lint` job and
locally through `make lint-deploy` / `make lint-k8s`.

| Tool | Target | What it catches |
|---|---|---|
| `yamllint` | Deploy YAML | Syntax, formatting, line length |
| `terraform fmt` / `terraform validate` / `tflint` | Terraform | HCL formatting, provider syntax, lint findings |
| `terraform test` | Terraform modules and root | Module invariants and variable validation |
| `helm lint` / `helm template` / `kubeconform` | Helm charts | Chart/schema drift |
| Checkov / Trivy / Conftest | IaC, Dockerfile, workflow, Kubernetes policy | Security misconfiguration and project-specific invariants |
| `docker compose config` / `caddy validate` | Dormant/local artifacts | Keeps fallback/local configs parseable |
| [`deploy/tests/validate-configs.sh`](../deploy/tests/validate-configs.sh) | Cross-config consistency | Port, env-var, tfvars, and config-shape invariants |
