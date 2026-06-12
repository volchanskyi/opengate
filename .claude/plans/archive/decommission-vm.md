# Decommission the compose VM — finalize the OKE cutover

**Status:** Completed — compose VM decommissioned 2026-06-10 (commit `be98b5e`). Archived 2026-06-10.

## Context

Phase 13b (compose→OKE cutover, ADR-030/ADR-034) is effectively complete and has
been stable for 4 days: the app + all 7 monitoring services run on the OKE cluster,
`opengate.cloudisland.net/healthz` → `ok`, `status.*` is up, `quic.*` →
`163.192.3.217` (the OKE worker node), `K8S_CUTOVER=true`. Re-verified live at the
start of this session.

The **only remaining tail** is the old compose VM `163.192.34.124`
(`oci_core_instance.opengate`, "opengate-server"), kept running purely as a rollback
path. **Nothing points to it** — DNS, agents, and CD all target the cluster. This plan
destroys it and repoints the operator tooling that still references it.

This is **irreversible**: destroying the instance releases its boot volume + public IP.
Post-decommission rollback = terraform-recreate the VM + `docker compose up` from git +
restore a `pg_dump` (runbook `docs/Kubernetes-Migration.md` (removed) §6/Rollback).
In-cluster pg_dump backups already run (the `opengate-postgres-backup` CronJob, last
success ~9h ago), so the cluster data itself is independently protected.

## Decisions (confirmed with user)

1. **Drive end-to-end, including the destroy** — no per-step confirmation; I still
   print the `terraform plan` and self-abort if it shows anything beyond the instance
   (+ the bastion repoint) being destroyed.
2. **Bastion: repoint to the OKE node** (not removed) — keep `make ssh` for node-level
   debugging; its target subnet + IP move from the VM to the worker node.
3. **`/observe`: repoint to the cluster inline** — rewrite to query in-cluster
   monitoring via `kubectl`, so the skill keeps working through the cutover.

## Execution sequence

Ordering keeps committed code ≈ live state: destroy **before** committing the terraform
edits (otherwise the nightly drift job sees a pending-destroy as drift).

1. **Safety snapshot.** `kubectl -n opengate exec opengate-postgres-0 -- pg_dump` of the
   live cluster DB → timestamped local file. Belt-and-suspenders over the existing
   backup CronJob, taken before deleting the last non-cluster host.
2. **(Best-effort) graceful stop on the VM.** Via bastion `make ssh` (or JIT NSG rule),
   `docker compose down` app + monitoring stacks under `/opt/opengate`. Optional courtesy
   — the destroy nukes the host regardless and the VM is fully idle; do **not** let
   bastion-SSH flakiness block the destroy.
3. **Edit terraform + CI** (details below) — removes the compute instantiation, repoints
   the bastion, drops the dead outputs/test/CI-check.
4. **Destroy (IRREVERSIBLE).** `terraform -chdir=deploy/terraform plan -out=tf.plan` →
   read it, confirm it is **exactly**: destroy `module.compute.oci_core_instance.opengate`
   (+ bastion in-place/replace for the subnet repoint) and **nothing protected**
   (no vcn/subnet/security_list/nsg/bucket) → `terraform apply tf.plan`.
5. **Verify cluster unaffected** (see Verification).
6. **Edit operator tooling + docs** (bastion-session.sh, `make tunnel`/`ssh`, `/observe`,
   docs/Infrastructure.md), restore the accidentally-emptied NOTES.txt, update phases.
7. **/precommit → commit → /refactor → pull --rebase → push** (the `integration.tftest.hcl`
   edit satisfies the TDD gate for the `.tf` source edits).

## Files to modify

### Terraform (core teardown + bastion repoint)
- **[deploy/terraform/main.tf](../../deploy/terraform/main.tf)** — delete the `module "compute"`
  block and its `moved { … oci_core_instance.opengate }` block; change `module.bastion`
  `target_subnet_id` from `module.networking.subnet_id` → `module.networking.oke_node_subnet_id`.
- **[deploy/terraform/outputs.tf](../../deploy/terraform/outputs.tf)** — delete
  `instance_public_ip`, `instance_id`, `instance_private_ip` (all reference `module.compute`).
- **[deploy/terraform/tests/integration.tftest.hcl](../../deploy/terraform/tests/integration.tftest.hcl)** —
  delete `run "instance_attached_to_cd_nsg"` (references `module.compute`); retitle/retext
  `run "bastion_targets_compute_subnet"` → node subnet (assertion is bastion_name only, still valid).
- **Keep** `modules/compute/` — it stays a tested, reusable rollback path; only the root
  instantiation is removed. Its own `free_tier.tftest.hcl` (mock provider) keeps passing.

### CI
- **[.github/workflows/ci.yml](../../.github/workflows/ci.yml)** (~L822) — drop `instance_id`
  from the `for out in instance_id cd_nsg_id` output-sensitivity loop → just `cd_nsg_id`.

