output "instance_public_ip" {
  description = "Public IP of the OpenGate server"
  value       = module.compute.public_ip
}

output "instance_id" {
  description = "OCID of the compute instance"
  value       = module.compute.instance_id
  sensitive   = true
}

output "vcn_id" {
  description = "OCID of the VCN"
  value       = module.networking.vcn_id
}

output "subnet_id" {
  description = "OCID of the public subnet"
  value       = module.networking.subnet_id
}

output "cd_nsg_id" {
  description = "OCID of the CD deploy NSG (for GitHub secrets)"
  value       = module.networking.nsg_id
  sensitive   = true
}
