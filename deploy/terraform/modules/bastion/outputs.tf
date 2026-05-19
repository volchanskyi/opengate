output "bastion_id" {
  description = "OCID of the OCI Bastion resource — consumed by deploy/scripts/bastion-session.sh to create Managed SSH sessions"
  value       = oci_bastion_bastion.opengate.id
  sensitive   = true
}

output "bastion_name" {
  description = "Display name of the bastion (for human-readable logs)"
  value       = oci_bastion_bastion.opengate.name
}

output "max_session_ttl_in_seconds" {
  description = "Session TTL cap — surfaced so the session-cache wrapper can compute expiry without re-querying OCI"
  value       = oci_bastion_bastion.opengate.max_session_ttl_in_seconds
}
