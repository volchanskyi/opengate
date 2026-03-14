output "instance_public_ip" {
  description = "Public IP of the OpenGate server"
  value       = oci_core_instance.opengate.public_ip
}

output "instance_id" {
  description = "OCID of the compute instance"
  value       = oci_core_instance.opengate.id
  sensitive   = true
}

output "vcn_id" {
  description = "OCID of the VCN"
  value       = oci_core_vcn.opengate.id
}

output "subnet_id" {
  description = "OCID of the public subnet"
  value       = oci_core_subnet.opengate_public.id
}

output "cd_nsg_id" {
  description = "OCID of the CD deploy NSG (for GitHub secrets)"
  value       = oci_core_network_security_group.cd_deploy.id
  sensitive   = true
}
