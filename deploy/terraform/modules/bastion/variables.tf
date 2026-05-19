variable "compartment_id" {
  description = "OCI compartment OCID that owns the bastion resource"
  type        = string
  sensitive   = true
}

variable "target_subnet_id" {
  description = "OCID of the subnet the bastion's /28 service endpoint is carved from — must contain the target instance(s) for intra-subnet reachability"
  type        = string

  validation {
    condition     = length(var.target_subnet_id) > 0
    error_message = "target_subnet_id must be a non-empty subnet OCID."
  }
}
