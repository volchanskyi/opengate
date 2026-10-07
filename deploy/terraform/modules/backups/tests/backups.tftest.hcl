mock_provider "oci" {}

variables {
  compartment_ocid  = "ocid1.tenancy.oc1..fake"
  namespace         = "fakenamespace"
  bucket_name       = "opengate-pg-backups"
  lifecycle_days    = 7
  lifecycle_prefix  = "opengate-"
  policy_name       = "opengate-os-lifecycle"
  policy_statements = ["Allow service objectstorage-us-sanjose-1 to manage object-family in tenancy"]
}

run "bucket_is_private" {
  command = plan

  assert {
    condition     = oci_objectstorage_bucket.this.access_type == "NoPublicAccess"
    error_message = "Backup bucket access_type must remain NoPublicAccess — it stores Postgres dumps."
  }
}

run "bucket_wired_to_inputs" {
  command = plan

  assert {
    condition     = oci_objectstorage_bucket.this.name == var.bucket_name
    error_message = "Bucket name must equal var.bucket_name (matches the live imperatively-created bucket)."
  }

  assert {
    condition     = oci_objectstorage_bucket.this.namespace == var.namespace
    error_message = "Bucket namespace must equal var.namespace (resolved by the root module)."
  }

  assert {
    condition     = oci_objectstorage_bucket.this.compartment_id == var.compartment_ocid
    error_message = "Bucket compartment_id must equal var.compartment_ocid."
  }
}

run "lifecycle_rule_deletes_after_retention" {
  command = plan

  assert {
    condition     = one(oci_objectstorage_object_lifecycle_policy.this.rules).action == "DELETE"
    error_message = "The lifecycle rule action must be DELETE (auto-expire old dumps)."
  }

  assert {
    condition     = one(oci_objectstorage_object_lifecycle_policy.this.rules).is_enabled == true
    error_message = "The lifecycle rule must be enabled — a disabled rule silently disables retention."
  }

  assert {
    condition     = one(oci_objectstorage_object_lifecycle_policy.this.rules).time_unit == "DAYS"
    error_message = "Lifecycle time_unit must be DAYS."
  }

  # time_amount is Optional+Computed, so the mock provider leaves it unknown under plan.

  assert {
    condition     = one(oci_objectstorage_object_lifecycle_policy.this.rules).target == "objects"
    error_message = "Lifecycle target must be objects."
  }

  assert {
    condition     = one(one(oci_objectstorage_object_lifecycle_policy.this.rules).object_name_filter).inclusion_prefixes == toset([var.lifecycle_prefix])
    error_message = "Lifecycle rule must be scoped to the var.lifecycle_prefix inclusion prefix."
  }
}

run "iam_policy_is_least_privilege" {
  command = plan

  assert {
    condition     = oci_identity_policy.os_lifecycle.name == var.policy_name
    error_message = "IAM policy name must equal var.policy_name (matches the live opengate-os-lifecycle)."
  }

  assert {
    condition     = oci_identity_policy.os_lifecycle.statements == var.policy_statements
    error_message = "IAM policy statements must equal var.policy_statements verbatim — never broaden the live grant."
  }

  assert {
    condition     = alltrue([for s in oci_identity_policy.os_lifecycle.statements : !strcontains(lower(s), "all-resources")])
    error_message = "IAM policy must not grant manage all-resources — least privilege only."
  }
}

run "rejects_nonpositive_retention" {
  command = plan

  variables {
    lifecycle_days = 0
  }

  expect_failures = [var.lifecycle_days]
}

run "rejects_empty_policy_statements" {
  command = plan

  variables {
    policy_statements = []
  }

  expect_failures = [var.policy_statements]
}
