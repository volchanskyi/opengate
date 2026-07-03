# Dormant artifacts to clean up

**Status:** Draft for architect evaluation — refined against a live OCI/OKE cross-check (2026-07-02).
**Type:** Cleanup plan for functionality replaced by the current production path. This
is **not** a future-feature backlog and does **not** target anything waiting on an
unimplemented plan.

## Context

The repo still carries artifacts from the pre-OKE production path: VPS/Docker Compose
application deployment, host-level deploy/rollback scripts, Docker Compose monitoring,
Compose-era monitoring config copies, a compiled multiserver E2E binary, and Terraform
rollback surfaces for the old compute shape. Current production **and** staging run on
OKE through Terraform + Helm with Kubernetes-native monitoring.

This plan turns the dormant-artifact audit into cleanup workstreams. It does not target
local/E2E Compose, guardrail tests, future implementation seams, or active Kubernetes
deployment assets.

## Live OCI / cluster cross-check (verified 2026-07-02)

Verified against the live OKE cluster (`context-c23expbbogq`) and OCI tenancy
(`us-sanjose-1`) so the decision gate rests on real cloud state, not repo config. The
local `deploy/terraform/terraform.tfstate` is a 0-byte stub — real state is the remote
S3-compat backend — so these facts come from `kubectl` and `oci ... list`, not state.

- **Both environments are Kubernetes.** Namespace `opengate` (production:
  `opengate-server` Deployment + `opengate-postgres` StatefulSet) and `opengate-staging`
  (`opengate-staging-server` + `opengate-staging-postgres`), plus `monitoring` (Grafana,
  VictoriaMetrics, Loki, postgres-exporter). **Nothing runs on Docker Compose** in either
  environment — the Compose app + monitoring bundles are dormant with zero live consumer.
- **Compose VM is already destroyed.** The only live compute instance is the OKE worker
  node (`oke-c23expbbogq-…`, `VM.Standard.A1.Flex`, RUNNING). The old
  `oci_core_instance.opengate` is gone. → **Item 5 (compute module) is a zero-live-impact
  code deletion**, not a cloud destroy.
- **Legacy network surfaces are STILL LIVE.** `opengate-vcn` (10.0.0.0/16) contains
  `opengate-public-subnet` (10.0.1.0/24), the `opengate-cd-deploy` NSG, and the
  `opengate-sl` security list — all dormant (no instance uses the public subnet now that
  the VM is gone), but each is a real live resource. → **Item 6 is a genuine
  `terraform apply` destroy of live cloud resources**, mandatory plan-review-before-apply.
- **The route table + internet gateway are shared and ACTIVE.** `oci_core_route_table.opengate`
  is reused by all three OKE subnets (`modules/networking/oke.tf:300,310,320`) and the IGW
  backs it. **They must NOT be removed** even though the dormant public subnet also
  references the route table.

## Decision gate

Classify each candidate before deletion. "Live blast radius" is the verified impact of a
`terraform apply` / merge — the column the first pass lacked.

| Candidate | Live blast radius | Default | Architect decision |
|---|---|---|---|
| `server/e2e-multiserver` binary | None (source `server/internal/multiserver/` already deleted) | Delete | Confirm no external release/debug flow consumes it. |
| Compose prod/staging app bundle + Caddyfiles | None (no live Compose) | Delete | Is the old app rollback path still a supported recovery mechanism? |
| VPS `deploy.sh` / `rollback.sh` (+ their test) | None (CD is Helm-only) | Delete | Does any operator runbook still invoke them? |
| Compose monitoring stack + Compose-era `loki/`,`promtail/`,`victoriametrics/` copies | None (Helm monitoring is live) | Delete | Confirm Helm monitoring fully replaces the Docker-log stack. |
| `deploy/tests/validate-configs.sh` + `policy/docker_compose/` | None (only validate the retired Compose bundle) | Delete | Confirm no residual invariant worth re-homing (e.g. env-var coverage). |
| compute module + `cloud-init.yaml` | **None** — VM already terminated | Remove unless rollback-by-VM is still supported | Reusable-rollback value vs. maintenance cost? |
| Legacy public subnet / `opengate-sl` SL / `cd_deploy` NSG | **Live destroy** of 3 real OCI resources | Remove only after reviewing the plan | Destroy the dormant break-glass surfaces, or keep them explicitly documented? |

## Cleanup order

### 1. Remove the high-confidence leftover binary

Scope:
- Delete `server/e2e-multiserver` (tracked 16 MB executable; the only tracked compiled
  binary in the tree).
- Reference sweep for `e2e-multiserver`, `OPENGATE_MULTISERVER_E2E`, `make e2e-multiserver`.

Why first: its source package `server/internal/multiserver/` is **already deleted** (the
Dormant Multi-Replica Teardown removed it); active Makefile/CI references are gone. Only
`CHANGELOG.md` (historical) and a frozen `.claude/baseline/pmat-tdg-baseline-2026-05-20.json`
snapshot still name `multiserver.go` — both are point-in-time records, **not** cleanup
targets; leave them.

Validation:
- `git ls-files server/e2e-multiserver` is empty after deletion.
- Reference sweep finds no active hits outside archived plans/ADRs/CHANGELOG/baseline.

