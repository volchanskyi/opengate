# The bucket carries no freeform_tags because the live bucket has none; tags would plan a change.

resource "oci_objectstorage_bucket" "this" {
  compartment_id = var.compartment_ocid
  namespace      = var.namespace
  name           = var.bucket_name

  # The bucket holds Postgres dumps, so it is never publicly readable.
  access_type  = "NoPublicAccess"
  storage_tier = "Standard"
  versioning   = "Disabled"
}

# Server-side retention deletes objects older than the window, independent of any node.
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

# Least-privilege grant that lets the Object Storage service principal run the bucket lifecycle.
resource "oci_identity_policy" "os_lifecycle" {
  compartment_id = var.compartment_ocid
  name           = var.policy_name
  description    = var.policy_description
  statements     = var.policy_statements
}
