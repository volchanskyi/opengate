data "oci_identity_availability_domains" "ads" {
  compartment_id = var.tenancy_ocid
}

data "oci_core_images" "ubuntu" {
  compartment_id           = var.compartment_id
  operating_system         = "Canonical Ubuntu"
  operating_system_version = "24.04"
  shape                    = var.instance_shape
  sort_by                  = "TIMECREATED"
  sort_order               = "DESC"
}

resource "oci_core_instance" "opengate" {
  compartment_id      = var.compartment_id
  availability_domain = data.oci_identity_availability_domains.ads.availability_domains[0].name
  display_name        = "opengate-server"
  shape               = var.instance_shape

  # Required by policy/terraform/tags.rego (S3 of iac-security-testing-pyramid).
  # Tag values are static today; broaden to networking submodule in a follow-up.
  freeform_tags = {
    env        = "prod"
    component  = "server"
    managed_by = "terraform"
  }

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
    subnet_id        = var.subnet_id
    assign_public_ip = true
    display_name     = "opengate-vnic"
    nsg_ids          = var.nsg_ids
  }

  metadata = {
    ssh_authorized_keys = file(pathexpand(var.ssh_public_key_path))
    user_data           = base64encode(file(var.cloud_init_path))
  }

  # OCI Cloud Agent — enable the Bastion plugin so Managed SSH sessions
  # established by the operator-facing OCI Bastion (see [bastion submodule
  # README](../bastion/README.md) and [ADR-018](../../../docs/adr/ADR-018-oci-bastion-operator-access.md))
  # can reach this instance over the plugin's outbound tunnel. Plugins not
  # listed here retain their existing desired_state — only Bastion is
  # actively managed by terraform.
  agent_config {
    plugins_config {
      name          = "Bastion"
      desired_state = "ENABLED"
    }
  }

  lifecycle {
    ignore_changes = [metadata, source_details[0].source_id]
  }
}
