# OKE BASIC cluster — the control plane is free on BASIC; workers are the
# Always-Free A1.Flex node pool. See ADR-030. This module is provisioned at
# cutover (docs/Kubernetes-Migration.md), not wired into the root stack that
# still manages the single-VM compose deployment.

resource "oci_containerengine_cluster" "opengate" {
  compartment_id     = var.compartment_id
  kubernetes_version = var.kubernetes_version
  name               = var.cluster_name
  vcn_id             = var.vcn_id
  # BASIC keeps the control plane free; ENHANCED bills per cluster-hour.
  type = "BASIC_CLUSTER"

  endpoint_config {
    subnet_id            = var.api_endpoint_subnet_id
    is_public_ip_enabled = var.endpoint_is_public
    nsg_ids              = var.api_nsg_ids
  }

  options {
    service_lb_subnet_ids = var.service_lb_subnet_ids

    add_ons {
      is_kubernetes_dashboard_enabled = false
      is_tiller_enabled               = false
    }

    kubernetes_network_config {
      pods_cidr     = var.pods_cidr
      services_cidr = var.services_cidr
    }
  }

  freeform_tags = {
    env        = var.environment
    component  = "oke"
    managed_by = "terraform"
  }
}

resource "oci_containerengine_node_pool" "opengate" {
  cluster_id         = oci_containerengine_cluster.opengate.id
  compartment_id     = var.compartment_id
  kubernetes_version = var.kubernetes_version
  name               = "${var.cluster_name}-np"
  node_shape         = var.node_shape

  node_shape_config {
    ocpus         = var.node_ocpus
    memory_in_gbs = var.node_memory_gb
  }

  node_config_details {
    size    = var.node_pool_size
    nsg_ids = var.node_nsg_ids

    placement_configs {
      availability_domain = var.availability_domain
      subnet_id           = var.node_subnet_id
    }
  }

  node_source_details {
    source_type             = "IMAGE"
    image_id                = var.node_image_id
    boot_volume_size_in_gbs = var.node_boot_volume_gb
  }

  initial_node_labels {
    key   = "opengate.io/pool"
    value = "default"
  }

  ssh_public_key = file(var.ssh_public_key_path)

  freeform_tags = {
    env        = var.environment
    component  = "oke-node"
    managed_by = "terraform"
  }
}
