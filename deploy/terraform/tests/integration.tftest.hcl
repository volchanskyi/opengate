# Mock data sources for the full root plan (OKE images/availability domains,
# object-storage namespace for backups).
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
  mock_data "oci_objectstorage_namespace" {
    defaults = {
      namespace = "fakenamespace"
    }
  }
}

variables {
  tenancy_ocid        = "ocid1.tenancy.oc1..fake"
  user_ocid           = "ocid1.user.oc1..fake"
  fingerprint         = "aa:bb:cc:dd:ee:ff:00:11:22:33:44:55:66:77:88:99"
  private_key_path    = "/dev/null"
  ssh_allowed_cidr    = "203.0.113.42/32"
  ssh_public_key_path = "/dev/null"
}

# The bastion's /28 service endpoint sits in the OKE worker-node subnet, so
# `make ssh` reaches the node. The target subnet isn't directly assertable from
# the root scope, so we pin the display name the wrapper script
# (deploy/scripts/bastion-session.sh) greps for. The integration runs `apply`
# rather than `plan` because the subnet ID is computed.
run "bastion_targets_node_subnet" {
  command = apply

  assert {
    condition     = module.bastion.bastion_name == "opengate-bastion"
    error_message = "Bastion display name drifted — the wrapper script (deploy/scripts/bastion-session.sh) greps for this exact name when prompting the operator for an OCI IAM-grant request."
  }
}

# The backup bucket is wired into the root plan with the namespace resolved from
# the (mocked) oci_objectstorage_namespace data source. Surfaced via the root
# output so the module-internal resource is assertable from this scope.
run "backups_bucket_is_wired" {
  command = apply

  assert {
    condition     = output.backup_bucket_name == "opengate-pg-backups"
    error_message = "Root must expose the backup bucket name — drift means the backups module is no longer wired into the root plan."
  }
}