### 2. Retire the legacy Compose production deployment path

Scope:
- Remove `deploy/docker-compose.yml`, `deploy/docker-compose.staging.yml`,
  `deploy/.env.example`, `deploy/postgres/init.sql`, and the Caddyfiles
  `deploy/caddy/Caddyfile` + `deploy/caddy/Caddyfile.staging`.
- Remove the Compose/Caddy validation this bundle feeds — **named surfaces**:
  - `Makefile` `lint-deploy`: the two `docker compose … config --quiet` lines (prod +
    prod/staging overlay) and the `caddy fmt/validate` block.
  - `.github/workflows/ci.yml`: `docker compose config` (≈L801–802), the Caddy install +
    "Validate Caddyfiles" steps (≈L805–824).
  - `policy/docker_compose/` (conftest `images.rego` + `images_test.rego`) — it only lints
    `docker-compose.yml` + `docker-compose.staging.yml`; orphaned once they go. Drop the
    `conftest test --policy policy/docker_compose …` line in the `iac-policy-custom` target.
- `deploy/tests/validate-configs.sh`: **delete outright.** Its only inputs are
  `.env.example`, `cloud-init.yaml`, `docker-compose[.staging].yml`, and the `opengate-sl`
  security list in `main.tf` — every one is retired here or in items 5–6. Unwire it from
  `Makefile:lint-deploy` (last line) and `ci.yml` ("Deploy config integration tests",
  ≈L851); drop its row from `docs/Infrastructure.md`. If any invariant is worth keeping
  (e.g. env-var coverage of a *Helm* values file), re-home it — but its current port /
  UFW / OCI-SL checks are all Compose-VM-shaped and die with the bundle.
- Keep `deploy/docker-compose.test.yml` — active local/E2E infra (`ci.yml:511`, `make e2e`).

Current replacement: `deploy/helm/opengate` + the Kubernetes CD jobs in
`.github/workflows/cd.yml`.

Validation: `make lint-k8s` green; E2E still uses only `docker-compose.test.yml`;
reference sweep for `docker-compose.staging.yml`, prod Caddyfiles, `opengate-caddy` has no
active-runtime hits.

### 3. Retire VPS deploy and rollback scripts

Scope:
- Delete `deploy/scripts/deploy.sh`, `deploy/scripts/rollback.sh`, and their test
  `scripts/tests/deploy-rollback.test.sh` (its only consumers).
- Split `deploy/scripts/common.sh` precisely. `smoke-test.sh` (still active, item below)
  sources it but calls **only** `log`, `fail`, `validate_mode`. Keep those three; remove
  the 12 Compose/sentinel/digest helpers that served `deploy.sh`/`rollback.sh`:
  `env_file`, `prev_tag_file`, `sentinel_file`, `read_sentinel_field`, `write_sentinel`,
  `inspect_digest`, `wait_healthy`, `container_name`, `verify_image`, `redeploy`,
  `set_env_var`, `compose_cmd`.

Current replacement: Helm upgrade + Helm rollback history is the deployment boundary;
`deploy/scripts/smoke-test.sh` stays active — it is invoked by `cd.yml` for staging
(≈L288) and production (≈L445) over a Kubernetes port-forward.

Validation: CD workflow invokes Helm/kubectl only; smoke tests still pass through the
Service port-forward; no active workflow invokes `deploy.sh`, `rollback.sh`, or host-side
`docker compose up`.

### 4. Retire the Compose monitoring stack and its config copies

Scope:
- Delete `deploy/docker-compose.monitoring.yml`, `deploy/.env.monitoring.example`,
  `deploy/promtail/promtail-config.yml`, `deploy/victoriametrics/scrape.yml`,
  `deploy/victoriametrics/alerts.yml`, and any old Caddy status-route wiring.
- **Resolve the loki/promtail duplication.** The Helm copies are canonical (mounted by the
  chart): `deploy/helm/monitoring/files/loki-config.yml`,
  `deploy/helm/monitoring/files/promtail-config.yaml`. The Compose-era copies
  `deploy/loki/loki-config.yml` and `deploy/promtail/promtail-config.yml` are dormant →
  delete them (make the Helm copy the single source).
- **DO NOT delete `deploy/grafana/provisioning/`.** Although the old
  `docker-compose.monitoring.yml` mounts it, it is now the **canonical** Grafana config
  source: the Helm `grafana.yaml` is templated from it, `NOTES.txt` builds the dashboards/
  alerting ConfigMaps `--from-file=deploy/grafana/provisioning/…`, and it is referenced by
  `docs/Monitoring.md`, `docs/Infrastructure.md`, ADR-014, and `ci-trend-retirement.test.sh`.
  It stays.
- Update monitoring docs/ADRs to drop Docker-log / VPS-monitoring runbook references.

Current replacement: `deploy/helm/monitoring`, Kubernetes pod-log discovery, Kubernetes
service discovery, and Grafana provisioning mounted by the chart.

