output "instance_id" {
  description = "OCID of the compute instance"
  value       = oci_core_instance.opengate.id
  sensitive   = true
}

output "public_ip" {
  description = "Public IP of the compute instance — surfaces in the root module as instance_public_ip"
  value       = oci_core_instance.opengate.public_ip
}

output "private_ip" {
  description = "Private IP of the compute instance — useful if the dormant rollback VM is re-instantiated"
  value       = oci_core_instance.opengate.private_ip
}

# Introspection outputs — surfaced solely so `tests/integration.tftest.hcl` can
# assert that the instance attached the networking module's cd_deploy NSG.

output "instance_nsg_ids" {
  description = "create_vnic_details[0].nsg_ids on the compute instance — verified by the integration test to equal [networking.nsg_id]"
  value       = oci_core_instance.opengate.create_vnic_details[0].nsg_ids
}
