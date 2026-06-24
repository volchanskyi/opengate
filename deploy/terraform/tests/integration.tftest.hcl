# Mock data sources used by the compute module — identical to the per-module
# free_tier test, since the integration test exercises the full root plan.
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

# The public subnet must reference exactly the security list owned by the
# networking module. Surfaced via the `public_subnet_security_list_ids`
# introspection output because module-internal resources are otherwise opaque
# from the root test scope.
run "subnet_uses_security_list_from_networking" {
  # `apply` instead of `plan` because the equality check below compares two
  # computed `id`s that aren't known until the (mocked) apply completes.
  command = apply

  assert {
    condition     = length(module.networking.public_subnet_security_list_ids) == 1
    error_message = "Subnet must reference exactly one security list (the networking module's). Adding extra security lists changes the effective ingress posture."
  }

  assert {
    condition     = one(module.networking.public_subnet_security_list_ids) == module.networking.security_list_id
    error_message = "Subnet's security_list_ids must equal [networking.security_list_id] — drift here means the security list under test in security.tftest.hcl is no longer the one actually applied to the subnet."
  }
}

# The bastion's /28 service endpoint must sit in the OKE worker-node subnet —
# the compose VM it formerly fronted was decommissioned once the OKE cutover
# stabilised (Phase 13b), so `make ssh` now reaches the node. The target subnet
# isn't directly assertable from the root scope, so we pin the display name the
# wrapper script (deploy/scripts/bastion-session.sh) greps for. The integration
# runs `apply` rather than `plan` because the subnet ID is computed.
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
