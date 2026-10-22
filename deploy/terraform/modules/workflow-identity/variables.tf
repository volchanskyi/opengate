variable "tenancy_id" {
  description = "OCID of the tenancy, where IAM users and groups live"
  type        = string
}

variable "compartment_id" {
  description = "OCID of the compartment holding the OKE cluster; the tenancy OCID for the root"
  type        = string
}

variable "name" {
  description = "Name shared by the workflow user and its group"
  type        = string
  default     = "opengate-workflows"
}
