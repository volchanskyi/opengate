output "bastion_id" {
  description = "OCID of the OCI Bastion resource — consumed by deploy/scripts/bastion-session.sh to create Managed SSH sessions"
  value       = oci_bastion_bastion.opengate.id
  sensitive   = true
}

output "bastion_name" {
  description = "Display name of the bastion — asserted by tests/integration.tftest.hcl so a rename is caught before the operator hits a stale make tunnel"
  value       = oci_bastion_bastion.opengate.name
}
