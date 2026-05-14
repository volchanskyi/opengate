# Mock data sources for the AD and Ubuntu image lookups so a plan succeeds
# without real OCI creds. The fake values are only consumed during plan-time
# substitution — they never reach OCI.
mock_provider "oci" {
  mock_data "oci_identity_availability_domains" {
    defaults = {
      availability_domains = [
        { name = "AD-1" }
      ]
    }
  }
  mock_data "oci_core_images" {
    defaults = {
      images = [
        { id = "ocid1.image.oc1..fake" }
      ]
    }
  }
}

variables {
  compartment_id      = "ocid1.compartment.oc1..fake"
  tenancy_ocid        = "ocid1.tenancy.oc1..fake"
  subnet_id           = "ocid1.subnet.oc1..fake"
  nsg_ids             = ["ocid1.nsg.oc1..fake"]
  instance_shape      = "VM.Standard.A1.Flex"
  instance_ocpus      = 2
  instance_memory_gb  = 12
  boot_volume_gb      = 50
  ssh_public_key_path = "/dev/null"
  cloud_init_path     = "/dev/null"
}

# Pin the shape to Always Free ARM64. Override only with explicit cost approval,
# in which case this test will fail and the cost-approval PR will need a
# matching change here.
run "free_tier_shape" {
  command = plan

  assert {
    condition     = oci_core_instance.opengate.shape == "VM.Standard.A1.Flex"
    error_message = "Instance shape must remain VM.Standard.A1.Flex (Always Free ARM64). Override only with explicit cost approval."
  }
}

# Always Free A1.Flex caps: ≤4 OCPUs total, ≤24 GB memory total per tenancy.
# Stamped here so a config tweak that pushes us over the cap is caught at plan
# time, not at apply time when OCI returns LimitExceeded.
run "free_tier_compute_limits" {
  command = plan

  assert {
    condition     = oci_core_instance.opengate.shape_config[0].ocpus <= 4
    error_message = "Always Free A1.Flex tenant cap is 4 OCPUs total"
  }

  assert {
    condition     = oci_core_instance.opengate.shape_config[0].memory_in_gbs <= 24
    error_message = "Always Free A1.Flex tenant cap is 24 GB memory total"
  }
}

run "free_tier_boot_volume_limit" {
  command = plan

  assert {
    condition     = oci_core_instance.opengate.source_details[0].boot_volume_size_in_gbs <= 200
    error_message = "Always Free boot volume cap is 200 GB total per tenancy"
  }
}
