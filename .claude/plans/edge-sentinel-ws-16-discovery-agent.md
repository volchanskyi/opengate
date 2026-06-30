# WS-16 — Auto-discovery collectors + DiscoveryReport wire (agent)

**Objective:** Zero-config edge profiling — periodically discover listening ports, services, running
DB engines, containers, and installed packages — and ship a tenant-tagged `DiscoveryReport`. Server
storage + UI are WS-17/WS-18.

**Dependencies:** WS-3/WS-10 (wire), WS-1 (capability). **Blocks:** WS-17. **Wave:** with WS-12.

## Context

Inventory today is shallow + on-demand: `collect_hardware_info()` (CPU/disks/networks via `sysinfo`)
at [main.rs:739](../../agent/crates/mesh-agent/src/main.rs#L739). Netdata's auto-discovery profiles
the host on install and re-profiles as new components appear. This WS adds **non-intrusive, read-only
discovery loops** (no slow WMI, no network scanning).

## File inventory

- **Create:** `mesh-agent-core/src/discovery/` collectors — **ports** (`/proc/net` on Linux;
  `GetExtendedTcpTable` via the MIT/Apache `windows` crate), **services** (systemd list-units /
  Windows service manager), **DB engines** (process+port heuristics), **containers** (read-only
  docker/podman/containerd socket if present), **packages** (dpkg/rpm / Windows registry).
- **Modify:** [`control.rs`](../../agent/crates/mesh-protocol/src/control.rs) / Go protocol — additive
  `DiscoveryReport { ts, org_id, ports[], services[], db_engines[], containers[], packages[] }`;
  goldens.
- **Modify:** [`main.rs`](../../agent/crates/mesh-agent/src/main.rs) — periodic discovery task
  (default-off; long interval; bounded; yields; re-profiles on change).

## Steps (TDD-first)

1. **Test first:** collector tests over fixtures (Linux + Windows) — ports/services/DBs/containers/
   packages parse correctly; non-intrusive; bounded → implement collectors.
2. **Test first (cross-lang):** `DiscoveryReport` round-trip + capability gating; `make golden`.
3. Spawn the periodic discovery task (default-off; change-triggered re-profile).

## Gotchas / constraints

- **Read-only + non-intrusive:** OS/localhost introspection only — no WMI, no network port scanning.
- **Per-device caps** on packages / services / ports / processes (top-N + truncation) so a busy host
  cannot explode the report or the inventory table; long discovery interval (change-triggered).
- **No secrets in the report:** engine + port + version only (never DB connection strings/credentials).
- Additive, capability-gated; graceful no-op where a source is absent (cross-platform).
- No `unwrap()`; `#[non_exhaustive]`; `///` docs.

## Reviewer checklist

- [ ] Collectors read-only + non-intrusive (no WMI, no net scan); Linux + Windows; bounded.
- [ ] `DiscoveryReport` additive + capability-gated; goldens green; no secrets in payload.
- [ ] Discovery task default-off; re-profiles on change; yields; `/precommit` green.

## Verification

`cd agent && cargo test -p mesh-agent-core`; `make golden`. `/precommit` green. `/docs`: agent
architecture page.
