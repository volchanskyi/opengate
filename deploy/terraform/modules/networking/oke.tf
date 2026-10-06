# Workers carry public IPs so the QUIC/MPS hostPorts are reachable and flannel egress works.
# The flannel overlay keeps pods_cidr outside the VCN, so no in-VCN pod subnet exists.

locals {
  oke_api_subnet_cidr  = "10.0.0.0/28"
  oke_node_subnet_cidr = "10.0.2.0/24"
  oke_lb_subnet_cidr   = "10.0.3.0/24"
  # NodePort range the ingress-nginx OCI LB forwards to on the workers.
  nodeport_min = 30000
  nodeport_max = 32767
}

resource "oci_core_network_security_group" "oke_cp" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.opengate.id
  display_name   = "opengate-oke-cp"
}

resource "oci_core_network_security_group" "oke_node" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.opengate.id
  display_name   = "opengate-oke-node"
}

resource "oci_core_network_security_group_security_rule" "cp_ingress_6443" {
  network_security_group_id = oci_core_network_security_group.oke_cp.id
  direction                 = "INGRESS"
  protocol                  = "6"
  source                    = oci_core_network_security_group.oke_node.id
  source_type               = "NETWORK_SECURITY_GROUP"
  stateless                 = false
  tcp_options {
    destination_port_range {
      min = 6443
      max = 6443
    }
  }
}

resource "oci_core_network_security_group_security_rule" "cp_ingress_12250" {
  network_security_group_id = oci_core_network_security_group.oke_cp.id
  direction                 = "INGRESS"
  protocol                  = "6"
  source                    = oci_core_network_security_group.oke_node.id
  source_type               = "NETWORK_SECURITY_GROUP"
  stateless                 = false
  tcp_options {
    destination_port_range {
      min = 12250
      max = 12250
    }
  }
}

# ICMP type 3 code 4 carries path-MTU discovery.
resource "oci_core_network_security_group_security_rule" "cp_ingress_icmp" {
  network_security_group_id = oci_core_network_security_group.oke_cp.id
  direction                 = "INGRESS"
  protocol                  = "1"
  source                    = oci_core_network_security_group.oke_node.id
  source_type               = "NETWORK_SECURITY_GROUP"
  stateless                 = false
  icmp_options {
    type = 3
    code = 4
  }
}

# CD runs from dynamic GitHub-runner IPs, so the source is the internet.
# The endpoint has its own TLS and RBAC.
resource "oci_core_network_security_group_security_rule" "cp_ingress_public_6443" {
  network_security_group_id = oci_core_network_security_group.oke_cp.id
  direction                 = "INGRESS"
  protocol                  = "6"
  source                    = "0.0.0.0/0"
  source_type               = "CIDR_BLOCK"
  stateless                 = false
  tcp_options {
    destination_port_range {
      min = 6443
      max = 6443
    }
  }
}

resource "oci_core_network_security_group_security_rule" "cp_egress_all" {
  network_security_group_id = oci_core_network_security_group.oke_cp.id
  direction                 = "EGRESS"
  protocol                  = "all"
  destination               = "0.0.0.0/0"
  destination_type          = "CIDR_BLOCK"
  stateless                 = false
}

resource "oci_core_network_security_group_security_rule" "node_ingress_kubelet" {
  network_security_group_id = oci_core_network_security_group.oke_node.id
  direction                 = "INGRESS"
  protocol                  = "6"
  source                    = oci_core_network_security_group.oke_cp.id
  source_type               = "NETWORK_SECURITY_GROUP"
  stateless                 = false
  tcp_options {
    destination_port_range {
      min = 10250
      max = 10250
    }
  }
}

# ICMP type 3 code 4 carries path-MTU discovery.
resource "oci_core_network_security_group_security_rule" "node_ingress_cp_icmp" {
  network_security_group_id = oci_core_network_security_group.oke_node.id
  direction                 = "INGRESS"
  protocol                  = "1"
  source                    = oci_core_network_security_group.oke_cp.id
  source_type               = "NETWORK_SECURITY_GROUP"
  stateless                 = false
  icmp_options {
    type = 3
    code = 4
  }
}

resource "oci_core_network_security_group_security_rule" "node_ingress_self" {
  network_security_group_id = oci_core_network_security_group.oke_node.id
  direction                 = "INGRESS"
  protocol                  = "all"
  source                    = oci_core_network_security_group.oke_node.id
  source_type               = "NETWORK_SECURITY_GROUP"
  stateless                 = false
}

