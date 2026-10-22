# The workflows' own cloud login: an API-key user in a group of its own, outside every
# administrators group, allowed only the cluster use a kubeconfig needs.

locals {
  scope = var.compartment_id == var.tenancy_id ? "tenancy" : "compartment id ${var.compartment_id}"
}

resource "oci_identity_group" "workflow" {
  compartment_id = var.tenancy_id
  name           = var.name
  description    = "GitHub workflows that deploy and test OpenGate; cluster rights come from Kubernetes RBAC"
}

resource "oci_identity_user" "workflow" {
  compartment_id = var.tenancy_id
  name           = var.name
  description    = "GitHub workflows' cluster login; its API key is stored as repository secrets"
}

resource "oci_identity_user_group_membership" "workflow" {
  group_id = oci_identity_group.workflow.id
  user_id  = oci_identity_user.workflow.id
}

resource "oci_identity_policy" "workflow" {
  compartment_id = var.compartment_id
  name           = var.name
  description    = "Lets the workflow login fetch a kubeconfig for the OKE cluster"
  statements = [
    "Allow group ${oci_identity_group.workflow.name} to use clusters in ${local.scope}",
  ]
}
