output "cluster_id" {
  description = "OCID of the OKE cluster. Feed to `oci ce cluster create-kubeconfig --cluster-id …` in CD."
  value       = oci_containerengine_cluster.opengate.id
}

output "cluster_name" {
  description = "Display name of the OKE cluster."
  value       = oci_containerengine_cluster.opengate.name
}

output "node_pool_id" {
  description = "OCID of the worker node pool."
  value       = oci_containerengine_node_pool.opengate.id
}

output "kubernetes_version" {
  description = "Kubernetes version running on the control plane and node pool."
  value       = oci_containerengine_cluster.opengate.kubernetes_version
}