# ICMP type 3 code 4 carries path-MTU discovery.
resource "oci_core_network_security_group_security_rule" "node_ingress_icmp" {
  network_security_group_id = oci_core_network_security_group.oke_node.id
  direction                 = "INGRESS"
  protocol                  = "1"
  source                    = "0.0.0.0/0"
  source_type               = "CIDR_BLOCK"
  stateless                 = false
  icmp_options {
    type = 3
    code = 4
  }
}

# The QUIC agent transport listens through a hostPort.
resource "oci_core_network_security_group_security_rule" "node_ingress_quic" {
  network_security_group_id = oci_core_network_security_group.oke_node.id
  direction                 = "INGRESS"
  protocol                  = "17"
  source                    = "0.0.0.0/0"
  source_type               = "CIDR_BLOCK"
  stateless                 = false
  udp_options {
    destination_port_range {
      min = 9090
      max = 9090
    }
  }
}

# Intel AMT MPS/CIRA listens through a hostPort.
resource "oci_core_network_security_group_security_rule" "node_ingress_mps" {
  network_security_group_id = oci_core_network_security_group.oke_node.id
  direction                 = "INGRESS"
  protocol                  = "6"
  source                    = "0.0.0.0/0"
  source_type               = "CIDR_BLOCK"
  stateless                 = false
  tcp_options {
    destination_port_range {
      min = 4433
      max = 4433
    }
  }
}

# The OCI load balancer forwards 80/443 to the ingress-nginx NodePorts.
resource "oci_core_network_security_group_security_rule" "node_ingress_nodeport" {
  network_security_group_id = oci_core_network_security_group.oke_node.id
  direction                 = "INGRESS"
  protocol                  = "6"
  source                    = local.oke_lb_subnet_cidr
  source_type               = "CIDR_BLOCK"
  stateless                 = false
  tcp_options {
    destination_port_range {
      min = local.nodeport_min
      max = local.nodeport_max
    }
  }
}

# Operator break-glass SSH to nodes, restricted to ssh_allowed_cidr.
resource "oci_core_network_security_group_security_rule" "node_ingress_ssh" {
  network_security_group_id = oci_core_network_security_group.oke_node.id
  direction                 = "INGRESS"
  protocol                  = "6"
  source                    = var.ssh_allowed_cidr
  source_type               = "CIDR_BLOCK"
  stateless                 = false
  tcp_options {
    destination_port_range {
      min = 22
      max = 22
    }
  }
}

resource "oci_core_network_security_group_security_rule" "node_egress_all" {
  network_security_group_id = oci_core_network_security_group.oke_node.id
  direction                 = "EGRESS"
  protocol                  = "all"
  destination               = "0.0.0.0/0"
  destination_type          = "CIDR_BLOCK"
  stateless                 = false
}

resource "oci_core_security_list" "oke_lb" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.opengate.id
  display_name   = "opengate-oke-lb-sl"

  egress_security_rules {
    destination = "0.0.0.0/0"
    protocol    = "all"
    stateless   = false
  }

  ingress_security_rules {
    source   = "0.0.0.0/0"
    protocol = "6"
    tcp_options {
      min = 80
      max = 80
    }
    stateless = false
  }

  ingress_security_rules {
    source   = "0.0.0.0/0"
    protocol = "6"
    tcp_options {
      min = 443
      max = 443
    }
    stateless = false
  }

  # The OCI Cloud Controller Manager rewrites this list's rules at runtime; ignoring them
  # stops Terraform fighting it and keeps the nightly drift job stable.
  lifecycle {
    ignore_changes = [egress_security_rules, ingress_security_rules]
  }
}

resource "oci_core_subnet" "oke_api" {
  compartment_id             = var.compartment_id
  vcn_id                     = oci_core_vcn.opengate.id
  display_name               = "opengate-oke-api-subnet"
  cidr_block                 = local.oke_api_subnet_cidr
  dns_label                  = "okeapi"
  route_table_id             = oci_core_route_table.opengate.id
  prohibit_public_ip_on_vnic = false
}

resource "oci_core_subnet" "oke_node" {
  compartment_id             = var.compartment_id
  vcn_id                     = oci_core_vcn.opengate.id
  display_name               = "opengate-oke-node-subnet"
  cidr_block                 = local.oke_node_subnet_cidr
  dns_label                  = "okenode"
  route_table_id             = oci_core_route_table.opengate.id
  prohibit_public_ip_on_vnic = false
}

resource "oci_core_subnet" "oke_lb" {
  compartment_id             = var.compartment_id
  vcn_id                     = oci_core_vcn.opengate.id
  display_name               = "opengate-oke-lb-subnet"
  cidr_block                 = local.oke_lb_subnet_cidr
  dns_label                  = "okelb"
  route_table_id             = oci_core_route_table.opengate.id
  security_list_ids          = [oci_core_security_list.oke_lb.id]
  prohibit_public_ip_on_vnic = false
}
