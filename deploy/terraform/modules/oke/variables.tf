variable "compartment_id" {
  description = "OCID of the compartment that owns the OKE cluster and node pool."
  type        = string
}

variable "cluster_name" {
  description = "Display name for the OKE cluster."
  type        = string
  default     = "opengate-oke"
}

variable "kubernetes_version" {
  description = "Kubernetes version for the control plane and node pool (e.g. v1.31.1). Resolve available versions via `oci ce cluster-options get --cluster-option-id all`."
  type        = string
}

variable "environment" {
  description = "Environment tag (staging / production) applied as a freeform tag."
  type        = string
  default     = "production"
}

# --- Networking (provided by the root/networking module) --------------------

variable "vcn_id" {
  description = "OCID of the VCN hosting the cluster."
  type        = string
}

variable "api_endpoint_subnet_id" {
  description = "OCID of the subnet for the Kubernetes API endpoint."
  type        = string
}

variable "node_subnet_id" {
  description = "OCID of the subnet that worker nodes join."
  type        = string
}

variable "service_lb_subnet_ids" {
  description = "OCIDs of subnet(s) for Service type=LoadBalancer. Empty list keeps L4 on hostPort (single-node)."
  type        = list(string)
  default     = []
}

variable "api_nsg_ids" {
  description = "NSG OCIDs applied to the API endpoint (OKE-compliant rules supplied by the networking module)."
  type        = list(string)
  default     = []
}

variable "node_nsg_ids" {
  description = "NSG OCIDs applied to worker nodes."
  type        = list(string)
  default     = []
}

variable "endpoint_is_public" {
  description = "Expose the API endpoint with a public IP. Public simplifies CD's kubectl reach; private (false) routes via the OCI Bastion (modules/bastion, ADR-018)."
  type        = bool
  default     = true
}

variable "pods_cidr" {
  description = "CIDR for the pod overlay network (must not overlap the VCN)."
  type        = string
  default     = "10.244.0.0/16"
}

variable "services_cidr" {
  description = "CIDR for ClusterIP Services (must not overlap the VCN or pods_cidr)."
  type        = string
  default     = "10.96.0.0/16"
}

# --- Node pool (Always-Free A1.Flex) ----------------------------------------

variable "availability_domain" {
  description = "Availability domain name the node pool places workers in."
  type        = string
}

variable "node_image_id" {
  description = "OCID of an OKE-compatible Oracle Linux image for the node shape. Resolve via `oci ce node-pool-options get --node-pool-option-id all`."
  type        = string
}

variable "node_shape" {
  description = "Worker node shape. Pinned to the Always-Free ARM64 flex shape."
  type        = string
  default     = "VM.Standard.A1.Flex"
}

variable "node_pool_size" {
  description = "Number of worker nodes. Bounded so total OCPU/memory stays inside the Always-Free 4 OCPU / 24 GB cap."
  type        = number
  default     = 1

  validation {
    condition     = var.node_pool_size >= 1 && var.node_pool_size <= 4
    error_message = "node_pool_size must be between 1 and 4 (Always-Free 4-OCPU ceiling, ≥1 OCPU per node)."
  }
}

variable "node_ocpus" {
  description = "OCPUs per worker node. node_ocpus × node_pool_size must stay ≤ 4 (asserted in tests/free_tier.tftest.hcl)."
  type        = number
  default     = 2
}

variable "node_memory_gb" {
  description = "Memory (GB) per worker node. node_memory_gb × node_pool_size must stay ≤ 24."
  type        = number
  default     = 12
}

variable "node_boot_volume_gb" {
  description = "Boot volume (GB) per worker node. node_boot_volume_gb × node_pool_size must stay ≤ 200 (Always-Free block-storage cap)."
  type        = number
  default     = 50
}

variable "ssh_public_key_path" {
  description = "Path to the SSH public key authorized for node access."
  type        = string
}
