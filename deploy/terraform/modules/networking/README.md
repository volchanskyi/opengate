# Networking submodule

Owns the OpenGate VCN and everything inside it that is not compute: internet gateway, route table, the `cd_deploy` network security group, the public-subnet security list, and the public subnet itself. Sibling [compute](../compute/) attaches its instance to outputs from this module. Sibling [bastion](../bastion/) carves a /28 service endpoint from the public subnet — the security list grants TCP 22 from `local.public_subnet_cidr` to enable that path.

## Inputs

| Variable | Type | Purpose |
|---|---|---|
| `compartment_id` | string (sensitive) | OCI compartment OCID that owns the VCN and its children. |
| `ssh_allowed_cidr` | string (sensitive) | CIDR block allowed to reach TCP 22 on the public subnet (operator break-glass — production access is via OCI Bastion per [ADR-018](../../../docs/adr/ADR-018-oci-bastion-operator-access.md); set to `127.0.0.1/32` to disable). `0.0.0.0/0` is rejected by the root variable's validation block either way. |

## Outputs

| Output | Purpose |
|---|---|
| `vcn_id` | OCID of the VCN. |
| `subnet_id` | OCID of the public subnet — passed to compute for VNIC attachment and to the bastion module as `target_subnet_id`. |
| `subnet_cidr` | CIDR of the public subnet — passed to the bastion module so the /28 service-endpoint allocation and the SSH-from-bastion ingress rule share a single source of truth. |
| `nsg_id` | OCID of the `cd_deploy` NSG. Mutated at deploy time by `.github/workflows/cd.yml` for just-in-time SSH ingress. Surfaced as a sensitive output so CD can read it via GitHub Secrets without exposing it in logs. |
| `security_list_id` | OCID of the public-subnet security list (verified by `tests/integration.tftest.hcl`). |

## Test coverage

- [`tests/security.tftest.hcl`](tests/security.tftest.hcl) — security-list invariants: no SSH to world, public TCP ports = {80, 443, 4433}, public UDP ports = {443, 9090}, egress is stateful, bastion SSH ingress (TCP 22 from public subnet CIDR) is present. Runs against a mock provider — no OCI creds required.
