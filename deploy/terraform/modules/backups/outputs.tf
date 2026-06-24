output "bucket_name" {
  description = "Name of the Postgres backup bucket"
  value       = oci_objectstorage_bucket.this.name
}

output "bucket_ocid" {
  description = "OCID of the Postgres backup bucket"
  value       = oci_objectstorage_bucket.this.bucket_id
  sensitive   = true
}

output "policy_ocid" {
  description = "OCID of the opengate-os-lifecycle IAM policy"
  value       = oci_identity_policy.os_lifecycle.id
  sensitive   = true
}
