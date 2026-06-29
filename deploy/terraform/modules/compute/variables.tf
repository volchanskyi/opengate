variable "compartment_id" {
  description = "OCI compartment OCID that owns the instance and looks up Ubuntu images"
  type        = string
  sensitive   = true
}

variable "tenancy_ocid" {
  description = "OCI tenancy OCID — used to enumerate availability domains across the tenancy"
  type        = string
  sensitive   = true
}

variable "subnet_id" {
  description = "OCID of the public subnet the instance VNIC is attached to"
  type        = string
}

variable "nsg_ids" {
  description = "OCIDs of network security groups attached to the instance's primary VNIC"
  type        = list(string)
}

variable "instance_shape" {
  description = "OCI compute shape — must be VM.Standard.A1.Flex to qualify as Always Free"
  type        = string
}

variable "instance_ocpus" {
  description = "Number of OCPUs for the instance (Always Free A1.Flex cap is 2 OCPUs total per tenancy)"
  type        = number
}

variable "instance_memory_gb" {
  description = "Memory in GB for the instance (Always Free A1.Flex cap is 12 GB total per tenancy)"
  type        = number
}

variable "boot_volume_gb" {
  description = "Boot volume size in GB (Always Free cap is 200 GB total per tenancy)"
  type        = number
}

variable "ssh_public_key_path" {
  description = "Path to SSH public key written into instance metadata.ssh_authorized_keys"
  type        = string
}

variable "cloud_init_path" {
  description = "Path to the cloud-init YAML file embedded as base64 user_data"
  type        = string
}
