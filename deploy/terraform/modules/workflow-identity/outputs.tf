output "user_id" {
  description = "OCID of the workflow user: the OCI_WORKFLOW_USER_OCID secret and its cluster bindings"
  value       = oci_identity_user.workflow.id
}

output "group_id" {
  description = "OCID of the workflow group"
  value       = oci_identity_group.workflow.id
}
