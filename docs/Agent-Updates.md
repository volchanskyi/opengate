# Agent Auto-Update

See also: [ADR-005 in Architecture Decision Records](Architecture-Decision-Records)

## Overview

Agents receive OTA (over-the-air) binary updates pushed from the server via the QUIC control channel. Updates are Ed25519-signed for integrity verification.

## Architecture

```
Admin UI                    Server                           Agent
  │                           │                                │
  │── Publish manifest ──────►│                                │
  │   (version, URL, hash,    │                                │
  │    Ed25519 signature)     │                                │
  │                           │                                │
  │── Push update ───────────►│── AgentUpdate ────────────────►│
  │                           │   (version, url, hash, sig)    │
  │                           │                                │── semver check
  │                           │                                │── download binary
  │                           │                                │── SHA-256 verify
  │                           │                                │── Ed25519 verify
  │                           │                                │── atomic replace
  │                           │                                │── write .update-pending
  │                           │◄── AgentUpdateAck ─────────────│
  │                           │   (version, success, error)    │
  │                           │                                │── exit(42)
  │                           │                                │
  │                           │              systemd restarts (RestartForceExitStatus=42)
  │                           │                                │
  │                           │                                │── startup watchdog
  │                           │◄── AgentRegister ──────────────│── registration OK
  │                           │                                │── clear .update-pending
```

## Components

### Server Side

| Component | Location | Purpose |
|-----------|----------|---------|
| **Signing keys** | `server/internal/updater/signing.go` | Ed25519 key generation, loading, signing, verification |
| **Manifest store** | `server/internal/updater/manifest.go` | Filesystem JSON storage for version manifests |
| **GitHub sync** | `server/internal/updater/github.go` | Periodic sync from GitHub Releases (hourly) |
| **REST API** | `server/internal/api/handlers_updates.go` | List/publish/push manifests, query update status |
| **QUIC handler** | `server/internal/agentapi/conn.go` | Send `AgentUpdate`, handle `AgentUpdateAck` |
| **DB tracking** | `server/internal/db/sqlite.go` | `device_updates` table (pending/success/failed) |

### Agent Side

| Component | Location | Purpose |
|-----------|----------|---------|
| **Update engine** | `agent/crates/mesh-agent-core/src/update.rs` | Download, verify, atomic replace, rollback |
| **Control handler** | `agent/crates/mesh-agent/src/main.rs` | Version comparison, ack, watchdog, signing key management |

## Agent Version Detection

The agent binary version (`AGENT_VERSION`) is determined at build time via `build.rs` with a priority chain:

1. **`OPENGATE_VERSION` env var** — set by CI from the git tag (e.g. `v0.15.4` → `0.15.4`)
2. **`git describe --tags --abbrev=0`** — auto-detects from the nearest git tag (local dev builds)
3. **`CARGO_PKG_VERSION`** — last-resort fallback from `Cargo.toml`

This ensures local builds, CI builds, and release builds all report the correct version without manual bumps.

## Version Comparison

Before applying an update, the agent compares the incoming version against its current version using semver:

- **Incoming > current**: proceed with update
- **Incoming <= current**: skip, send ack with `success=true, error="already up to date"`
- **Parse failure**: fail-open (proceed with update)

## Rollback Mechanism

The agent protects against bad updates with a multi-layer rollback system:

1. **Backup**: `apply_update()` saves the current binary as `{path}.prev` before replacing
2. **Sentinel**: After successful replacement, writes `.update-pending` to the data directory
3. **Watchdog**: On startup, if `.update-pending` exists, a 60-second watchdog starts:
   - If the agent successfully registers with the server, the sentinel is cleared (update verified healthy)
   - If the watchdog fires before registration, the agent calls `rollback()` to restore `.prev` and exits with code 42
4. **Loop protection**: A rollback counter file tracks attempts. After 2 consecutive rollbacks, the agent stops rolling back and clears the sentinel to prevent infinite loops between two bad binaries

## Signing Key Delivery

The Ed25519 public key is delivered to agents automatically during enrollment:

1. Server includes `update_signing_key` (hex-encoded) in the enrollment response
2. Agent saves the key to `{data_dir}/update-signing-key.hex`
3. On startup, if `--update-public-key` CLI flag is not set, the agent loads the key from file
4. CLI flag takes precedence over the saved file

## GitHub Release Sync

The server periodically syncs agent manifests from GitHub Releases:

- Runs immediately on startup, then every hour via `StartPeriodicSync()`
- Configured via `OPENGATE_GITHUB_REPO` environment variable
- Downloads release assets, computes SHA-256 hashes, signs with Ed25519, stores manifests

## Wire Protocol Messages

### AgentUpdate (server → agent)

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | Target version |
| `url` | string | Download URL for the binary |
| `sha256` | string | Expected SHA-256 hash (hex) |
| `signature` | string | Ed25519 signature (hex) |

The public key is delivered out-of-band during enrollment (see [Signing Key Delivery](#signing-key-delivery) above), not per-update.

### AgentUpdateAck (agent → server)

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | Version that was processed |
| `success` | bool | Whether the update was applied |
| `error` | string | Error message (empty on success) |

## Admin UI

The Settings > Agent Updates page (`/settings/updates`) provides:

- **Manifest list**: All published versions with OS/arch, hash, signature
- **Publish form**: Create new manifests manually
- **Push updates**: Push a specific version to eligible agents (filters by OS/arch after normalization)
- **Status view**: Per-device update status (pending/success/failed) after a push
- **Signing key display**: The server's Ed25519 public key for reference

## OS/Arch Normalization

Agents report platform information using native identifiers (e.g. `Ubuntu 22.04.4 LTS`, `x86_64`), while update manifests use GOOS-style values (`linux`, `amd64`). The server normalizes agent-reported values before matching:

| Agent reports | Normalized to |
|--------------|---------------|
| `Ubuntu 22.04 LTS`, `Debian GNU/Linux 12`, `Fedora Linux 40`, `CentOS Stream 9`, `Alpine Linux`, `Arch Linux`, `Red Hat Enterprise Linux` | `linux` |
| `Windows 11 Pro` | `windows` |
| `macOS 14.4` | `darwin` |
| `x86_64` | `amd64` |
| `aarch64` | `arm64` |

This normalization is applied in `eligibleAgents()` in `handlers_updates.go`.
