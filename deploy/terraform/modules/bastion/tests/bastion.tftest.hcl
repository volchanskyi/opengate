mock_provider "oci" {}

variables {
  compartment_id   = "ocid1.compartment.oc1..fake"
  target_subnet_id = "ocid1.subnet.oc1..fake"
}

run "bastion_type_is_standard" {
  command = plan

  assert {
    condition     = oci_bastion_bastion.opengate.bastion_type == "STANDARD"
    error_message = "OCI Bastion must be of type STANDARD — EPHEMERAL is deprecated and lacks Managed SSH support."
  }
}

run "target_subnet_is_wired" {
  command = plan

  assert {
    condition     = oci_bastion_bastion.opengate.target_subnet_id == var.target_subnet_id
    error_message = "Bastion target_subnet_id must equal the subnet OCID passed in by the root module."
  }
}

run "client_cidr_is_open" {
  command = plan

  assert {
    condition     = oci_bastion_bastion.opengate.client_cidr_block_allow_list == tolist(["0.0.0.0/0"])
    error_message = "client_cidr_block_allow_list must be [\"0.0.0.0/0\"] — IAM gates session creation, not the CIDR allow-list."
  }
}

run "session_ttl_at_oci_max" {
  command = plan

  assert {
    condition     = oci_bastion_bastion.opengate.max_session_ttl_in_seconds == 10800
    error_message = "max_session_ttl_in_seconds must be 10800 (3h, OCI service cap)."
  }
}

run "target_subnet_id_validation" {
  command = plan

  variables {
    target_subnet_id = ""
  }

  expect_failures = [var.target_subnet_id]
}
