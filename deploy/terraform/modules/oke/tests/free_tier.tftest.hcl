# Module-invariant tests for the OKE cluster + node pool. mock_provider lets a
# plan succeed without OCI creds; no data sources are used (all inputs are
# variables resolved by the root module), so an empty mock suffices.
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

# The control plane must stay BASIC — ENHANCED bills per cluster-hour and leaves
# the Always-Free envelope. Override only with explicit cost approval.
run "control_plane_is_basic" {
  command = plan

  assert {
    condition     = oci_containerengine_cluster.opengate.type == "BASIC_CLUSTER"
    error_message = "OKE cluster type must remain BASIC_CLUSTER (free control plane). ENHANCED bills per cluster-hour."
  }
}

# Pin worker nodes to the Always-Free ARM64 flex shape.
run "node_shape_is_free_tier" {
  command = plan

  assert {
    condition     = oci_containerengine_node_pool.opengate.node_shape == "VM.Standard.A1.Flex"
    error_message = "Node shape must remain VM.Standard.A1.Flex (Always Free ARM64). Override only with explicit cost approval."
  }
}

# Always-Free A1.Flex tenancy caps: ≤4 OCPUs and ≤24 GB across ALL nodes. A
# config tweak that pushes the pool over the cap is caught at plan time, not at
# apply when OCI returns LimitExceeded.
run "node_pool_within_compute_cap" {
  command = plan

  assert {
    condition     = oci_containerengine_node_pool.opengate.node_shape_config[0].ocpus * oci_containerengine_node_pool.opengate.node_config_details[0].size <= 4
    error_message = "Total node-pool OCPUs (ocpus × size) must stay ≤ 4 (Always-Free A1.Flex tenant cap)."
  }

  assert {
    condition     = oci_containerengine_node_pool.opengate.node_shape_config[0].memory_in_gbs * oci_containerengine_node_pool.opengate.node_config_details[0].size <= 24
    error_message = "Total node-pool memory (memory_in_gbs × size) must stay ≤ 24 GB (Always-Free A1.Flex tenant cap)."
  }
}

# Always-Free block-storage cap is 200 GB total across the tenancy.
run "node_pool_within_boot_volume_cap" {
  command = plan

  assert {
    condition     = oci_containerengine_node_pool.opengate.node_source_details[0].boot_volume_size_in_gbs * oci_containerengine_node_pool.opengate.node_config_details[0].size <= 200
    error_message = "Total node-pool boot storage (boot_volume_gb × size) must stay ≤ 200 GB (Always-Free cap)."
  }
}

# The Kubernetes dashboard add-on is a known attack surface; keep it off.
run "dashboard_addon_disabled" {
  command = plan

  assert {
    condition     = oci_containerengine_cluster.opengate.options[0].add_ons[0].is_kubernetes_dashboard_enabled == false
    error_message = "The Kubernetes dashboard add-on must stay disabled."
  }
}

# node_pool_size variable validation rejects a pool that would blow the OCPU cap.
run "rejects_oversized_pool" {
  command = plan

  variables {
    node_pool_size = 5
  }

  expect_failures = [
    var.node_pool_size,
  ]
}
