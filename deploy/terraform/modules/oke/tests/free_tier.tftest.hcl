mock_provider "oci" {}

variables {
  compartment_id         = "ocid1.compartment.oc1..fake"
  kubernetes_version     = "v1.31.1"
  vcn_id                 = "ocid1.vcn.oc1..fake"
  api_endpoint_subnet_id = "ocid1.subnet.oc1..fakeapi"
  node_subnet_id         = "ocid1.subnet.oc1..fakenode"
  availability_domain    = "AD-1"
  node_image_id          = "ocid1.image.oc1..fake"
  node_shape             = "VM.Standard.A1.Flex"
  node_pool_size         = 1
  node_ocpus             = 2
  node_memory_gb         = 12
  node_boot_volume_gb    = 50
  ssh_public_key_path    = "/dev/null"
}

run "control_plane_is_basic" {
  command = plan

  assert {
    condition     = oci_containerengine_cluster.opengate.type == "BASIC_CLUSTER"
    error_message = "OKE cluster type must remain BASIC_CLUSTER (free control plane). ENHANCED bills per cluster-hour."
  }
}

run "node_shape_is_free_tier" {
  command = plan

  assert {
    condition     = oci_containerengine_node_pool.opengate.node_shape == "VM.Standard.A1.Flex"
    error_message = "Node shape must remain VM.Standard.A1.Flex (Always Free ARM64). Override only with explicit cost approval."
  }
}

run "node_pool_within_compute_cap" {
  command = plan

  assert {
    condition     = oci_containerengine_node_pool.opengate.node_shape_config[0].ocpus * oci_containerengine_node_pool.opengate.node_config_details[0].size <= 2
    error_message = "Total node-pool OCPUs (ocpus × size) must stay ≤ 2 (Always-Free A1.Flex tenant cap)."
  }

  assert {
    condition     = oci_containerengine_node_pool.opengate.node_shape_config[0].memory_in_gbs * oci_containerengine_node_pool.opengate.node_config_details[0].size <= 12
    error_message = "Total node-pool memory (memory_in_gbs × size) must stay ≤ 12 GB (Always-Free A1.Flex tenant cap)."
  }
}

run "node_pool_within_boot_volume_cap" {
  command = plan

  assert {
    condition     = oci_containerengine_node_pool.opengate.node_source_details[0].boot_volume_size_in_gbs * oci_containerengine_node_pool.opengate.node_config_details[0].size <= 200
    error_message = "Total node-pool boot storage (boot_volume_gb × size) must stay ≤ 200 GB (Always-Free cap)."
  }
}

run "dashboard_addon_disabled" {
  command = plan

  assert {
    condition     = oci_containerengine_cluster.opengate.options[0].add_ons[0].is_kubernetes_dashboard_enabled == false
    error_message = "The Kubernetes dashboard add-on must stay disabled."
  }
}

run "rejects_oversized_pool" {
  command = plan

  variables {
    node_pool_size = 5
  }

  expect_failures = [
    var.node_pool_size,
  ]
}
