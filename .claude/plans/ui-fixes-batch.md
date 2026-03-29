# UI & Functionality Fixes (16 Items)

## Context

Multiple UI bugs, missing features, and UX improvements identified during manual testing. The most critical finding is that **remote sessions (terminal, file manager, desktop) are all broken** because `createSession` sends no permissions — the server defaults nil permissions to all-false, so the agent blocks everything. Item 15 (remote cleanup on delete) is the largest change, requiring protocol additions across all three layers.

---

## Batch 1: Trivial UI Text/Style Changes (items 0, 9, 11, 13)

### Item 0 — Default session tab → Terminal
- `web/src/features/session/SessionView.tsx:26`: `useState<Tab>('Desktop')` → `useState<Tab>('Terminal')`

### Item 9 — Rename "Add Device" → "Quick Setup" on dashboard
- `web/src/features/dashboard/Dashboard.tsx:50`: change link text to `Quick Setup`

### Item 11 — Search field placeholder cutoff
- `web/src/features/devices/DeviceSearchBar.tsx:19`: `max-w-sm` → `max-w-md`

### Item 13 — Rename "Agent Updates" → "Agent Settings"
- `web/src/features/admin/AdminLayout.tsx:9`: sidebar label
- `web/src/features/admin/AgentUpdates.tsx:74`: page heading
- `web/src/components/Breadcrumbs.tsx:44-45`: both breadcrumb references

---

## Batch 2: UI Component Changes (items 7, 10, 12, 14)

### Item 7 — Remove redundant "New Token" button from /setup
- `web/src/features/agent-setup/EnrollmentTokenForm.tsx:47-56`: remove the `{!showTokenForm && (...)}` block containing the "New Token" button. The QuickInstallContent's "Create Token" button already controls `showTokenForm`.

### Item 10 — Remove Platform/Install Script/Manual Install from /setup
- `web/src/features/agent-setup/AgentSetupPage.tsx`:
  - Line 87: remove `<InstallInstructions manifests={manifests} />`
  - Line 5: remove `import { InstallInstructions }`
  - Lines 9, 12: remove `manifests` and `fetchManifests` (only used by InstallInstructions)
  - Line 20-21: remove `fetchManifests()` from useEffect
- Can delete `web/src/features/agent-setup/InstallInstructions.tsx` entirely

### Item 12 — Show token exhausted/expired status on /settings/updates
- `web/src/features/admin/AgentUpdates.tsx:146-187`: the token table lacks status badges
- Add a `Status` column. For each token row compute:
  - `expired = new Date(t.expires_at) <= new Date()`
  - `exhausted = t.max_uses > 0 && t.use_count >= t.max_uses`
- Render badges matching the style in `EnrollmentTokenForm.tsx:134-148`:
  - Red "Expired", yellow "Exhausted", green "Active"

### Item 14 — Hide signing key behind toggle
- `web/src/features/admin/AgentUpdates.tsx:200-220`
- Add `const [showSigningKey, setShowSigningKey] = useState(false)`
- Replace the `<code>` block with a conditional: show "Show The Key" button when hidden, show key + "Hide The Key" + "Copy to clipboard" when visible

---

## Batch 3: Session Permissions & Cleanup (items 1, 2, 3) — HIGHEST VALUE

### Root cause (items 2 & 3)
`session-store.ts:31` sends `{ device_id: deviceId }` with **no permissions**.
`converters.go:79-81` defaults nil permissions to all-false `Permissions{}`.
Agent receives `terminal=false, desktop=false, file_read=false` → blocks everything.

### Fix for items 2 & 3 — Pass permissions in createSession
- `web/src/state/session-store.ts:31`: change to:
  ```ts
  api.POST('/api/v1/sessions', {
    body: {
      device_id: deviceId,
      permissions: {
        desktop: true,
        terminal: true,
        file_read: true,
        file_write: false,
        input: true,
      },
    },
  })
  ```
- Server safety net — `server/internal/api/converters.go:79-82`: change nil default to all-true (except file_write):
  ```go
  if p == nil {
      return protocol.Permissions{Desktop: true, Terminal: true, FileRead: true, Input: true}
  }
  ```

### Fix for item 1 — Clean up sessions on disconnect
- `web/src/features/session/SessionView.tsx`:
  - Import `useSessionStore`
  - In `handleDisconnect` (line 39): call `deleteSession(token)` before `disconnect()`
  - In useEffect cleanup (line 32-34): call `useSessionStore.getState().deleteSession(token)` before `disconnect()`

---

## Batch 4: AMT Instructions (items 6 & 8)

### Add "AMT Setup" button + instructions modal on devices page
- `web/src/features/devices/DeviceList.tsx:43-47`: add an "AMT Setup" button next to "Add Device"
- Create a simple modal/section explaining how to configure CIRA:
  - Open AMT BIOS (Ctrl+P at boot or via Intel MEBX)
  - Set MPS server address to the OpenGate server hostname
  - Set provisioning credentials
  - Device will auto-appear in the device list once CIRA connects
