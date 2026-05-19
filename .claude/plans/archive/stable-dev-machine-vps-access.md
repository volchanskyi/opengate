# Stable dev-machine access to OpenGate VPS (SSH + monitoring UIs)

## Context

OpenGate runs a single OCI VPS with a public IP. Most operator ingress is gated through OCI security lists (see [`deploy/terraform/modules/networking/main.tf`](../../deploy/terraform/modules/networking/main.tf)):

- SSH (TCP 22) → `var.ssh_allowed_cidr` (static — set once at apply time)
- HTTP/HTTPS/QUIC/MPS → `0.0.0.0/0`
- Grafana :3000 and Uptime Kuma admin :3001 bind `127.0.0.1` ([`deploy/docker-compose.monitoring.yml`](../../deploy/docker-compose.monitoring.yml)) → reachable only via SSH tunnel

The dev machine sits on a **dynamic IP**. Every ISP-issued IP change invalidates `ssh_allowed_cidr`, breaking the SSH tunnel that Grafana/Uptime Kuma debugging depends on. CI sidesteps this with a JIT NSG-rule pattern in [`.github/actions/oci-ssh-setup/action.yml`](../../.github/actions/oci-ssh-setup/action.yml) (add `${RUNNER_IP}/32` rule, run deploy, teardown). No equivalent exists for the operator.

User constraints (collected up-front):
- **Users**: solo today, small team (2-5) within 6-12 months → must support per-user identity
- **Frequency**: daily/near-daily → session-setup latency matters (≤10s tolerable, not 30s+)
- **Budget**: $0 — Always Free OCI only, no paid SaaS
- **Access mode**: WSL desktop + CI-side scripts; no mobile

## Recommended approach: OCI Bastion + thin Makefile wrapper

Stand up an **OCI Bastion service** in the production VCN and use **Managed SSH sessions** for human access; keep the existing JIT NSG pattern for CI as-is. The bastion replaces the static `ssh_allowed_cidr` rule with IAM-gated, time-limited sessions; the dev machine's IP becomes irrelevant.

### Why OCI Bastion (vs. alternatives)

| Option | Daily-use fit | $0 fit | Multi-user fit | Verdict |
|---|---|---|---|---|
| **OCI Bastion** (managed) | 5-10s session create, cached for 3h | Always Free | Native OCI IAM | **Recommended** |
| Self-service JIT NSG script | <2s, but lingers if script crashes | Free | Shared OCI key | OK as fallback, weak audit |
| DDNS + auto-refresh of `ssh_allowed_cidr` | Per-IP, not per-user | Free | No identity model | Doesn't scale to team |
| WireGuard self-hosted overlay (Headscale) | <1s once running | Free | Per-peer key | More moving parts, ops burden |
| Tailscale free tier | <1s | $0 for ≤3 users, then paid | Built-in IAM | Violates "$0 with team growth" |
| Cloudflare Tunnel + Access | <1s | Free up to 50 users | Built-in IAM | Adds SaaS dependency; rebinds the security model |

OCI Bastion is the only candidate that satisfies all four constraints simultaneously — Always Free in perpetuity, native IAM (per-user audit in OCI), no third-party SaaS, and reuses the OCI toolchain already on every dev machine.

### How sessions work for this project

OCI Bastion supports two session types — we use the first for daily ops:

1. **Managed SSH session**: opens a Bastion-proxied SSH connection. Standard `ssh -L 3000:localhost:3000 -L 3001:localhost:3001 ubuntu@<vps-private-ip>` works through the proxy, multiplexing Grafana + Uptime Kuma + any future localhost-bound UIs over one TCP connection. Requires the OCI Cloud Agent **Bastion plugin** to be enabled on the VM (free, install via OCI Console or Cloud-Init).
2. **Port-forwarding session**: bastion-allocated direct TCP tunnel to `<target-ip>:<port>`. Not needed if (1) covers daily flow.

Session TTL is capped at **3 hours** (OCI service limit). The Makefile wrapper re-creates a session when the cached one expires.

### Implementation steps

1. **Terraform** — extend [`deploy/terraform/modules/networking/`](../../deploy/terraform/modules/networking/) with a `bastion` module:
   - `oci_bastion_bastion` resource targeting `opengate-public-subnet`, `client_cidr_block_allow_list = ["0.0.0.0/0"]` (IAM gates session creation; CIDR is just the L4 envelope filter)
   - Add an `ingress_security_rules` block (or NSG rule) allowing source = the bastion's allocated `/28` CIDR on TCP 22 → replaces the current `var.ssh_allowed_cidr` rule
   - Remove the static `ssh_allowed_cidr` rule from the security list (kept as an emergency-break terraform variable but set to `127.0.0.1/32` so no public SSH path exists outside the bastion)
   - Output the bastion OCID + target instance OCID for the Makefile wrapper

