variable "compartment_ocid" {
  description = "OCID of the compartment that owns the backup bucket and the lifecycle IAM policy (live: tenancy root)"
  type        = string
  sensitive   = true
}

variable "namespace" {
  description = "OCI Object Storage namespace, resolved by the root module via the oci_objectstorage_namespace data source"
  type        = string

  validation {
    condition     = length(var.namespace) > 0
    error_message = "namespace must be a non-empty Object Storage namespace."
  }
}

variable "bucket_name" {
  description = "Name of the off-cluster Postgres backup bucket (matches the live imperatively-created bucket)"
  type        = string
  default     = "opengate-pg-backups"
}

variable "lifecycle_days" {
  description = "Retention window in days — objects older than this are auto-deleted by the bucket lifecycle rule"
  type        = number
  default     = 7

  validation {
    condition     = var.lifecycle_days > 0
    error_message = "lifecycle_days must be a positive retention window; 0 would expire every dump immediately."
  }
}

variable "lifecycle_prefix" {
  description = "Object-name inclusion prefix the retention rule is scoped to"
  type        = string
  default     = "opengate-"
}

variable "policy_name" {
  description = "Name of the IAM policy granting the Object Storage service principal lifecycle permission"
  type        = string
  default     = "opengate-os-lifecycle"
}

variable "policy_description" {
  description = "Description of the lifecycle IAM policy"
  type        = string
  default     = "Allow Object Storage service principal to run lifecycle (auto-delete) on opengate buckets"
}

variable "policy_statements" {
  description = "IAM policy statements, copied verbatim from the live opengate-os-lifecycle policy — never broadened"
  type        = list(string)
  default     = ["Allow service objectstorage-us-sanjose-1 to manage object-family in tenancy"]

  validation {
    condition     = length(var.policy_statements) > 0
    error_message = "policy_statements must be non-empty — an empty grant silently stops the bucket lifecycle from running."
  }
}
