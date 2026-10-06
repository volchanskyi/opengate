mock_provider "oci" {}

variables {
  compartment_id   = "ocid1.compartment.oc1..fake"
  ssh_allowed_cidr = "203.0.113.42/32" # RFC 5737 documentation range
}

run "ssh_cidr_input_validation" {
  command = plan

  variables {
    ssh_allowed_cidr = "0.0.0.0/0"
  }

  expect_failures = [var.ssh_allowed_cidr]
}