2. **Cloud-Init / instance config** — enable the OCI Cloud Agent **Bastion plugin**. One-line update in [`deploy/terraform/modules/compute/`](../../deploy/terraform/modules/compute/) cloud-init or via `oci compute instance update --agent-config` post-apply. Already-running Always Free instance: enable via Console or `oci instance-agent plugin-configuration`.

3. **Makefile target + helper script** — add to root `Makefile`:
   - `make tunnel` → creates (or reuses cached) Managed SSH session, then runs `ssh -L 3000:localhost:3000 -L 3001:localhost:3001 ...` and prints `http://localhost:3000` / `http://localhost:3001` for Grafana / Uptime Kuma
   - `make ssh` → same session, plain shell
   - Backing script: `deploy/scripts/bastion-session.sh` — pure bash + OCI CLI, no new deps. Caches the active session OCID + expiry under `~/.cache/opengate/bastion-session.json` so daily invocations skip the 5-10s session-create when a session is still live.

4. **Per-user onboarding doc** — short section in [`docs/Infrastructure.md`](../../docs/Infrastructure.md) listing the prerequisites:
   - OCI IAM user with `manage bastion-session` on the compartment + `read instance` on the VPS
   - `~/.oci/config` profile + SSH key (the same one already configured for `oci compute` calls)
   - `make tunnel` from a fresh checkout
   - Drift safety: `Makefile` target fails fast if the bastion OCID is missing from terraform outputs, prompting `terraform output -raw bastion_ocid` instead of hard-coding.

5. **CI unchanged** — keep `.github/actions/oci-ssh-setup` / `oci-ssh-teardown` on the JIT NSG rule pattern. The bastion is for **human** access only. A future PR could migrate CI onto the bastion for a unified audit trail, but that's out of scope here (the JIT pattern works, has zero per-run cost, and avoids the 3-hour session limit constraint on long e2e runs).

### Critical files to be modified / created

| File | Change |
|---|---|
| [`deploy/terraform/modules/networking/main.tf`](../../deploy/terraform/modules/networking/main.tf) | Replace the static SSH ingress with bastion-CIDR-only ingress |
| `deploy/terraform/modules/bastion/main.tf` (new) | Bastion resource + outputs |
| `deploy/terraform/main.tf` | Wire the bastion module + propagate outputs |
| `deploy/terraform/modules/compute/main.tf` | Enable Cloud Agent bastion plugin |
| `deploy/scripts/bastion-session.sh` (new) | Session create/reuse + SSH-with-tunnels |
| `Makefile` | `tunnel` and `ssh` targets |
| [`docs/Infrastructure.md`](../../docs/Infrastructure.md) | Onboarding section (IAM, prerequisites) |
| [`.claude/decisions.md`](../decisions.md) + `docs/adr/NNNN-oci-bastion-operator-access.md` | New immutable ADR recording the choice |

### Verification

End-to-end check after the first apply:
1. `terraform apply` succeeds; new outputs include `bastion_ocid` and `instance_ocid`.
2. `oci bastion bastion get --bastion-id $(terraform output -raw bastion_ocid)` returns `lifecycleState: ACTIVE`.
3. From a fresh terminal on the dev machine, `make tunnel` completes in <15s on first run, <2s on subsequent runs within the 3h session TTL.
4. `curl -sf http://localhost:3000` returns Grafana's HTML.
5. `curl -sf http://localhost:3001` returns Uptime Kuma's admin HTML.
6. `oci audit event list --compartment-id <id>` shows a `CreateManagedSshSession` event with your IAM user's OCID — confirms per-user audit.
7. Negative path: from a second machine without OCI credentials, `make tunnel` fails with a clear IAM-error message (not a silent timeout).
8. Confirm public SSH closed: `nmap -p 22 $(terraform output -raw instance_public_ip)` from an off-network host shows port 22 as filtered/closed (only the bastion CIDR is allowed).

### Risks and mitigations

- **3-hour session TTL** → Wrapper re-creates sessions transparently. Long-running interactive debug must accept a one-time mid-debug reconnect.
- **OCI Cloud Agent reliability** → Bastion plugin must stay enabled; cloud-init pins it. Monitor via `oci instance-agent plugin status` in the existing infra health check.
- **OCI Bastion is regional** → Single-region project today, so not an issue. If/when multi-region happens, one bastion per region.
- **First-time onboarding friction** → Mitigated by the doc + Makefile wrapper. Each new operator gets one PR-sized change: their IAM user added to the `bastion-users` group.
- **Bastion plugin requires the instance to reach OCI service endpoints** → Egress is `0.0.0.0/0` already; no NSG change needed.

### Out of scope (explicit non-goals)

- Migrating CI workflows onto the bastion (separate decision; JIT pattern works fine).
- Public exposure of Grafana / Uptime Kuma behind Caddy + SSO. Considered, but it expands the public attack surface and adds a SaaS auth dependency — both at odds with the project's current single-VPS posture.
- IPv6 / DDNS workarounds. They solve a narrower problem (IP changes) without addressing the multi-user requirement.
