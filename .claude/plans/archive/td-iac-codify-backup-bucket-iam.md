# Micro-Plan: Codify Backup Bucket + IAM Policy in Terraform

**Register entry:** [techdebt.md](../../techdebt.md) — "ADR-035 … residual external
follow-ups", item 3 (IaC drift). **Master:** `techdebt-paydown-master.md`.
**Branch:** `dev`. **Owner:** infra (Terraform/OCI).

## 1. Problem

The Postgres backup **bucket**, its **lifecycle** rule, and the
`opengate-os-lifecycle` **IAM policy** were created imperatively via the `oci` CLI
(recorded in [NOTES.txt](../../../deploy/helm/opengate/templates/NOTES.txt)), not in
[`deploy/terraform/`](../../../deploy/terraform/) — live infra has drifted from IaC.
Reconciling means **importing** the existing live resources into Terraform state, never
recreating them.

This micro-plan covers **only** the codebase IaC item. The other ADR-035 residuals
(external uptime-SaaS account, Cloudflare `status.` DNS) are user-owned and stay in the
register.

## 2. Two-phase split (critical — read first)

CI lints Terraform with `terraform init -backend=false`
([ci.yml](../../../.github/workflows/ci.yml) `config-lint`), so **CI never touches OCI
state**. Therefore:

- **Phase A (this PR, in CI):** author the module + `terraform test`; `fmt`/`validate`/
  `tflint`/`test` pass with `-backend=false`. No live resource touched.
- **Phase B (operator, out-of-band):** with remote-backend creds, run `terraform import`
  for each live resource, then `terraform plan` and confirm **zero changes**. Attach the
  no-change plan to the PR/issue as evidence. This is an operator action (CI has no OCI
  creds), and the post-merge `terraform plan` CI job (ci.yml plan-against-backend) must
  then also show no drift.

## 3. File inventory

| File | Change |
|---|---|
| `deploy/terraform/modules/backups/main.tf` | **New module.** `oci_objectstorage_bucket` (private), `oci_objectstorage_object_lifecycle_policy` (match the live retention days), `oci_identity_policy` "opengate-os-lifecycle" (statements **copied verbatim** from the live policy — see §4). |
| `deploy/terraform/modules/backups/variables.tf` | Inputs: `compartment_ocid`, `namespace`, `bucket_name`, `lifecycle_days`, policy statement inputs. |
| `deploy/terraform/modules/backups/outputs.tf` | `bucket_name`, `bucket_ocid`, `policy_ocid`. |
| `deploy/terraform/modules/backups/versions.tf` | `required_providers { oci }` pin (mirror [modules/oke/versions.tf](../../../deploy/terraform/modules/oke/versions.tf)). |
| `deploy/terraform/modules/backups/tests/backups.tftest.hcl` | **New** `terraform test` (mirror [modules/oke/tests/free_tier.tftest.hcl](../../../deploy/terraform/modules/oke/tests/free_tier.tftest.hcl)): assert bucket is private, lifecycle present, policy least-privilege; runs with `-backend=false` (no apply against OCI). |
| [`deploy/terraform/main.tf`](../../../deploy/terraform/main.tf) | `module "backups" { source = "./modules/backups" … }` (mirror the existing `module "networking"/"oke"` blocks). |
| [`deploy/terraform/variables.tf`](../../../deploy/terraform/variables.tf), [`outputs.tf`](../../../deploy/terraform/outputs.tf) | Wire the new inputs/outputs. |
| [`docs/Infrastructure.md`](../../../docs/Infrastructure.md) | Note backups infra is now in Terraform; the PAR remains a runtime credential (out of git/state). |

## 4. Approach

1. **Capture the live truth** (operator, read-only): `oci os bucket get`,
   `oci os object-lifecycle-policy get`, `oci iam policy get --policy-id <…>` for
   `opengate-os-lifecycle`. Record exact names, namespace, compartment, lifecycle days,
   and every policy statement.
2. **Author** the module to reproduce that config **exactly** (no broadening of IAM).
3. **Phase A validation (must pass, matches CI):**
   ```
   terraform -chdir=deploy/terraform fmt -check -recursive
   terraform -chdir=deploy/terraform init -backend=false
   terraform -chdir=deploy/terraform validate
   tflint --chdir=deploy/terraform --format=compact
   terraform -chdir=deploy/terraform test          # requires TF >= 1.7
   ```
4. **Phase B (operator, remote backend):**
   ```
   terraform -chdir=deploy/terraform init -backend-config=backend.tfbackend
   terraform import module.backups.oci_objectstorage_bucket.this <namespace>/<bucket>
   terraform import module.backups.oci_objectstorage_object_lifecycle_policy.this <namespace>/<bucket>
   terraform import module.backups.oci_identity_policy.os_lifecycle <policy_ocid>
   terraform plan        # MUST be: No changes. Attach output.
   ```
   Iterate the HCL until the plan is a no-op (adjust to match live, never `apply` a
   destroy/recreate).
5. `/precommit` → commit → `/refactor` → `/precommit` → commit → push (Phase A files).

## 5. Acceptance criteria / DoD

- [ ] **Phase A:** `fmt -check`, `validate`, `tflint`, and `terraform test` all green in
      CI with `-backend=false`.
- [ ] **Phase B:** post-import `terraform plan` shows **No changes** (bucket + data
      untouched); the no-change plan output is attached as evidence.
- [ ] No secret/PAR/pre-auth URL in any committed `.tf` or state-tracked output.
- [ ] IAM statements match the live `opengate-os-lifecycle` verbatim (no broadening).
- [ ] The post-merge CI `terraform plan` job shows no drift.
- [ ] `/precommit` green.

## 6. NFRs

- **Security:** least-privilege IAM (verbatim copy, not widened); bucket private; PAR
  excluded from git/state.
- **Maintainability:** infra parity — future changes go through Terraform review + test.
- **Reliability:** import-not-recreate; verified via no-change plan before sign-off.

## 7. Reviewer/QA checklist

- [ ] Module mirrors the existing module/versions/test structure (oke/networking).
- [ ] `terraform test` added and green with `-backend=false`.
- [ ] No-change `terraform plan` evidence attached (Phase B).
- [ ] No OCIDs hard-coded outside variables; no PAR/token committed.
- [ ] `.tflint.hcl` rules pass; provider pin present.

## 8. Risks

- Object Storage import addressing (`<namespace>/<bucket>`) and lifecycle/policy import
  IDs — verify on `plan`, never blind `apply`.
- Remote-state backend must be reachable for Phase B; coordinate creds with the operator.
