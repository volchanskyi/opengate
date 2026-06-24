# Off-cluster Postgres backup substrate (ADR-035). These three resources were
# originally created imperatively with the oci CLI (recorded in the Helm chart's
# NOTES.txt) and are reconciled into Terraform by *importing* the live resources
# in a separate operator step — never recreating them, which would drop the
# backup data.
#
# The bucket carries no freeform_tags because the live bucket has none: adding
# tags here would make the reconciling `terraform import` plan a no-op no longer
# (it would show a tag change requiring an apply). Match live exactly.

resource "oci_objectstorage_bucket" "this" {
  compartment_id = var.compartment_ocid
  namespace      = var.namespace
  name           = var.bucket_name

  # Holds Postgres dumps — never publicly readable.
  access_type  = "NoPublicAccess"
  storage_tier = "Standard"
  versioning   = "Disabled"
}

# Server-side retention: delete objects older than the retention window. This
# replaced a host-side `find -mtime` cron, so retention survives a node being
# rebuilt.
resource "oci_objectstorage_object_lifecycle_policy" "this" {
  namespace = var.namespace
  bucket    = oci_objectstorage_bucket.this.name

  rules {
    name        = "expire-old"
    action      = "DELETE"
    is_enabled  = true
    time_amount = var.lifecycle_days
    time_unit   = "DAYS"
    target      = "objects"

    object_name_filter {
      inclusion_prefixes = [var.lifecycle_prefix]
    }
  }
}

# Least-privilege grant that lets the Object Storage service principal execute
# the lifecycle (auto-delete) on the bucket. Statements are copied verbatim from
# the live policy; broadening them (e.g. manage all-resources) is a security
# regression caught by tests/backups.tftest.hcl.
resource "oci_identity_policy" "os_lifecycle" {
  compartment_id = var.compartment_ocid
  name           = var.policy_name
  description    = var.policy_description
  statements     = var.policy_statements
}
