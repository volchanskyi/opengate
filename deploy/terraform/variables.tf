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

variable "instance_shape" {
  description = "OCI compute shape"
  type        = string
  default     = "VM.Standard.A1.Flex"
}

variable "instance_ocpus" {
  description = "Number of OCPUs for the instance"
  type        = number
  default     = 2
}

variable "instance_memory_gb" {
  description = "Memory in GB for the instance"
  type        = number
  default     = 12
}

variable "boot_volume_gb" {
  description = "Boot volume size in GB"
  type        = number
  default     = 50
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
