output "vcn_id" {
  description = "OCID of the VCN"
  value       = module.networking.vcn_id
}


output "bastion_id" {
  description = "OCID of the OCI Bastion resource — consumed by deploy/scripts/bastion-session.sh to create Managed SSH sessions for operator access"
  value       = module.bastion.bastion_id
  sensitive   = true
}

output "oke_cluster_id" {
  description = "OCID of the OKE cluster — `oci ce cluster create-kubeconfig` + the OKE_CLUSTER_ID GitHub secret (oci-kube-setup, cd.yml)"
  value       = module.oke.cluster_id
  sensitive   = true
}

output "oke_node_pool_id" {
  description = "OCID of the OKE node pool"
  value       = module.oke.node_pool_id
  sensitive   = true
}

output "backup_bucket_name" {
  description = "Name of the off-cluster Postgres backup bucket"
  value       = module.backups.bucket_name
}

output "backup_policy_ocid" {
  description = "OCID of the opengate-os-lifecycle IAM policy that authorizes bucket lifecycle"
  value       = module.backups.policy_ocid
  sensitive   = true
}
