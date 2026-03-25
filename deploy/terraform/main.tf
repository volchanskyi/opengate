# Remote backend (recommended for production):
#
#   terraform {
#     backend "s3" {
#       bucket                      = "opengate-tfstate"
#       key                         = "terraform.tfstate"
#       region                      = "us-ashburn-1"
#       endpoint                    = "https://<namespace>.compat.objectstorage.<region>.oraclecloud.com"
#       shared_credentials_file     = "~/.oci/terraform-credentials"
#       skip_region_validation      = true
#       skip_credentials_validation = true
#       skip_metadata_api_check     = true
#       force_path_style            = true
#     }
#   }
#
# See: https://docs.oracle.com/en-us/iaas/Content/API/SDKDocs/terraformUsingObjectStore.htm

terraform {
  required_version = ">= 1.5"

  required_providers {
    oci = {
      source  = "oracle/oci"
      version = "~> 6.0"
    }
  }
}

provider "oci" {
  tenancy_ocid     = var.tenancy_ocid
  user_ocid        = var.user_ocid
  fingerprint      = var.fingerprint
  private_key_path = var.private_key_path
  region           = var.region
}

locals {
  compartment_id = var.compartment_ocid != "" ? var.compartment_ocid : var.tenancy_ocid
}

# ---------- Networking ----------

resource "oci_core_vcn" "opengate" {
  compartment_id = local.compartment_id
  display_name   = "opengate-vcn"
  cidr_blocks    = ["10.0.0.0/16"]
  dns_label      = "opengate"
}

resource "oci_core_internet_gateway" "opengate" {
  compartment_id = local.compartment_id
  vcn_id         = oci_core_vcn.opengate.id
  display_name   = "opengate-igw"
  enabled        = true
}

resource "oci_core_route_table" "opengate" {
  compartment_id = local.compartment_id
  vcn_id         = oci_core_vcn.opengate.id
  display_name   = "opengate-rt"

  route_rules {
    destination       = "0.0.0.0/0"
    network_entity_id = oci_core_internet_gateway.opengate.id
  }
}

resource "oci_core_network_security_group" "cd_deploy" {
  compartment_id = local.compartment_id
  vcn_id         = oci_core_vcn.opengate.id
  display_name   = "opengate-cd-deploy"
}

resource "oci_core_security_list" "opengate" {
  compartment_id = local.compartment_id
  vcn_id         = oci_core_vcn.opengate.id
  display_name   = "opengate-sl"

  # Egress: allow all outbound
  egress_security_rules {
    destination = "0.0.0.0/0"
    protocol    = "all"
    stateless   = false
  }

  # SSH — restricted to operator IP
  ingress_security_rules {
    source   = var.ssh_allowed_cidr
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
  compartment_id             = local.compartment_id
  vcn_id                     = oci_core_vcn.opengate.id
  display_name               = "opengate-public-subnet"
  cidr_block                 = "10.0.1.0/24"
  dns_label                  = "pub"
  route_table_id             = oci_core_route_table.opengate.id
  security_list_ids          = [oci_core_security_list.opengate.id]
  prohibit_public_ip_on_vnic = false
}

# ---------- Compute ----------

data "oci_identity_availability_domains" "ads" {
  compartment_id = var.tenancy_ocid
}

data "oci_core_images" "ubuntu" {
  compartment_id           = local.compartment_id
  operating_system         = "Canonical Ubuntu"
  operating_system_version = "24.04"
  shape                    = var.instance_shape
  sort_by                  = "TIMECREATED"
  sort_order               = "DESC"
}

resource "oci_core_instance" "opengate" {
  compartment_id      = local.compartment_id
  availability_domain = data.oci_identity_availability_domains.ads.availability_domains[0].name
  display_name        = "opengate-server"
  shape               = var.instance_shape

  shape_config {
    ocpus         = var.instance_ocpus
    memory_in_gbs = var.instance_memory_gb
  }

  source_details {
    source_type             = "image"
    source_id               = data.oci_core_images.ubuntu.images[0].id
    boot_volume_size_in_gbs = var.boot_volume_gb
  }

  create_vnic_details {
    subnet_id        = oci_core_subnet.opengate_public.id
    assign_public_ip = true
    display_name     = "opengate-vnic"
    nsg_ids          = [oci_core_network_security_group.cd_deploy.id]
  }

  metadata = {
    ssh_authorized_keys = file(pathexpand(var.ssh_public_key_path))
    user_data           = base64encode(file("${path.module}/cloud-init.yaml"))
  }

  lifecycle {
    ignore_changes = [metadata, source_details[0].source_id]
  }
}
