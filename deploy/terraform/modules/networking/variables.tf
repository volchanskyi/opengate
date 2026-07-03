variable "compartment_id" {
  description = "OCI compartment OCID that owns the VCN and its children"
  type        = string
  sensitive   = true
}

variable "ssh_allowed_cidr" {
  description = "CIDR block allowed to reach TCP 22 on the OKE worker-node break-glass rule (operator IP)"
  type        = string
  sensitive   = true

  validation {
    condition     = var.ssh_allowed_cidr != "0.0.0.0/0"
    error_message = "ssh_allowed_cidr must be a specific operator CIDR, never 0.0.0.0/0."
  }
}
