# Networking submodule

Owns the OpenGate VCN and its shared surfaces — internet gateway and route table — plus the OKE API, worker-node, and load-balancer subnets and NSGs. The root module wires [bastion](../bastion/) to the OKE worker-node subnet for human node access.

## Inputs

| Variable | Type | Purpose |
|---|---|---|
| `compartment_id` | string (sensitive) | OCI compartment OCID that owns the VCN and its children. |
| `ssh_allowed_cidr` | string (sensitive) | CIDR block allowed to reach TCP 22 on break-glass SSH rules (operator break-glass — normal access is via OCI Bastion per [ADR-018](../../../../docs/adr/ADR-018-oci-bastion-operator-access.md); set to `127.0.0.1/32` to disable). `0.0.0.0/0` is rejected by the root variable's validation block either way. |

## Outputs

| Output | Purpose |
|---|---|
| `vcn_id` | OCID of the VCN. |
| `oke_api_endpoint_subnet_id` | OCID of the OKE API-endpoint subnet. |
| `oke_node_subnet_id` | OCID of the OKE worker-node subnet. |
| `oke_lb_subnet_id` | OCID of the OKE load-balancer subnet. |
| `oke_cp_nsg_id` | OCID of the OKE control-plane NSG. |
| `oke_node_nsg_id` | OCID of the OKE worker-node NSG. |

## Test coverage

- [`tests/security.tftest.hcl`](tests/security.tftest.hcl) — asserts the operator break-glass SSH CIDR (`var.ssh_allowed_cidr`, applied to the OKE worker-node NSG) can never be `0.0.0.0/0`. Runs against a mock provider — no OCI creds required.
