# ADR-005: Agent Auto-Update

**Status:** Accepted
**Date:** 2026-03-19

## Context

OpenGate agents run on remote machines that may not have SSH access or any
other out-of-band management channel. When a new agent version is released,
administrators need a way to push updates to deployed agents without visiting
each machine.

Requirements:
- Updates must be cryptographically verified (integrity + authenticity).
- The server pushes updates; agents do not poll.
- A failed update must not brick the agent (rollback required).
- The update flow must work with the existing QUIC control channel.
- Manifest metadata must be publishable from CI (GitHub Releases).

## Decision

### Signing

Ed25519 keypair generated at server startup and persisted in
`data/update-signing.json`. The server signs the SHA-256 hash of each binary.
Agents verify using the public key delivered during enrollment or configured
via `--update-public-key`.

### Manifest Storage

One JSON file per OS/arch combination stored on the filesystem at
`data/manifests/{os}-{arch}.json`. Each manifest contains: version, download
URL, SHA-256 hash, and Ed25519 signature.

### Update Flow

1. Admin publishes a manifest (manual or auto-synced from GitHub Releases).
2. Admin pushes the update via the API; server sends `AgentUpdate` control
   message to eligible connected agents.
3. Agent compares the incoming version against `AGENT_VERSION` using semver.
   If incoming <= current, the agent sends `AgentUpdateAck` with
   `success=true, error="already up to date"` and skips the update.
4. Agent downloads the binary, verifies SHA-256 hash, then verifies the
   Ed25519 signature over the hash.
5. Agent backs up the current binary to `{binary}.prev`.
6. Agent writes an `.update-pending` sentinel file.
7. Agent atomically replaces the binary via `rename(2)`.
8. Agent sends `AgentUpdateAck { success: true }`.
9. Agent exits with code 42.
10. systemd (`RestartForceExitStatus=42`) restarts the agent.
11. On startup, the agent detects `.update-pending` and starts a 60-second
    watchdog. If the agent successfully registers with the server, the
    sentinel is cleared. If registration fails, the agent rolls back to
    `.prev` and restarts.

### Rollback Safety

- A `.prev` backup is created before every update.
- A `.update-pending` sentinel triggers the startup watchdog.
- A `.rollback-count` file tracks consecutive rollback attempts. After 2
  rollbacks, the agent stops retrying to prevent infinite loops between two
  bad binaries.

### Server Tracking

The server records each push in a `device_updates` table with status
`pending`. When the agent sends `AgentUpdateAck`, the server updates the
record to `success` or `failed`. The admin UI shows per-device update status.

### Signing Key Delivery

During enrollment, the server includes its Ed25519 public key in the response.
The agent saves it to `{data_dir}/update-signing-key.hex`. On startup, if
`--update-public-key` is not set, the agent loads the key from this file.
The CLI flag takes precedence over the saved file.

### GitHub Release Sync

The server periodically syncs manifests from GitHub Releases (default: every
hour). On each sync it fetches the latest release, downloads `.sha256` sidecar
files, signs the hashes, and stores manifests for each OS/arch binary found.

## Consequences

### Positive
- Secure OTA updates with Ed25519 verification and SHA-256 integrity checks.
- No SSH or out-of-band access required.
- Automatic rollback protects against bad binaries.
- CI publishes releases to GitHub; the server auto-syncs manifests.
- Signing key auto-delivered during enrollment removes manual configuration.

### Negative
- Push-only model: agents that are offline during a push will not receive the
  update until they reconnect and the admin pushes again.
- Single manifest per OS/arch: no canary or staged rollout support.
- Filesystem-based manifest storage (acceptable at current scale).

## Explicitly Deferred

- Agent-initiated pull model (polling for updates).
- Windows/macOS agent support.
- Canary/staged rollout.
- Manifest storage migration to SQLite.
- Ed25519 key rotation mechanism.
