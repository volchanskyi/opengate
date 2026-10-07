# Remote state lives in OCI Object Storage through the S3-compatible API; the endpoint and
# credentials file come from the gitignored `backend.tfbackend`.

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

    # OCI's S3-compatible API lacks the AWS-only services these checks call.
    skip_region_validation      = true
    skip_credentials_validation = true
    skip_metadata_api_check     = true
    skip_requesting_account_id  = true
    use_path_style              = true

    # OCI rejects the SDK's flexible-checksum chunked upload with `501 NotImplemented`.
    skip_s3_checksum = true

    # `endpoints.s3` and `shared_credentials_files` are supplied at init through -backend-config.
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

# Operator access plane: the target is the OKE worker-node subnet, so `make ssh` reaches the node.
module "bastion" {
  source = "./modules/bastion"

  compartment_id   = local.compartment_id
  target_subnet_id = module.networking.oke_node_subnet_id
}

# OKE cluster: a BASIC control plane and one Always-Free A1.Flex worker node, with subnets and
# NSGs from the networking module; node, version, image and AD resolve live.
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

# Off-cluster Postgres backups: the bucket, retention lifecycle and lifecycle IAM policy,
# imported into state and never recreated; the namespace resolves live.
data "oci_objectstorage_namespace" "this" {
  compartment_id = local.compartment_id
}

module "backups" {
  source = "./modules/backups"

  compartment_ocid = local.compartment_id
  namespace        = data.oci_objectstorage_namespace.this.namespace
  bucket_name      = var.backup_bucket_name
  lifecycle_days   = var.backup_lifecycle_days
}

# These blocks map existing state addresses onto the module-prefixed ones, avoiding recreation.

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
