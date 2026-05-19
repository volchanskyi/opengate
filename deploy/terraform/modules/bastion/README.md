# Bastion submodule

Owns the OCI Bastion service that fronts human SSH + tunnel access to the OpenGate VPS. Replaces the static `var.ssh_allowed_cidr` ingress rule with IAM-gated, time-limited sessions — the dev-machine IP becomes irrelevant because OCI IAM decides who may create a session, not an L4 CIDR allow-list. See [ADR-018](../../../../docs/adr/ADR-018-oci-bastion-operator-access.md) for the decision rationale.

CI keeps the existing just-in-time NSG-rule pattern in [`.github/actions/oci-ssh-setup`](../../../../.github/actions/oci-ssh-setup/) — the bastion is for **human** access only.

## Inputs

| Variable | Type | Purpose |
|---|---|---|
| `compartment_id` | string (sensitive) | OCI compartment OCID that owns the bastion resource. |
| `target_subnet_id` | string | OCID of the subnet the bastion's /28 service endpoint is carved from. Must contain the target instance(s) for intra-subnet reachability. |

## Outputs

| Output | Purpose |
|---|---|
| `bastion_id` | OCID of the bastion (sensitive). Consumed by [`deploy/scripts/bastion-session.sh`](../../../scripts/bastion-session.sh) to create Managed SSH sessions. |
| `bastion_name` | Display name — surfaced for human-readable logs. |
| `max_session_ttl_in_seconds` | Session TTL cap (10800 = 3h). Surfaced so the cache wrapper can compute expiry without re-querying OCI. |

## Daily flow

```bash
make tunnel    # creates (or reuses) a Managed SSH session, forwards Grafana :3000 + Uptime Kuma :3001
make ssh       # same session, plain shell
```

Both targets resolve the bastion OCID via `terraform output -raw bastion_id`. First invocation takes 5–10 s (session create); subsequent invocations within the 3 h TTL skip the create step.

## Test coverage

- [`tests/bastion.tftest.hcl`](tests/bastion.tftest.hcl) — bastion type pinned to STANDARD, target subnet wiring, IAM-only gating (`client_cidr_block_allow_list = ["0.0.0.0/0"]`), session TTL pinned to OCI cap, target-subnet validation. Runs against a mock provider — no OCI creds required.
