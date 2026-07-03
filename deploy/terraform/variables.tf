variable "tenancy_ocid" {
  description = "OCI tenancy OCID"
  type        = string
  sensitive   = true
}

variable "user_ocid" {
  description = "OCI user OCID"
  type        = string
  sensitive   = true
}

variable "fingerprint" {
  description = "OCI API key fingerprint"
  type        = string
  sensitive   = true
}

variable "private_key_path" {
  description = "Path to OCI API private key PEM file"
  type        = string
  sensitive   = true
}

variable "region" {
  description = "OCI region"
  type        = string
  default     = "us-sanjose-1"
}

variable "compartment_ocid" {
  description = "OCI compartment OCID (defaults to tenancy root)"
  type        = string
  default     = ""
  sensitive   = true
}

variable "ssh_public_key_path" {
  description = "Path to SSH public key for instance access"
  type        = string
  default     = "~/.ssh/id_ed25519.pub"
}

variable "ssh_allowed_cidr" {
  description = "CIDR block allowed for SSH access (operator IP)"
  type        = string
  sensitive   = true

  validation {
    condition     = var.ssh_allowed_cidr != "0.0.0.0/0"
    error_message = "ssh_allowed_cidr must be a specific operator CIDR, never 0.0.0.0/0."
  }
}

# --- OKE (Phase 13b cutover) -------------------------------------------------
# Defaults are the live values resolved at wiring time (region us-sanjose-1,
# 2026-06-06): `oci ce cluster-options get` for the version and
# `oci ce node-pool-options get` for the aarch64 OKE image. Refresh when OCI
# deprecates the image (the node pool will report an unavailable-image error).

variable "oke_kubernetes_version" {
  description = "OKE control-plane + node-pool Kubernetes version. Resolve via `oci ce cluster-options get --cluster-option-id all`."
  type        = string
  default     = "v1.34.2"
}

variable "oke_node_image_id" {
  description = "OCID of the aarch64 OKE worker image matching oke_kubernetes_version. Resolve via `oci ce node-pool-options get --node-pool-option-id all`."
  type        = string
  default     = "ocid1.image.oc1.us-sanjose-1.aaaaaaaalih7kqgecormb75syw4mwchfbzfjxtz4skym66ansmw2utfxjnsq"
}

variable "oke_availability_domain" {
  description = "Availability domain for the OKE node pool. Resolve via `oci iam availability-domain list`."
  type        = string
  default     = "pQib:US-SANJOSE-1-AD-1"
}

# --- Off-cluster Postgres backups (ADR-035) ----------------------------------
# Defaults match the live imperatively-created bucket + retention rule so the
# Phase-B `terraform import` reconciliation plans a no-op.

variable "backup_bucket_name" {
  description = "Name of the off-cluster Postgres backup bucket."
  type        = string
  default     = "opengate-pg-backups"
}

variable "backup_lifecycle_days" {
  description = "Retention window in days for objects in the backup bucket — older objects are auto-deleted by the lifecycle rule."
  type        = number
  default     = 7
}
