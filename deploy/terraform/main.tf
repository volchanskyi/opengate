# Remote state lives in an OCI Object Storage bucket accessed via the S3-compatible
# API. Operator-specific values (the OCI namespace in the endpoint, the credentials
# file path) are provided via `backend.tfbackend` (gitignored). See
# docs/Infrastructure.md → "State backend" for the migration runbook.
#
# CI runs `terraform init -backend=false` (Makefile lint-deploy + ci.yml config-lint),
# so the backend stanza is never resolved in CI and no OCI creds are required there.

terraform {
  # 1.7+ is required for `expect_failures` against variable validation blocks
  # in the `terraform test` framework (security.tftest.hcl exercises this).
  required_version = ">= 1.7.0"

  required_providers {
    oci = {
      source  = "oracle/oci"
      version = "~> 6.0"
    }
  }

  backend "s3" {
    bucket = "opengate-tfstate"
    key    = "terraform.tfstate"
    region = "us-sanjose-1"

    # OCI Object Storage S3-compat is not real AWS — these checks must be skipped.
    skip_region_validation      = true
    skip_credentials_validation = true
    skip_metadata_api_check     = true
    skip_requesting_account_id  = true
    use_path_style              = true

    # AWS SDK v2 defaults to a flexible-checksum + chunked-encoding upload that
    # OCI S3-compat rejects with `501 NotImplemented: AWS chunked encoding not
    # supported`. Skip it.
    skip_s3_checksum = true

    # `endpoints.s3` and `shared_credentials_files` are supplied at init time via
    # `terraform init -backend-config=backend.tfbackend`. See backend.tfbackend.example.
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

module "networking" {
  source = "./modules/networking"

  compartment_id   = local.compartment_id
  ssh_allowed_cidr = var.ssh_allowed_cidr
}

module "compute" {
  source = "./modules/compute"

  compartment_id      = local.compartment_id
  tenancy_ocid        = var.tenancy_ocid
  subnet_id           = module.networking.subnet_id
  nsg_ids             = [module.networking.nsg_id]
  instance_shape      = var.instance_shape
  instance_ocpus      = var.instance_ocpus
  instance_memory_gb  = var.instance_memory_gb
  boot_volume_gb      = var.boot_volume_gb
  ssh_public_key_path = var.ssh_public_key_path
  cloud_init_path     = "${path.module}/cloud-init.yaml"
}

# OCI Bastion service — operator access plane. Replaces the static
# `var.ssh_allowed_cidr` ingress rule for human SSH + monitoring-UI tunnels.
# CI still uses the just-in-time NSG-rule pattern (see
# .github/actions/oci-ssh-setup). See ADR-018 for the decision rationale.
module "bastion" {
  source = "./modules/bastion"

  compartment_id   = local.compartment_id
  target_subnet_id = module.networking.subnet_id
}

# OKE cluster (Phase 13b cutover, ADR-030 / ADR-034). Stands up the BASIC
# control plane + a single Always-Free A1.Flex worker node *beside* the compose
# VM during the pilot (the 1×2 OCPU/12 GB defaults fit the remaining budget;
# plan §5). Networking (subnets + NSGs) comes from the networking module's OKE
# additions. node/version/image/AD are resolved live (see variables.tf defaults).
module "oke" {
  source = "./modules/oke"

  compartment_id         = local.compartment_id
  kubernetes_version     = var.oke_kubernetes_version
  vcn_id                 = module.networking.vcn_id
  api_endpoint_subnet_id = module.networking.oke_api_endpoint_subnet_id
  node_subnet_id         = module.networking.oke_node_subnet_id
  service_lb_subnet_ids  = [module.networking.oke_lb_subnet_id]
  api_nsg_ids            = [module.networking.oke_cp_nsg_id]
  node_nsg_ids           = [module.networking.oke_node_nsg_id]
  availability_domain    = var.oke_availability_domain
  node_image_id          = var.oke_node_image_id
  ssh_public_key_path    = var.ssh_public_key_path
}

# Reconcile pre-decomposition state addresses with the new module-prefixed
# addresses. Without these blocks Terraform would plan to destroy + recreate
# every resource on the next apply. Data sources do not need `moved` — they
# re-resolve at plan time.

moved {
  from = oci_core_vcn.opengate
  to   = module.networking.oci_core_vcn.opengate
}

moved {
  from = oci_core_internet_gateway.opengate
  to   = module.networking.oci_core_internet_gateway.opengate
}

moved {
  from = oci_core_route_table.opengate
  to   = module.networking.oci_core_route_table.opengate
}

moved {
  from = oci_core_network_security_group.cd_deploy
  to   = module.networking.oci_core_network_security_group.cd_deploy
}

moved {
  from = oci_core_security_list.opengate
  to   = module.networking.oci_core_security_list.opengate
}

moved {
  from = oci_core_subnet.opengate_public
  to   = module.networking.oci_core_subnet.opengate_public
}

moved {
  from = oci_core_instance.opengate
  to   = module.compute.oci_core_instance.opengate
}
