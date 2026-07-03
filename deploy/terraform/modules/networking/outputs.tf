output "vcn_id" {
  description = "OCID of the VCN"
  value       = oci_core_vcn.opengate.id
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
