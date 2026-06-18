# OCI Bastion service — operator access plane for human SSH sessions.
# Replaces the static `var.ssh_allowed_cidr` ingress rule used previously; the
# dev-machine IP becomes irrelevant because IAM gates session creation instead
# of an L4 CIDR allow-list.
#
# See `docs/adr/ADR-018-oci-bastion-operator-access.md`.

mock_provider "oci" {}

variables {
  compartment_id   = "ocid1.compartment.oc1..fake"
  target_subnet_id = "ocid1.subnet.oc1..fake"
}

# The bastion type must be STANDARD. The other supported value (`EPHEMERAL`)
# is being deprecated by OCI and only exists for backwards compatibility with
# legacy sessions; STANDARD is the only billable-as-Always-Free option that
# supports both Managed SSH and Port-Forwarding sessions.
run "bastion_type_is_standard" {
  command = plan

  assert {
    condition     = oci_bastion_bastion.opengate.bastion_type == "STANDARD"
    error_message = "OCI Bastion must be of type STANDARD — EPHEMERAL is deprecated and lacks Managed SSH support."
  }
}

# The bastion must attach to the subnet containing its current target. The root
# module wires this to the OKE worker-node subnet, so OCI's allocated /28 service
# endpoint lands inside the VCN data plane.
run "target_subnet_is_wired" {
  command = plan

  assert {
    condition     = oci_bastion_bastion.opengate.target_subnet_id == var.target_subnet_id
    error_message = "Bastion target_subnet_id must equal the subnet OCID passed in by the root module."
  }
}

# Session creation is IAM-gated, not CIDR-gated — the allow-list is just the
# L4 envelope filter for which clients may even talk to the bastion endpoint.
# Locking it to 0.0.0.0/0 keeps the dev-machine IP irrelevant; tightening to
# specific CIDRs would re-introduce the dynamic-IP problem this module fixes.
run "client_cidr_is_open" {
  command = plan

  assert {
    condition     = oci_bastion_bastion.opengate.client_cidr_block_allow_list == tolist(["0.0.0.0/0"])
    error_message = "client_cidr_block_allow_list must be [\"0.0.0.0/0\"] — IAM gates session creation, not the CIDR allow-list."
  }
}

# OCI service cap is 10800 seconds (3 hours). Pinning it here documents the
# upper bound the Makefile wrapper schedules cache refreshes against.
run "session_ttl_at_oci_max" {
  command = plan

  assert {
    condition     = oci_bastion_bastion.opengate.max_session_ttl_in_seconds == 10800
    error_message = "max_session_ttl_in_seconds must be 10800 (3h, OCI service cap)."
  }
}

# Operator-supplied subnet OCID must be non-empty — otherwise the bastion
# would attach to a phantom subnet at apply time. Validated at the variable.
run "target_subnet_id_validation" {
  command = plan

  variables {
    target_subnet_id = ""
  }

  expect_failures = [var.target_subnet_id]
}
