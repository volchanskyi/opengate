resource "oci_bastion_bastion" "opengate" {
  compartment_id = var.compartment_id
  bastion_type   = "STANDARD"
  name           = "opengate-bastion"

  target_subnet_id = var.target_subnet_id

  # IAM gates session creation; the CIDR list is only the L4 envelope filter.
  client_cidr_block_allow_list = ["0.0.0.0/0"]

  # 10800 seconds (3h) is the OCI service cap on session lifetime.
  max_session_ttl_in_seconds = 10800

  # policy/terraform/tags.rego requires these tags.
  freeform_tags = {
    env        = "prod"
    component  = "bastion"
    managed_by = "terraform"
  }
}
