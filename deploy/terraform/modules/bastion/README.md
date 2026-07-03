# Bastion submodule

Owns the OCI Bastion service that fronts human SSH access to the OKE worker node via IAM-gated, time-limited sessions — the dev-machine IP is irrelevant because OCI IAM decides who may create a session, not an L4 CIDR allow-list. See [ADR-018](../../../../docs/adr/ADR-018-oci-bastion-operator-access.md) for the decision rationale.

CI uses [`.github/actions/oci-kube-setup`](../../../../.github/actions/oci-kube-setup/action.yml) and talks to Kubernetes directly — the bastion is for **human** node-level access only.

## Inputs

| Variable | Type | Purpose |
|---|---|---|
| `compartment_id` | string (sensitive) | OCI compartment OCID that owns the bastion resource. |
| `target_subnet_id` | string | OCID of the subnet the bastion's /28 service endpoint is carved from. The root module wires this to the OKE worker-node subnet. |

## Outputs

| Output | Purpose |
|---|---|
| `bastion_id` | OCID of the bastion (sensitive). Consumed by [`deploy/scripts/bastion-session.sh`](../../../scripts/bastion-session.sh) to create Managed SSH sessions. |
| `bastion_name` | Display name — asserted by [`tests/integration.tftest.hcl`](../../tests/integration.tftest.hcl) so a rename is caught before the operator hits a stale `make ssh`. |

## Daily flow

```bash
make ssh       # creates (or reuses) a Managed SSH session to the OKE worker node
make tunnel    # kubectl port-forward to in-cluster Grafana; does not use bastion
```

`make ssh` resolves the bastion OCID via `terraform output -raw bastion_id` and resolves the current worker node from `oke_node_pool_id`. First invocation takes 60–90 s (session create); subsequent invocations within the 3 h TTL skip the create step. `make tunnel` is intentionally separate because Grafana is now a ClusterIP Kubernetes Service reached with `kubectl port-forward`.

## Test coverage

- [`tests/bastion.tftest.hcl`](tests/bastion.tftest.hcl) — bastion type pinned to STANDARD, target subnet wiring, IAM-only gating (`client_cidr_block_allow_list = ["0.0.0.0/0"]`), session TTL pinned to OCI cap, target-subnet validation. Runs against a mock provider — no OCI creds required.
