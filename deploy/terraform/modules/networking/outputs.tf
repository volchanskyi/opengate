output "vcn_id" {
  description = "OCID of the VCN"
  value       = oci_core_vcn.opengate.id
}

output "subnet_id" {
  description = "OCID of the public subnet"
  value       = oci_core_subnet.opengate_public.id
}

output "nsg_id" {
  description = "OCID of the legacy cd_deploy network security group (sensitive; retained for rollback tooling)"
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

# --- OKE networking (Phase 13b cutover) — consumed by the root `oke` module ---

output "oke_api_endpoint_subnet_id" {
  description = "OCID of the OKE API-endpoint subnet"
  value       = oci_core_subnet.oke_api.id
}

output "oke_node_subnet_id" {
  description = "OCID of the OKE worker-node subnet"
  value       = oci_core_subnet.oke_node.id
}

output "oke_lb_subnet_id" {
  description = "OCID of the OKE load-balancer subnet (ingress-nginx OCI LB)"
  value       = oci_core_subnet.oke_lb.id
}

output "oke_cp_nsg_id" {
  description = "OCID of the OKE control-plane (API endpoint) NSG"
  value       = oci_core_network_security_group.oke_cp.id
}

output "oke_node_nsg_id" {
  description = "OCID of the OKE worker-node NSG"
  value       = oci_core_network_security_group.oke_node.id
}
