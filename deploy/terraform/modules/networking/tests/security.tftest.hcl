mock_provider "oci" {}

variables {
  compartment_id   = "ocid1.compartment.oc1..fake"
  ssh_allowed_cidr = "203.0.113.42/32" # RFC 5737 documentation range
}

# Load-bearing invariant: the operator break-glass SSH CIDR (var.ssh_allowed_cidr,
# applied to the OKE worker-node NSG rule in oke.tf) must never be 0.0.0.0/0. The
# variable-validation block is the gate; this run is the audit. Requires Terraform
# 1.7+ (`expect_failures` for variable validation) — the failing run is the proof
# the gate works, so disabling the validation would make this run fail.
run "ssh_cidr_input_validation" {
  command = plan

  variables {
    ssh_allowed_cidr = "0.0.0.0/0"
  }

  expect_failures = [var.ssh_allowed_cidr]
}
