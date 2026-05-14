mock_provider "oci" {}

variables {
  compartment_id   = "ocid1.compartment.oc1..fake"
  ssh_allowed_cidr = "203.0.113.42/32" # RFC 5737 documentation range
}

# Load-bearing invariant: SSH must NEVER be open to the world. The previous
# revision had `var.ssh_allowed_cidr = "0.0.0.0/0"` accidentally committed
# once; the input-validation block below is the gate, this run is the audit.
run "ssh_must_not_be_open_to_world" {
  command = plan

  # Filter via `for ... if` so r.tcp_options[0] is only accessed for rules that
  # have a tcp_options block. Terraform 1.9's expression evaluator does not
  # short-circuit `&&`, so guarding the index inside the value expression alone
  # is not portable; the `if` clause is the load-bearing filter.
  assert {
    condition = alltrue([
      for r in oci_core_security_list.opengate.ingress_security_rules :
      !(r.tcp_options[0].min == 22 && r.source == "0.0.0.0/0")
      if length(r.tcp_options) > 0
    ])
    error_message = "SSH port 22 must not be open to 0.0.0.0/0 — pin to operator CIDR via var.ssh_allowed_cidr"
  }
}

# Exercises the validation block on var.ssh_allowed_cidr. Requires Terraform
# 1.7+ (`expect_failures` for variable validation). The failing run is the
# proof that the gate works — flipping the validation off would make this run
# pass silently (a false positive).
run "ssh_cidr_input_validation" {
  command = plan

  variables {
    ssh_allowed_cidr = "0.0.0.0/0"
  }

  expect_failures = [var.ssh_allowed_cidr]
}

# Public TCP ingress is exactly {80, 443, 4433}; SSH (22) is operator-only and
# never appears in the 0.0.0.0/0 set. Public UDP is exactly {443 HTTP/3, 9090 QUIC}.
run "expected_public_ports_only" {
  command = plan

  assert {
    condition = toset(
      [for r in oci_core_security_list.opengate.ingress_security_rules :
      r.tcp_options[0].min if r.protocol == "6" && r.source == "0.0.0.0/0" && length(r.tcp_options) > 0]
    ) == toset([80, 443, 4433])
    error_message = "Public TCP ingress must be exactly {80, 443, 4433}; SSH is operator-only"
  }

  assert {
    condition = toset(
      [for r in oci_core_security_list.opengate.ingress_security_rules :
      r.udp_options[0].min if r.protocol == "17" && r.source == "0.0.0.0/0" && length(r.udp_options) > 0]
    ) == toset([443, 9090])
    error_message = "Public UDP ingress must be exactly {443 HTTP/3, 9090 QUIC}"
  }
}

# Egress is unrestricted by design but MUST stay stateful so return traffic to
# stateful connections passes without an explicit reciprocal ingress rule.
run "egress_is_unrestricted_but_stateful" {
  command = plan

  assert {
    condition     = alltrue([for r in oci_core_security_list.opengate.egress_security_rules : r.stateless == false])
    error_message = "Egress rules must be stateful (stateless = false) for return-traffic compatibility"
  }
}