### Operator tooling (repoint VM → OKE node / cluster)
- **[deploy/scripts/bastion-session.sh](../../deploy/scripts/bastion-session.sh)** — replace the
  `terraform output -raw instance_private_ip` target lookup with a dynamic node-IP lookup
  via `oci ce node-pool get --node-pool-id "$(terraform … output -raw oke_node_pool_id)"`
  → first node's `private-ip`. Preserve `tunnel`/`ssh`/`diagnose` subcommands + cache format.
- **Makefile `tunnel`/`ssh` targets** — `make ssh` → node shell (bastion). `make tunnel`:
  the monitoring UIs are now ClusterIP services, not node host-ports, so the bastion can't
  reach them — repoint `make tunnel` to `kubectl -n monitoring port-forward
  svc/monitoring-grafana 3000:3000` + `svc/monitoring-uptime-kuma 3001:3001`.

### `/observe` skill (rewrite VM/docker → cluster/kubectl)
- **[.claude/skills/observe/SKILL.md](../../.claude/skills/observe/SKILL.md)**:
  - Prerequisites: SSH-to-VM check → `kubectl config current-context` + `kubectl get nodes`.
  - §1 Metrics: `kubectl -n monitoring exec statefulset/monitoring-victoriametrics --
    wget -qO- 'http://127.0.0.1:8428/api/v1/query…'` (PromQL tables unchanged; node-exporter
    metrics now describe the OKE node).
  - §2 Logs: `kubectl -n monitoring exec statefulset/monitoring-loki -- wget …:3100…`.
    Relabel LogQL from `{container="opengate-server"}` → `{namespace="opengate"}` (+ `app`/
    `container` per the promtail `role: pod` scheme); staging = `{namespace="opengate-staging"}`;
    replace the dead `opengate-caddy` reverse-proxy rows with `ingress-nginx`
    (`-n ingress-nginx`). Exact `app`/`container` label values verified live via
    `/loki/api/v1/labels` during the rewrite.
  - §3 Health: `docker ps/logs/stats` → `kubectl -n opengate get pods` /
    `kubectl logs deploy/opengate-server` / health via the ingress
    `https://opengate.cloudisland.net/api/v1/health`; resource usage stays PromQL.
  - §4 Local WSL agent diagnostics: unchanged.
  - §5 Playbooks: swap VPS-container / Caddy references for cluster-pod / ingress-nginx.

### Docs + state
- **[docs/Infrastructure.md](../../docs/Infrastructure.md)** — "Operator access via OCI Bastion"
  section: target VM → OKE node; drop the now-removed `instance_id`/`instance_public_ip`
  commands + the `nmap` public-SSH check; monitoring-UI access → `kubectl port-forward` /
  the `status.*` ingress.
- **[deploy/helm/opengate/templates/NOTES.txt](../../deploy/helm/opengate/templates/NOTES.txt)** —
  restore from git (working-tree copy was accidentally emptied; content — ingress-nginx/
  cert-manager prereqs, Secret, DNS, verify — is still accurate). Veto-able.
- **[.claude/phases.md](../phases.md)** — mark Phase 13b cutover fully complete (VM decommissioned).
- Memory `oke-cutover-in-progress.md` → rewrite as "cutover complete". No new ADR (this
  finalizes an already-decided cutover, not a new decision).

## Verification

- **Plan gate (pre-destroy):** `terraform plan` shows only the instance destroyed (+ bastion
  repoint); **abort** if any vcn/subnet/security_list/nsg/bucket is in the destroy set.
- **Post-destroy, cluster intact:**
  - `kubectl -n opengate get pods` → `opengate-server` + `opengate-postgres-0` Running.
  - `curl -fsS https://opengate.cloudisland.net/healthz` → `ok`; `status.*` reachable.
  - `opengate_agents_connected` via the rewritten `/observe` §1 holds at its pre-destroy value
    (agents still connected to the node).
  - `oci compute instance get` on the old OCID → `TERMINATED` (or 404).
- **Tooling:** `make terraform-test` green (removed run + repointed bastion); `terraform output`
  no longer lists `instance_*`; `make ssh` opens a shell on the OKE node (if the OCI Bastion
  plugin isn't enabled on OKE nodes, record it as a one-line follow-up — does **not** block
  the destroy); `make tunnel` reaches Grafana/Kuma via port-forward.
- **`/observe` smoke:** run one PromQL instant query + one LogQL range query through the new
  kubectl path and confirm non-empty results.
- **Gauntlet:** `/precommit` green (it re-runs terraform-test + the sensitivity grep + lints).

## Risks / open items (non-blocking)
- **Bastion plugin on OKE nodes:** Managed SSH needs the OCI Cloud Agent Bastion plugin on
  the target. OKE worker nodes may not have it enabled by default — verify during step 6; if
  absent, enabling it is a small node-pool-config follow-up (the node also has a public IP as
  a fallback). Not a blocker for the VM destroy.
- **CD compose job:** grep confirms no workflow reads `instance_*` outputs (CD already targets
  OKE); re-confirm no dead VM-deploy job during verification.
