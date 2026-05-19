locals {
  # The public subnet CIDR is referenced by both the security list (as the
  # source of the SSH-from-bastion ingress rule) and the subnet itself. Pulling
  # it into a local breaks what would otherwise be a resource cycle —
  # subnet → security_list_ids → security_list → subnet.cidr_block — and keeps
  # the value single-sourced for both consumers.
  public_subnet_cidr = "10.0.1.0/24"
}

resource "oci_core_vcn" "opengate" {
  compartment_id = var.compartment_id
  display_name   = "opengate-vcn"
  cidr_blocks    = ["10.0.0.0/16"]
  dns_label      = "opengate"
}

resource "oci_core_internet_gateway" "opengate" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.opengate.id
  display_name   = "opengate-igw"
  enabled        = true
}

resource "oci_core_route_table" "opengate" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.opengate.id
  display_name   = "opengate-rt"

  route_rules {
    destination       = "0.0.0.0/0"
    network_entity_id = oci_core_internet_gateway.opengate.id
  }
}

resource "oci_core_network_security_group" "cd_deploy" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.opengate.id
  display_name   = "opengate-cd-deploy"
}

resource "oci_core_security_list" "opengate" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.opengate.id
  display_name   = "opengate-sl"

  # Egress: allow all outbound
  egress_security_rules {
    destination = "0.0.0.0/0"
    protocol    = "all"
    stateless   = false
  }

  # SSH (operator break-glass) — restricted to `var.ssh_allowed_cidr`.
  # Production operator access is via OCI Bastion (see ADR-018); this rule
  # exists only as an emergency-break. Set ssh_allowed_cidr = "127.0.0.1/32"
  # in terraform.tfvars to close the path entirely when not needed. The
  # input-validation block rejects 0.0.0.0/0 either way.
  ingress_security_rules {
    source   = var.ssh_allowed_cidr
    protocol = "6" # TCP
    tcp_options {
      min = 22
      max = 22
    }
    stateless = false
  }

  # SSH (OCI Bastion) — source is the public subnet's own CIDR so the
  # bastion's allocated /28 service endpoint (carved from this subnet at
  # apply time) can reach TCP 22 on the target VM. Single-VM subnet today,
  # so the broader source is a no-op in practice. See ADR-018.
  ingress_security_rules {
    source   = local.public_subnet_cidr
    protocol = "6" # TCP
    tcp_options {
      min = 22
      max = 22
    }
    stateless = false
  }

  # HTTP (redirect to HTTPS)
  ingress_security_rules {
    source   = "0.0.0.0/0"
    protocol = "6" # TCP
    tcp_options {
      min = 80
      max = 80
    }
    stateless = false
  }

  # HTTPS
  ingress_security_rules {
    source   = "0.0.0.0/0"
    protocol = "6" # TCP
    tcp_options {
      min = 443
      max = 443
    }
    stateless = false
  }

  # HTTP/3 (Caddy)
  ingress_security_rules {
    source   = "0.0.0.0/0"
    protocol = "17" # UDP
    udp_options {
      min = 443
      max = 443
    }
    stateless = false
  }

  # MPS (Intel AMT CIRA)
  ingress_security_rules {
    source   = "0.0.0.0/0"
    protocol = "6" # TCP
    tcp_options {
      min = 4433
      max = 4433
    }
    stateless = false
  }

  # QUIC (agent connections)
  ingress_security_rules {
    source   = "0.0.0.0/0"
    protocol = "17" # UDP
    udp_options {
      min = 9090
      max = 9090
    }
    stateless = false
  }
}

resource "oci_core_subnet" "opengate_public" {
  compartment_id             = var.compartment_id
  vcn_id                     = oci_core_vcn.opengate.id
  display_name               = "opengate-public-subnet"
  cidr_block                 = local.public_subnet_cidr
  dns_label                  = "pub"
  route_table_id             = oci_core_route_table.opengate.id
  security_list_ids          = [oci_core_security_list.opengate.id]
  prohibit_public_ip_on_vnic = false
}
