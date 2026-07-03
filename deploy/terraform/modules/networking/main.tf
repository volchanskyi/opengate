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

