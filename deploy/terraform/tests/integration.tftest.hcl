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

# The compute instance VNIC must attach exactly the cd_deploy NSG. cd.yml
# mutates that NSG's ingress rules at deploy time for just-in-time SSH (see
# .github/actions/oci-ssh-setup); if the wiring breaks, those rules land on
# the wrong NSG and CD fails silently.
run "instance_attached_to_cd_nsg" {
  command = plan

  assert {
    condition     = length(module.compute.instance_nsg_ids) == 1
    error_message = "Instance VNIC must attach exactly one NSG (the cd_deploy NSG from networking). Multiple NSGs would split cd.yml's runtime mutations across NSGs."
  }
}
