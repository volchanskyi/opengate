# Backups submodule

Codifies the off-cluster Postgres backup substrate ([ADR-035](../../../../docs/adr/ADR-035-oke-free-tier-block-volume-remediation.md)) that was originally stood up imperatively with the `oci` CLI:

- a **private** Object Storage bucket (`opengate-pg-backups`, `NoPublicAccess`),
- a server-side **retention lifecycle** rule (DELETE objects older than `lifecycle_days`, scoped to the `opengate-` prefix) — the replacement for the old host-side `find -mtime` cron, and
- the least-privilege **IAM policy** (`opengate-os-lifecycle`) that lets the Object Storage service principal run that lifecycle.

The write-only pre-authenticated request (PAR) the server uses to push dumps is a **runtime credential**, not infrastructure — it lives in the Kubernetes Secret (`BACKUP_PAR_URL`), never in Terraform code or state.

## Reconcile, don't recreate

The live resources already exist. This module is reconciled into state by **importing** them, never by `apply`-creating fresh ones (that would drop the backup data). The bucket deliberately carries no `freeform_tags` because the live bucket has none — adding any would make the import plan show a change instead of a no-op.

The `s3` backend needs `AWS_REQUEST_CHECKSUM_CALCULATION=when_required` on every
invocation (see [docs/Infrastructure.md](../../../../docs/Infrastructure.md) → "Required env var").

```bash
export AWS_REQUEST_CHECKSUM_CALCULATION=when_required
terraform -chdir=deploy/terraform init -backend-config=backend.tfbackend
terraform import 'module.backups.oci_objectstorage_bucket.this' n/<namespace>/b/opengate-pg-backups
terraform import 'module.backups.oci_objectstorage_object_lifecycle_policy.this' n/<namespace>/b/opengate-pg-backups/l
terraform import 'module.backups.oci_identity_policy.os_lifecycle' <policy_ocid>
terraform -chdir=deploy/terraform plan   # MUST be: No changes.
```

The first plan after import shows two cosmetic in-place updates on `compartment_id`
(Terraform re-marking it `sensitive` now that it flows from a sensitive variable —
the value is unchanged). A single `terraform apply` of that 0-add/0-destroy plan
settles the state metadata, after which the plan is a clean **No changes**.

## Inputs

| Variable | Type | Purpose |
|---|---|---|
| `compartment_ocid` | string (sensitive) | Compartment that owns the bucket and policy (live: tenancy root). |
| `namespace` | string | Object Storage namespace, resolved by the root module. |
| `bucket_name` | string | Backup bucket name (default `opengate-pg-backups`). |
| `lifecycle_days` | number | Retention window in days (default `7`). |
| `lifecycle_prefix` | string | Object-name inclusion prefix the retention rule covers (default `opengate-`). |
| `policy_name` | string | IAM policy name (default `opengate-os-lifecycle`). |
| `policy_description` | string | IAM policy description. |
| `policy_statements` | list(string) | IAM statements, verbatim from the live policy — never broadened. |

## Outputs

| Output | Purpose |
|---|---|
| `bucket_name` | Backup bucket name. |
| `bucket_ocid` | Bucket OCID (sensitive). |
| `policy_ocid` | OCID of the `opengate-os-lifecycle` policy (sensitive). |

## Test coverage

- [`tests/backups.tftest.hcl`](tests/backups.tftest.hcl) — bucket is private, lifecycle rule deletes after the retention window scoped to the prefix, IAM policy is the verbatim least-privilege statement set (no `all-resources` broadening), and the retention/statement variable validations reject a zero window and an empty grant. Runs against a mock provider — no OCI creds required.
