# OKE networking (Phase 13b cutover, ADR-030 / ADR-034). Added alongside the
# single-VM compose networking above so the cluster can stand up *beside* the VM
# during the pilot (plan §5) and reclaim its budget only after cutover.
#
# Topology: a public API-endpoint subnet, a public worker subnet (workers carry
# public IPs so the QUIC/MPS hostPorts are reachable and flannel egress works),
# and a public load-balancer subnet for the ingress-nginx OCI LB (the
# Always-Free 10 Mbps flexible LB — DNS points at its external IP). CNI is
# flannel overlay (pods_cidr 10.244.0.0/16 lives outside the VCN), so there is no
# in-VCN pod subnet. NSG rules follow Oracle's documented flannel matrix.

locals {
  oke_api_subnet_cidr  = "10.0.0.0/28"
  oke_node_subnet_cidr = "10.0.2.0/24"
  oke_lb_subnet_cidr   = "10.0.3.0/24"
  # NodePort range the ingress-nginx OCI LB forwards to on the workers.
  nodeport_min = 30000
  nodeport_max = 32767
}

# --- Network security groups ------------------------------------------------

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

# --- Control-plane (API endpoint) NSG rules ---------------------------------

# Workers → API server (6443) and managed-control-plane comms (12250).
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

# Workers → control plane path-MTU discovery (ICMP type 3 code 4).
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

# kubectl / CD reach the public API endpoint on 6443. The endpoint has its own
# TLS + RBAC; CD runs from dynamic GitHub-runner IPs, so the source is the
# internet (mirrors OKE's public-endpoint default; ADR-030).
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

# Control plane → all (workers on 10250/kubelet + OCI/OKE services). Egress-all
# matches the existing single-VM security posture (security list above).
resource "oci_core_network_security_group_security_rule" "cp_egress_all" {
  network_security_group_id = oci_core_network_security_group.oke_cp.id
  direction                 = "EGRESS"
  protocol                  = "all"
  destination               = "0.0.0.0/0"
  destination_type          = "CIDR_BLOCK"
  stateless                 = false
}

# --- Worker NSG rules -------------------------------------------------------

# Control plane → kubelet (10250).
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

# Control plane → workers path-MTU discovery.
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

# Worker ↔ worker (pod-to-pod over flannel, node-to-node) — all protocols within
# the node NSG.
resource "oci_core_network_security_group_security_rule" "node_ingress_self" {
  network_security_group_id = oci_core_network_security_group.oke_node.id
  direction                 = "INGRESS"
  protocol                  = "all"
  source                    = oci_core_network_security_group.oke_node.id
  source_type               = "NETWORK_SECURITY_GROUP"
  stateless                 = false
}

# Internet → workers path-MTU discovery.
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

# Internet → QUIC agent transport (9090/udp, hostPort).
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

# Internet → Intel AMT MPS/CIRA (4433/tcp, hostPort).
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

# LB subnet → ingress-nginx NodePorts (the OCI LB forwards 80/443 here).
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

# Operator break-glass SSH to nodes (restricted to ssh_allowed_cidr; ADR-018).
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

# Workers → all (image pulls, control plane 6443/12250, OCI services). Egress-all
# mirrors the existing single-VM posture.
resource "oci_core_network_security_group_security_rule" "node_egress_all" {
  network_security_group_id = oci_core_network_security_group.oke_node.id
  direction                 = "EGRESS"
  protocol                  = "all"
  destination               = "0.0.0.0/0"
  destination_type          = "CIDR_BLOCK"
  stateless                 = false
}

# --- Load-balancer subnet security list -------------------------------------
# The ingress-nginx OCI LB lives here; it accepts 80/443 from the internet and
# forwards to the worker NodePorts (allowed by node_ingress_nodeport above).
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
}

# --- Subnets ----------------------------------------------------------------
# All three reuse the public route table (IGW) — public endpoint + public
# workers + public LB for the single-node pilot.

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
