# Networking submodule

Owns the OpenGate VCN and shared network surfaces: internet gateway, route table, the legacy `cd_deploy` network security group, the dormant public-subnet security list/subnet, and the active OKE API, worker-node, and load-balancer subnets/NSGs. The root module wires [bastion](../bastion/) to the OKE worker-node subnet for human node access; [compute](../compute/) is retained as a tested rollback module rather than an active production instance.

## Inputs

| Variable | Type | Purpose |
|---|---|---|
| `compartment_id` | string (sensitive) | OCI compartment OCID that owns the VCN and its children. |
| `ssh_allowed_cidr` | string (sensitive) | CIDR block allowed to reach TCP 22 on break-glass SSH rules (operator break-glass — normal access is via OCI Bastion per [ADR-018](../../../../docs/adr/ADR-018-oci-bastion-operator-access.md); set to `127.0.0.1/32` to disable). `0.0.0.0/0` is rejected by the root variable's validation block either way. |

## Outputs

| Output | Purpose |
|---|---|
| `vcn_id` | OCID of the VCN. |
| `subnet_id` | OCID of the dormant public subnet — consumed by the compute rollback module if the compose VM is re-instantiated. |
| `nsg_id` | OCID of the legacy `cd_deploy` NSG. Kept sensitive because OCIDs were historically consumed through GitHub Secrets and may be reused by rollback tooling. |
| `security_list_id` | OCID of the public-subnet security list (verified by `tests/integration.tftest.hcl`). |

## Test coverage

- [`tests/security.tftest.hcl`](tests/security.tftest.hcl) — security-list invariants: no SSH to world, public TCP ports = {80, 443, 4433}, public UDP ports = {443, 9090}, egress is stateful, and the dormant public-subnet Bastion recovery rule remains present. Runs against a mock provider — no OCI creds required.
