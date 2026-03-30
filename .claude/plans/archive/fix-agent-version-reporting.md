# Agent Version Alignment

## Context

The mesh-agent binary reports version `0.8.0` at startup and during server registration, despite the project being at `v0.14.x`. The admin dashboard shows all agents as `0.8.0`, OTA `should_skip_version()` compares against a stale version, and there's no way to tell which release an agent is actually running.

### Root Cause

1. All three agent `Cargo.toml` files have `version = "0.8.0"` (never bumped)
2. `build.rs` supports `OPENGATE_VERSION` env var → `AGENT_VERSION` compile-time constant, but nobody sets it
3. `release-agent.yml` does NOT set `OPENGATE_VERSION` during builds
4. `main.rs:239` startup log uses `env!("CARGO_PKG_VERSION")` instead of `env!("AGENT_VERSION")`

---

## Changes

### 1. Inject version from git tag in release workflow

**File:** `.github/workflows/release-agent.yml`

Add a version extraction step and pass `OPENGATE_VERSION` to both build steps:

```yaml
- name: Determine version
  id: version
  run: |
    TAG="${{ inputs.tag || github.ref_name }}"
    VERSION="${TAG#v}"
    echo "version=$VERSION" >> "$GITHUB_OUTPUT"
    echo "Agent version: $VERSION"
```

Then on both build steps, add:
```yaml
env:
  OPENGATE_VERSION: ${{ steps.version.outputs.version }}
```

### 2. Fix startup log to use `AGENT_VERSION`

**File:** `agent/crates/mesh-agent/src/main.rs` — line 239

Change `env!("CARGO_PKG_VERSION")` → `env!("AGENT_VERSION")` so the startup log matches what's sent to the server.

### 3. Bump Cargo.toml versions to current

Update all three crate versions as fallback for local/dev builds:

- `agent/crates/mesh-agent/Cargo.toml` → `version = "0.14.1"`
- `agent/crates/mesh-agent-core/Cargo.toml` → `version = "0.14.1"`
- `agent/crates/mesh-protocol/Cargo.toml` → `version = "0.14.1"`

These are overridden by `OPENGATE_VERSION` in CI but give local builds a reasonable version.

---

## Files to Modify

| File | Change |
|------|--------|
| `.github/workflows/release-agent.yml` | Add version step, pass `OPENGATE_VERSION` env to build steps |
| `agent/crates/mesh-agent/src/main.rs:239` | `CARGO_PKG_VERSION` → `AGENT_VERSION` |
| `agent/crates/mesh-agent/Cargo.toml` | `version = "0.14.1"` |
| `agent/crates/mesh-agent-core/Cargo.toml` | `version = "0.14.1"` |
| `agent/crates/mesh-protocol/Cargo.toml` | `version = "0.14.1"` |

## Verification

1. `make build` — agent compiles, startup log shows `0.14.1`
2. `OPENGATE_VERSION=99.0.0 cargo build -p mesh-agent` — verify env override works
3. `make test` — all tests pass
4. Inspect release workflow — on tag `v0.15.0`, build sets `OPENGATE_VERSION=0.15.0`
