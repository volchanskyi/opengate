output "vcn_id" {
  description = "OCID of the VCN"
  value       = oci_core_vcn.opengate.id
}

output "subnet_id" {
  description = "OCID of the public subnet"
  value       = oci_core_subnet.opengate_public.id
}

output "subnet_cidr" {
  description = "CIDR block of the public subnet — surfaced so the bastion module (which carves a /28 service endpoint from it) and any future per-subnet ingress rules can reference a single source of truth"
  value       = oci_core_subnet.opengate_public.cidr_block
}

output "nsg_id" {
  description = "OCID of the cd_deploy network security group (mutated at deploy time by cd.yml for just-in-time SSH ingress)"
  value       = oci_core_network_security_group.cd_deploy.id
  sensitive   = true
}

output "security_list_id" {
  description = "OCID of the public-subnet security list (security_list_ids of the public subnet)"
  value       = oci_core_security_list.opengate.id
}

# Introspection outputs — surfaced solely so `tests/integration.tftest.hcl` can
# assert cross-module wiring from the root scope. Module-internal resources are
# otherwise opaque from outside the module.

output "public_subnet_security_list_ids" {
  description = "security_list_ids attribute on the public subnet — verified by the integration test to equal exactly [security_list_id]"
  value       = oci_core_subnet.opengate_public.security_list_ids
}