- No backend changes needed — AMT devices auto-register via MPS/CIRA

---

## Batch 5: Agent Version & OS Info (items 4, 5)

### Item 4 — Sync agent version with release tags
- `.github/workflows/release-agent.yml`: add a step before building that patches Cargo.toml:
  ```yaml
  - name: Set version from tag
    run: |
      TAG="${{ inputs.tag || github.ref_name }}"
      VERSION="${TAG#v}"
      sed -i "s/^version = \".*\"/version = \"$VERSION\"/" agent/crates/mesh-agent/Cargo.toml
      sed -i "s/^version = \".*\"/version = \"$VERSION\"/" agent/crates/mesh-agent-core/Cargo.toml
      sed -i "s/^version = \".*\"/version = \"$VERSION\"/" agent/crates/mesh-protocol/Cargo.toml
  ```
- The agent already reports `env!("CARGO_PKG_VERSION")` in `AgentRegister` (`main.rs:381`)

### Item 5 — OS text with build versions
- `agent/crates/mesh-agent/src/main.rs:379`: replace `std::env::consts::OS.to_string()` with a `pretty_os()` helper
- Linux: read `/etc/os-release`, parse `PRETTY_NAME` (e.g. "Ubuntu 22.04.5 LTS")
- Windows: fallback to `std::env::consts::OS` for now (Windows support is stub)
- No server changes needed — `Device.OS` is a varchar that stores whatever the agent sends

---

## Batch 6: Remote Agent Cleanup on Delete (item 15) — LARGEST CHANGE

### Protocol additions

**Rust** — `agent/crates/mesh-protocol/src/control.rs`:
- Add `AgentCleanup` variant to `ControlMessage` enum
- Add `AgentDeregistered` variant (for reconnect rejection)

**Go** — `server/internal/protocol/control.go`:
- Add `MsgAgentCleanup` and `MsgAgentDeregistered` constants

**TypeScript** — `web/src/lib/protocol/types.ts`:
- Add to ControlMessage union (for completeness)

### Server changes

**Delete handler** — `server/internal/api/handlers_devices.go:87-106`:
- Before deleting from DB, check `s.agents.GetAgent(deviceID)`
- If connected: call `agentConn.SendAgentCleanup(ctx)` (best-effort, continue on error)
- Then delete from DB as before

**New method** — `server/internal/agentapi/conn.go`:
- Add `SendAgentCleanup(ctx)` mirroring `SendSessionRequest` pattern
- Encode `MsgAgentCleanup` control message and write to stream

**Reconnect rejection** — `server/internal/agentapi/server.go` (handleConn / handleRegister):
- After `handleRegister`, check if device exists in DB
- If device not found: send `AgentDeregistered` control message and close connection
- This handles offline-at-delete-time agents that reconnect later

### Agent changes

**Cleanup handler** — `agent/crates/mesh-agent/src/main.rs`:
- Add match arm for `ControlMessage::AgentCleanup` in the control loop
- Call `perform_cleanup()` then break the outer loop

**Deregistered handler** — same file:
- Add match arm for `ControlMessage::AgentDeregistered`
- Same cleanup flow as above

**Cleanup function** — new function in `main.rs`:
```rust
fn perform_cleanup() -> anyhow::Result<()> {
    let script = "#!/bin/sh\nsleep 2\nsystemctl stop mesh-agent 2>/dev/null || true\n...";
    // Write to /tmp/opengate-cleanup.sh, chmod +x, nohup execute, return
}
```
- Writes a self-destruct shell script to `/tmp/opengate-cleanup.sh`
- Script: stop service, disable service, remove binary, remove config dirs, remove unit file, daemon-reload, remove self
- Executes with `nohup` so it survives the agent process exiting

### Golden tests
- Add `golden_control_frame_agent_cleanup.bin` and `golden_control_frame_agent_deregistered.bin`
- Rust: `agent/crates/mesh-protocol/tests/golden_test.rs`
- Go: `server/internal/protocol/golden_test.go`
- Generate with `GENERATE_GOLDEN=1 cargo test`, verify with `make golden`

---

## Verification

After all batches:
1. `make lint` — all linters pass (including actionlint for release-agent.yml changes)
2. `make test` — all Go/Rust/web tests pass
3. `make golden` — cross-language protocol compatibility
4. `make e2e` — Playwright E2E tests
5. Manual verification:
   - Start a session → Terminal tab is default, terminal accepts input, file manager lists files
   - Disconnect session → navigate back to device detail → no stale sessions shown
   - Delete a device (agent online) → agent uninstalls itself
   - /setup page: no install instructions section, no redundant token button
   - /settings/updates: renamed to "Agent Settings", token statuses shown, signing key hidden by default
   - Dashboard: "Quick Setup" button text
   - Search bar: placeholder text not cut off
