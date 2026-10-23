mock_provider "oci" {}

variables {
  tenancy_id     = "ocid1.tenancy.oc1..fake"
  compartment_id = "ocid1.tenancy.oc1..fake"
}

run "the_user_joins_the_workflow_group_alone" {
  command = apply

  assert {
    condition     = oci_identity_user_group_membership.workflow.group_id == oci_identity_group.workflow.id
    error_message = "The workflow user must belong to its own group and to no administrators group."
  }

  assert {
    condition     = oci_identity_user_group_membership.workflow.user_id == oci_identity_user.workflow.id
    error_message = "The group membership must name the workflow user."
  }

  assert {
    condition     = !can(regex("(?i)admin", oci_identity_group.workflow.name))
    error_message = "The workflow group must not be an administrators group."
  }
}

run "the_policy_names_only_cluster_use" {
  command = apply

  assert {
    condition = oci_identity_policy.workflow.statements == tolist([
      "Allow group ${oci_identity_group.workflow.name} to use clusters in tenancy",
    ])
    error_message = "The workflow policy grants only the cluster use a kubeconfig needs."
  }
}

run "a_compartment_policy_names_its_compartment" {
  command = apply

  variables {
    compartment_id = "ocid1.compartment.oc1..fake"
  }

  assert {
    condition = oci_identity_policy.workflow.statements == tolist([
      "Allow group ${oci_identity_group.workflow.name} to use clusters in compartment id ocid1.compartment.oc1..fake",
    ])
    error_message = "Outside the tenancy root the policy names the cluster's compartment."
  }

  assert {
    condition     = oci_identity_user.workflow.compartment_id == "ocid1.tenancy.oc1..fake"
    error_message = "Users and groups live in the tenancy root."
  }
}

run "the_user_carries_the_address_it_is_given" {
  command = apply

  variables {
    email = "workflows@example.com"
  }

  assert {
    condition     = oci_identity_user.workflow.email == "workflows@example.com"
    error_message = "An identity-domain account refuses a user without a primary email address."
  }
}
