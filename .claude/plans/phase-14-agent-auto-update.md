# Phase 14: Agent Auto-Update

## Context

Phase 14 has **zero dependencies on Phase 13** (Multiserver & Scaling). The agent receives updates via its existing single-server QUIC control stream. No PostgreSQL, peer discovery, relay pool, or Kubernetes needed. The `AgentUpdate` control message is already defined in both Rust and Go wire protocol.

## Scope

**Server**: Ed25519 signing, manifest storage, admin API endpoints (publish/push/signing-key), version tracking in DB.
**Agent**: Update handler (download, verify, atomic replace, restart), new `mesh-agent` binary crate.
**Protocol**: Add `version` to `AgentRegister`, add `AgentUpdateAck` message, golden file tests.
**CI**: Agent release workflow (cross-compile, sign, GitHub Release).

## Implementation Steps

### Step 1: Wire Protocol Changes

**Files:**
- `agent/crates/mesh-protocol/src/control.rs` — add `version: String` to `AgentRegister`, add `AgentUpdateAck { version, success, error }` variant
- `server/internal/protocol/control.go` — add `Version` to `AgentRegister` group (already has `Version` for `AgentUpdate`, but add `Success *bool` + `Error` for `AgentUpdateAck`), add `MsgAgentUpdateAck` const

**Golden files:**
- `agent/crates/mesh-protocol/tests/golden_test.rs` — new golden for `AgentUpdateAck`
- `server/internal/protocol/golden_test.go` — verify `AgentUpdateAck` golden

### Step 2: Database Migration

**Files:**
- `server/internal/db/migrations/003_agent_version.up.sql` — `ALTER TABLE devices ADD COLUMN agent_version TEXT NOT NULL DEFAULT ''`
- `server/internal/db/migrations/003_agent_version.down.sql` — `ALTER TABLE devices DROP COLUMN agent_version`
- `server/internal/db/models.go` — add `AgentVersion string` to `Device`
- `server/internal/db/sqlite.go` — update `UpsertDevice` INSERT/UPDATE and all `scanDevice*` queries to include `agent_version`

### Step 3: Ed25519 Signing Infrastructure

**New package:** `server/internal/updater/`

**Files:**
- `server/internal/updater/signing.go` — `LoadOrGenerateSigningKeys(dataDir)` (pattern: `notifications/vapid.go`), `SignHash(sha256hex)`, `VerifyHash(sha256hex, sigHex)`, `PublicKeyHex()`
- `server/internal/updater/manifest.go` — `Manifest` struct (version/os/arch/url/sha256/signature/created_at), `ManifestStore` (disk-backed at `{dataDir}/manifests/{os}-{arch}.json`), `Put/Get/List` methods
- `server/internal/updater/signing_test.go` — generate/reload keys, sign/verify round-trip, tamper detection
- `server/internal/updater/manifest_test.go` — put/get/list/overwrite, missing returns nil

### Step 4: Server API Endpoints

**OpenAPI spec** (`api/openapi.yaml`):
- `GET /api/v1/updates/manifest` — list manifests (admin)
- `POST /api/v1/updates/manifest` — publish version (admin): `{ version, os, arch, url, sha256 }`
- `POST /api/v1/updates/push` — push to agents (admin): `{ version, os, arch, device_ids? }`
- `GET /api/v1/updates/signing-key` — get public key hex (admin)
- Add `agent_version` to `Device` schema

**Handler file:** `server/internal/api/handlers_updates.go`

**Interface change** (`server/internal/api/api.go`):
- Extend `AgentGetter` with `ListConnectedAgents() []*agentapi.AgentConn`
- Add `Updater` fields to `ServerConfig`/`Server`

**AgentConn changes** (`server/internal/agentapi/conn.go`):
- Add `OS`, `Arch`, `AgentVersion` fields to `AgentConn` struct
- `SendAgentUpdate(ctx, version, url, signature) error` method
- `handleRegister` stores `msg.Version` in DB and `AgentConn.OS`/`AgentVersion`

**AgentServer changes** (`server/internal/agentapi/server.go`):
- `ListConnectedAgents() []*AgentConn` — iterate `sync.Map`

**main.go** (`server/cmd/meshserver/main.go`):
- Init `updater.LoadOrGenerateSigningKeys`, `updater.NewManifestStore`
- Pass to `ServerConfig`

### Step 5: Agent Update Module (Rust)

**New deps** (workspace `agent/Cargo.toml`):
- `ed25519-dalek = "2"`, `sha2 = "0.10"`, `hex = "0.4"`, `reqwest = { version = "0.12", features = ["rustls-tls"], default-features = false }`

**New module:** `agent/crates/mesh-agent-core/src/update.rs`
- `UpdateConfig { signing_public_key: [u8; 32], current_binary_path, data_dir }`
- `apply_update(config, version, url, signature_hex) -> Result<bool, UpdateError>` — download to `.new`, SHA-256, verify Ed25519 sig, backup to `.prev`, atomic `rename(2)`
- `UpdateError` enum (thiserror): `Download`, `SignatureInvalid`, `Io`, `HashMismatch`
- Tests: verify valid/invalid sigs, atomic replace with tempdir, download error handling

### Step 6: Agent Binary Crate

**New crate:** `agent/crates/mesh-agent/`
- `Cargo.toml` — binary, depends on `mesh-agent-core`, `mesh-protocol`, `platform-linux` (optional), `tokio`, `tracing`, `anyhow`, `clap`
- `src/main.rs` — CLI args (server-addr, server-ca, data-dir), tracing init, identity load, QUIC connect, control loop dispatching `AgentUpdate` to `update::apply_update()`
- Version from `env!("CARGO_PKG_VERSION")`, sent in `AgentRegister`
- Exit code 42 = restart after update (systemd `RestartForceExitStatus=42`)

### Step 7: CI Release Workflow

**New file:** `.github/workflows/release-agent.yml`
- Trigger: git tag `v*` on `main`
- Cross-compile: `linux-amd64`, `linux-arm64`
- Compute SHA-256 per binary
- Create GitHub Release, upload binaries + checksums

### Step 8: Integration Tests + Observability

**Go tests** (`server/internal/api/update_handlers_test.go`):
- Publish, list, push (single + all), signing key, not-admin 403

**Audit events**: `update.publish`, `update.push`, `update.applied`, `update.failed`

## Verification

1. `make test` — all existing + new tests pass
2. `make golden` — new `AgentUpdateAck` golden verified cross-language
3. `make lint` — clippy + go vet + eslint + actionlint clean
4. Manual: publish manifest via admin API, push to connected agent, verify agent downloads + verifies + restarts
