resource "oci_bastion_bastion" "opengate" {
  compartment_id = var.compartment_id
  bastion_type   = "STANDARD"
  name           = "opengate-bastion"

  target_subnet_id = var.target_subnet_id

  # IAM gates session creation; CIDR is just the L4 envelope filter. Locking
  # to 0.0.0.0/0 keeps the dev-machine IP irrelevant — the whole point of
  # adopting the bastion (see ADR-018).
  client_cidr_block_allow_list = ["0.0.0.0/0"]

  # OCI service cap is 10800 seconds (3h). Pinned at the cap so a single
  # interactive debug session covers a typical incident window without
  # mid-flight reconnects; the Makefile wrapper still re-creates expired
  # sessions transparently.
  max_session_ttl_in_seconds = 10800

  # Required by policy/terraform/tags.rego for the compute instance — applied
  # here too for consistency. Bastion itself is Always Free in perpetuity, but
  # tagging the resource makes future cost-attribution searches uniform.
  freeform_tags = {
    env        = "prod"
    component  = "bastion"
    managed_by = "terraform"
  }
}
