# ADR-018: OCI Bastion service for operator node SSH

Date: 2026-05-18
Status: Accepted

## Context

OpenGate now runs production and staging on OKE. The original reason for this
ADR still holds: the operator's dev machine sits on a dynamic ISP-issued IP, and
pinning day-to-day SSH to `var.ssh_allowed_cidr` made access brittle after every
IP rebind.

Current access surfaces are split by purpose:

- Kubernetes deployment and routine diagnostics use the OKE API through
  [`.github/actions/oci-kube-setup/action.yml`](../../.github/actions/oci-kube-setup/action.yml)
  and local `kubectl`.
- Grafana is an in-cluster ClusterIP Service reached with `kubectl port-forward`
  (`make tunnel`), not an SSH tunnel.
- Node-level emergency/debug SSH uses OCI Bastion Managed SSH sessions through
  [`deploy/scripts/bastion-session.sh`](../../deploy/scripts/bastion-session.sh).
- `ssh_allowed_cidr` remains only as a break-glass input; the normal human path
  is IAM-gated Bastion access.

The compose VM that this ADR originally fronted was decommissioned after the OKE
cutover. The Bastion target is now the OKE worker-node subnet, and the wrapper
resolves the current worker node from the Terraform `oke_node_pool_id` output.

Constraints collected up-front remain valid:

- **Users**: solo today, small team (2–5) within 6–12 months → must support per-user identity.
- **Frequency**: daily / near-daily → session setup must be predictable and scriptable.
- **Budget**: $0 — Always Free OCI only, no paid SaaS for operator access.
- **Access mode**: WSL desktop + CI-side scripts; no mobile requirement.

## Options Considered

| Option | Daily-use fit | $0 fit | Multi-user fit | Verdict |
|---|---|---|---|---|
| **OCI Bastion** (managed) | Scriptable session create, cached for 3 h | Always Free | Native OCI IAM (per-user audit in OCI) | **Adopted** |
| Self-service JIT NSG script | Fast, but lingers if cleanup fails | Free | Shared OCI key | Fallback only — weak audit |
| DDNS + auto-refresh of `ssh_allowed_cidr` | Per-IP, not per-user | Free | No identity model | Doesn't scale to team |
| Self-hosted WireGuard overlay (Headscale) | Fast | Free | Per-peer key | More moving parts, ops burden |
| Tailscale free tier | Fast | $0 for ≤ 3 users, then paid | Built-in IAM | Violates "$0 with team growth" |
| Cloudflare Tunnel + Access | Fast | Free up to 50 users | Built-in IAM | Adds SaaS dependency; rebinds the security model |

OCI Bastion is still the only candidate that satisfies the access constraints
without adding a third-party dependency or a paid component.

## Decision

Provision an **OCI Bastion** in the production VCN and use **Managed SSH
sessions** for human node-level access. CI and normal deploys do not use the
bastion; they use `oci-kube-setup` and the Kubernetes API. The static
`ssh_allowed_cidr` rule stays as emergency break-glass and is normally safe to
pin to `127.0.0.1/32`.

### Terraform Layout

- [`deploy/terraform/modules/bastion/`](../../deploy/terraform/modules/bastion/)
  provisions `oci_bastion_bastion.opengate` as `STANDARD`, with
  `client_cidr_block_allow_list = ["0.0.0.0/0"]` because OCI IAM gates session
  creation, and `max_session_ttl_in_seconds = 10800` (3 h, OCI service cap).
- The root module wires Bastion `target_subnet_id` to
  `module.networking.oke_node_subnet_id`, matching the post-cutover OKE worker
  node target.
- [`deploy/scripts/bastion-session.sh`](../../deploy/scripts/bastion-session.sh)
  resolves the active worker-node OCID and private IP from the Terraform
  `oke_node_pool_id` output via `oci ce node-pool get`.
- [`deploy/terraform/modules/networking/oke.tf`](../../deploy/terraform/modules/networking/oke.tf)
  keeps node SSH break-glass restricted by `var.ssh_allowed_cidr`; public app
  ingress, QUIC, MPS, and NodePorts are modeled separately from operator access.

### Operator UX

[`deploy/scripts/bastion-session.sh`](../../deploy/scripts/bastion-session.sh)
is pure bash + OCI CLI. It caches the active session OCID, canonical SSH command,
and expiry under `~/.cache/opengate/bastion-session.json` so repeated `make ssh`
invocations within the 3 h TTL reuse the session.

- `make ssh` — creates or reuses an OCI Bastion Managed SSH session to the OKE worker node.
- `make tunnel` — runs `kubectl -n monitoring port-forward svc/monitoring-grafana 3000:3000`; it does **not** use Bastion.
- `deploy/scripts/bastion-session.sh diagnose` — read-only checks for Bastion state, node resolution, active sessions, and cache health.

### Onboarding (Per Operator)

Add the operator's OCI IAM user to the Bastion access group with policies for
`manage bastion-session` on the compartment and read access to the target node
metadata. Full runbook: [`docs/Infrastructure.md`](../Infrastructure.md#operator-access-via-oci-bastion).

## Out Of Scope

- **CI migration onto the bastion.** Current CI/CD reaches OKE through
  `oci-kube-setup`; adding Bastion to that path would make routine deploys slower
  and subject to Bastion session TTL limits.
- **Public exposure of Grafana.** Grafana remains ClusterIP-only and is reached
  by `kubectl port-forward`. Public uptime monitoring is handled by external
  SaaS per [ADR-035](ADR-035-oke-free-tier-block-volume-remediation.md).
- **IPv6 / DDNS workarounds.** They solve dynamic IP changes without per-user
  identity or OCI Audit attribution.

## Consequences

**Positive.**

- Dev-machine IP is irrelevant for normal human SSH access.
- Per-user audit trail in OCI Audit (`CreateManagedSshSession` events carry the IAM user's OCID).
- Operator node access is separated from normal Kubernetes diagnostics and deployment.
- Scales to 2–5 users without re-architecting; add IAM users to the group.

**Accepted trade-offs.**

- 3 h session TTL is an OCI service cap. Long interactive debug sessions accept a one-time reconnect; the cache wrapper refreshes on the next `make ssh`.
- Bastion/node-agent reachability is an extra dependency for node-level debugging. Routine Grafana and deployment paths stay available through the Kubernetes API.
- OCI Bastion is regional. Single-region project today, so not an issue; multi-region would need one bastion per region.

## References

- Plan: [`.claude/plans/stable-dev-machine-vps-access.md`](../../.claude/plans/archive/stable-dev-machine-vps-access.md)
- Operator runbook: [`docs/Infrastructure.md`](../Infrastructure.md#operator-access-via-oci-bastion)
- Bastion submodule README: [`deploy/terraform/modules/bastion/README.md`](../../deploy/terraform/modules/bastion/README.md)
- Bastion wrapper: [`deploy/scripts/bastion-session.sh`](../../deploy/scripts/bastion-session.sh)
