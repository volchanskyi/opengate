# ADR-018: OCI Bastion service for operator SSH + monitoring-UI access

Date: 2026-05-18
Status: Accepted

## Context

OpenGate runs a single OCI VPS with a public IP. Most operator ingress is gated through OCI security lists ([`deploy/terraform/modules/networking/main.tf`](../../deploy/terraform/modules/networking/main.tf)):

- SSH (TCP 22) was pinned to `var.ssh_allowed_cidr`, a static CIDR set once at apply time.
- HTTP/HTTPS/QUIC/MPS sit on `0.0.0.0/0`.
- Grafana (`127.0.0.1:3000`) and Uptime Kuma admin (`127.0.0.1:3001`) bind loopback ([`deploy/docker-compose.monitoring.yml`](../../deploy/docker-compose.monitoring.yml)) and are only reachable via SSH port-forward.

The dev machine sits on a dynamic ISP-issued IP. Every IP change invalidated `ssh_allowed_cidr`, breaking the SSH tunnel that Grafana / Uptime Kuma debugging depends on — operator pain point that surfaced multiple times during the Phase D / SonarCloud / mutation-testing rollouts. CI sidesteps the same problem with the just-in-time NSG-rule pattern in [`.github/actions/oci-ssh-setup/action.yml`](../../.github/actions/oci-ssh-setup/action.yml) (add `${RUNNER_IP}/32` rule, deploy, teardown), but no equivalent existed for the human operator.

Constraints collected up-front:

- **Users**: solo today, small team (2–5) within 6–12 months → must support per-user identity.
- **Frequency**: daily / near-daily → session-setup latency must be ≤ 10 s.
- **Budget**: $0 — Always Free OCI only, no paid SaaS.
- **Access mode**: WSL desktop + CI-side scripts; no mobile.

## Options considered

| Option | Daily-use fit | $0 fit | Multi-user fit | Verdict |
|---|---|---|---|---|
| **OCI Bastion** (managed) | 5–10 s session create, cached for 3 h | Always Free | Native OCI IAM (per-user audit in OCI) | **Adopted** |
| Self-service JIT NSG script | < 2 s, but lingers if the script crashes | Free | Shared OCI key | Fallback only — weak audit |
| DDNS + auto-refresh of `ssh_allowed_cidr` | Per-IP, not per-user | Free | No identity model | Doesn't scale to team |
| Self-hosted WireGuard overlay (Headscale) | < 1 s | Free | Per-peer key | More moving parts, ops burden |
| Tailscale free tier | < 1 s | $0 for ≤ 3 users, then paid | Built-in IAM | Violates "$0 with team growth" |
| Cloudflare Tunnel + Access | < 1 s | Free up to 50 users | Built-in IAM | Adds SaaS dependency; rebinds the security model |

OCI Bastion is the only candidate that satisfies all four constraints simultaneously — Always Free in perpetuity, native IAM, no third-party SaaS, and reuses the OCI toolchain already on every dev machine.

## Decision

Provision an **OCI Bastion** in the production VCN and use **Managed SSH sessions** for human access. CI keeps the existing just-in-time NSG-rule pattern. The bastion replaces the static `ssh_allowed_cidr` rule as the primary operator path; the variable is kept as an emergency-break, typically set to `127.0.0.1/32` to disable the public SSH path entirely.

### Terraform layout

- New submodule [`deploy/terraform/modules/bastion/`](../../deploy/terraform/modules/bastion/) provisions `oci_bastion_bastion.opengate` targeting `opengate-public-subnet`. STANDARD type, `client_cidr_block_allow_list = ["0.0.0.0/0"]` (IAM gates session creation; CIDR is just the L4 envelope filter), `max_session_ttl_in_seconds = 10800` (3 h, OCI service cap).
- [`deploy/terraform/modules/networking/main.tf`](../../deploy/terraform/modules/networking/main.tf) adds a second TCP 22 ingress rule with source = the public subnet's own CIDR (`10.0.1.0/24`). The bastion's allocated /28 service endpoint is carved from that subnet at apply time; the broader source is intra-VCN only and harmless in a single-VM subnet.
- [`deploy/terraform/modules/compute/main.tf`](../../deploy/terraform/modules/compute/main.tf) enables the OCI Cloud Agent **Bastion plugin** via `agent_config.plugins_config`. The plugin establishes the outbound tunnel Managed SSH sessions ride on — without it, sessions fail with a generic "target not reachable" error.
- The networking-module SSH-from-bastion ingress rule uses a `locals.public_subnet_cidr` value referenced by both the security list and the subnet, breaking what would otherwise be a `subnet → security_list_ids → security_list → subnet.cidr_block` resource cycle.

### Operator UX

[`deploy/scripts/bastion-session.sh`](../../deploy/scripts/bastion-session.sh) — pure bash + OCI CLI, no new deps. Caches the active session OCID + expiry under `~/.cache/opengate/bastion-session.json` so daily invocations skip the 5–10 s session create when a session is still live (5-min headroom on the 3 h TTL). Two modes via the Makefile:

- `make tunnel` — Managed SSH session + `-L 3000 -L 3001` for Grafana + Uptime Kuma.
- `make ssh` — same session, plain shell.

### Onboarding (per operator)

One PR-sized change: add the operator's OCI IAM user to a `bastion-users` group with three policies (`manage bastion-session` on the compartment, `read instance` and `read instance-agent-plugins` on the target). Full runbook in [`docs/Infrastructure.md`](../Infrastructure.md#operator-access-via-oci-bastion).

## Out of scope (explicit non-goals)

- **CI migration onto the bastion.** The just-in-time NSG-rule pattern in `oci-ssh-setup` works, has zero per-run cost, and avoids the 3 h session limit constraint on long e2e runs. A future PR may unify the audit trail; this ADR does not commit to it.
- **Public exposure of Grafana / Uptime Kuma behind Caddy + SSO.** Expands the public attack surface and adds a SaaS auth dependency — both at odds with the project's current single-VPS posture.
- **IPv6 / DDNS workarounds.** Solve a narrower problem (IP changes) without addressing the multi-user requirement.

## Consequences

**Positive.**
- Dev-machine IP is irrelevant — no more terraform-apply round trip after an ISP rebind.
- Per-user audit trail in OCI Audit (`CreateManagedSshSession` events carry the IAM user's OCID).
- One install command per operator covers every fleet machine — no `--flags`, no platform shims.
- Scales to 2–5 users without re-architecting (just add IAM users to the group).

**Accepted trade-offs.**
- 3 h session TTL is an OCI service cap. Long interactive debug sessions accept a one-time mid-session reconnect; the cache wrapper handles it transparently on the next `make ssh`.
- Bastion plugin reliability is an additional dependency. Monitored via the existing infrastructure health check; the static `ssh_allowed_cidr` rule stays as break-glass.
- OCI Bastion is regional. Single-region project today, so not an issue; multi-region would need one bastion per region.

## References

- Plan: [`.claude/plans/stable-dev-machine-vps-access.md`](../../.claude/plans/stable-dev-machine-vps-access.md)
- Operator runbook: [`docs/Infrastructure.md`](../Infrastructure.md#operator-access-via-oci-bastion)
- Bastion submodule README: [`deploy/terraform/modules/bastion/README.md`](../../deploy/terraform/modules/bastion/README.md)
