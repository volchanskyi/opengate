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

# OCI Bastion ingress — TCP 22 from the public subnet CIDR (`10.0.1.0/24`).
# The bastion's /28 service endpoint is carved from this subnet at apply time;
# allowing intra-subnet traffic to port 22 is what makes port-forwarding
# sessions reachable. Managed SSH sessions tunnel through the agent plugin
# and do not strictly require this rule, but it keeps port-forwarding open
# as a fallback when the plugin is offline. See ADR-018.
run "bastion_ssh_ingress_present" {
  command = plan

  assert {
    # Mirrors the `ssh_must_not_be_open_to_world` pattern upstream: the `if`
    # clause is restricted to the index-guard so Terraform 1.9's
    # non-short-circuiting `&&` cannot blow up `tcp_options[0]` for UDP
    # rules. The port equality moves into the value expression — safe because
    # the filter already dropped empty-tcp_options entries.
    condition = toset([
      for r in oci_core_security_list.opengate.ingress_security_rules :
      r.source
      if length(r.tcp_options) > 0
      ]) == toset([
      var.ssh_allowed_cidr, # operator break-glass (typically 127.0.0.1/32)
      "10.0.1.0/24",        # bastion service-endpoint /28 lives in this subnet
      "0.0.0.0/0",          # HTTP + HTTPS + MPS (80, 443, 4433) all use this source
    ])
    error_message = "TCP ingress source set drifted — expected {var.ssh_allowed_cidr, public subnet CIDR, 0.0.0.0/0}. The public subnet CIDR is what makes OCI Bastion's /28 service endpoint reachable; missing it breaks `make tunnel`."
  }

  # Specifically: there must be exactly one TCP 22 rule sourced from the
  # public subnet CIDR. Length-based assertion expressed via sum so the
  # condition reduces to a plain integer and avoids per-rule index access.
  assert {
    condition = length([
      for r in oci_core_security_list.opengate.ingress_security_rules :
      r
      if length(r.tcp_options) > 0
      && r.source == "10.0.1.0/24"
    ]) == 1
    error_message = "Exactly one ingress rule must source from the public subnet CIDR (10.0.1.0/24) — the bastion's /28 service endpoint depends on it."
  }
}