Validation: `make lint-k8s` renders monitoring; reference sweep for
`docker-compose.monitoring.yml`, Docker-socket Promtail, `opengate-uptime-kuma`, and static
Compose scrape targets has no active hits; monitoring docs describe only the K8s path.

### 5. Decide and clean up the dormant compute rollback module

Verified: the compute VM is **already terminated** (only the OKE node is live) and the
module is **not instantiated** (`main.tf:63` comment; root `outputs.tf` has no
instance/compute outputs). So this is a **code-only deletion with zero live blast radius**.

Scope if architect approves deletion:
- Delete `deploy/terraform/modules/compute/` (incl. `tests/free_tier.tftest.hcl`).
- Delete `deploy/terraform/cloud-init.yaml` — its **only** consumer is the compute
  module's `user_data` (`modules/compute/main.tf:48`); it is otherwise read solely by the
  now-deleted `validate-configs.sh` (item 2).
- Remove any root docs/outputs presenting the VM path as currently available.

Alternative if architect keeps rollback-by-VM: rename/document `modules/compute` as an
explicit archived rollback asset, state it is not instantiated, and scope its test to the
rollback module.

Validation: `make terraform-test` green; root Terraform plan shows no unintended
destroy/create; docs no longer imply the old compute VM is production.

### 6. Decide and clean up legacy network rollback surfaces

Verified live: destroying these removes **3 real OCI resources** — mandatory
plan-review-before-apply. The shared route table + IGW are reused by OKE and MUST stay.

Scope if architect approves removal — **named resources** in
`deploy/terraform/modules/networking/main.tf`:
- `oci_core_subnet.opengate_public` (public subnet, `main.tf:139`).
- `oci_core_security_list.opengate` (`opengate-sl`, `main.tf:41`) — the old VM public-edge
  ACL (HTTP/80, 443, HTTP/3 UDP 443, MPS 4433, QUIC 9090), attached only to the public
  subnet.
- `oci_core_network_security_group.cd_deploy` (`main.tf:35`) — empty rollback NSG.
- Their **three** `moved` blocks in root `main.tf` (`cd_deploy`, `opengate_public`,
  `opengate-sl`) — keep the vcn/igw/route_table `moved` blocks.
- Root/module outputs: `public_subnet_id`, `cd_nsg_id`, `security_list_id`,
  `public_subnet_security_list_ids` (`modules/networking/outputs.tf`).
- The `cd_nsg_id` **sensitive-output grep guard** in `Makefile:lint-deploy` (asserts
  `sensitive = true` on the retired NSG output).
- Test assertions over the dormant surfaces: `deploy/terraform/tests/integration.tftest.hcl`
  (asserts the public subnet's `security_list_ids == [security_list_id]`) and the
  public-subnet parts of `deploy/terraform/modules/networking/tests/security.tftest.hcl`.

Alternative if architect keeps break-glass: label them intentional retained rollback
resources, exclude from future dormant audits, and record a clear retirement condition.

Current replacement: OKE API/node/LB subnets + NSGs, plus Bastion access to the OKE node
subnet.

Validation: review the plan before apply — **abort on any unexpected VCN/route-table/IGW/
OKE-subnet/OKE-NSG destroy**; only the 3 named resources should be destroyed.
`make terraform-test` and CI sensitivity/output checks green.

## Out of scope (verified still active — do not touch)

- `deploy/docker-compose.test.yml` and test-only Compose wiring (`ci.yml:511`, `make e2e`).
- `deploy/scripts/wait-for-server.sh` (`ci.yml:554`).
- `deploy/scripts/smoke-test.sh` (`cd.yml` staging/prod), except stale comments/imports left
  by removing Compose-only `common.sh` helpers.
- `deploy/grafana/provisioning/` — canonical Grafana config source (see item 4).
- Shared `oci_core_route_table.opengate` + `oci_core_internet_gateway.opengate` (reused by
  OKE subnets).
- Active `SessionRegistry` / `InProcessRegistry` relay code (ADR-023).
- Protocol rejection tests and compatibility guardrails.
- `.claude/baseline/*` snapshots (frozen point-in-time records; the stale `multiserver.go`
  reference inside them is expected).
- The **frozen** ADR log `docs/Architecture-Decision-Records.md` (ADR-001–012) — never
  edited even where it mentions Compose/VPS. Docs-update touches only mutable surfaces
  (`docs/Infrastructure.md`, `docs/Monitoring.md`, `docs/adr/ADR-014`, `docs/adr/ADR-018`).
- Future implementation plans that do not represent replaced functionality.

## Acceptance criteria

- Architect decisions (esp. items 5–6) recorded before any irreversible infra cleanup.
- Each removal also removes its tests, CI validation, conftest policy, docs, Makefile
  references, Terraform outputs, and `moved` blocks.
- Current production paths still pass validation: `make lint-k8s` (Helm render),
  `make terraform-test`, CD smoke-test assumptions, and local/E2E Compose.
- Item 6's `terraform plan` is human-reviewed and destroys **only** the 3 named legacy
  resources — no OKE/shared-networking collateral.
- Final dangling-reference sweep has no active hits for retired artifacts (archived
  plans/ADRs/CHANGELOG/baseline excepted).
- `/precommit` passes before the cleanup is committed.
