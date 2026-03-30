# File Manager View/Download + Headless Capability Detection

## Context

Three related issues to fix:

1. **File Manager can't view or download files** — only directory navigation works. The FileFrame handler is not wired up, and there are no action buttons in the UI.
2. **No file content viewer** — no way to view file contents in-browser.
3. **Agent always advertises RemoteDesktop/Chat** — even on headless Linux (no GUI). These capabilities should be detected at runtime and flow through the full stack so the web UI hides unsupported tabs.

---

## Part A: File Manager View & Download

### A1: Add `viewingFile` state to file store

**Modify**: [file-store.ts](web/src/state/file-store.ts)

- Add `viewingFile: { name: string; content: string } | null`
- Add `setViewingFile(name, content)` / `clearViewingFile()` actions

### A2: Tests for FileFrame handler (TDD)

**New file**: `web/src/features/file-manager/use-file-manager.test.ts`

- `setOnFileFrame` is called when transport exists
- FileFrame handler creates accumulator on first frame and updates progress
- Completed download (mode='download') triggers browser save and clears state
- Completed view (mode='view') sets `viewingFile` in store
- Cleanup calls `setOnFileFrame(null)` on unmount
- `requestDownload`/`requestView` send `FileDownloadRequest` and set initial progress

### A3: Implement FileFrame handler

**Modify**: [use-file-manager.ts](web/src/features/file-manager/use-file-manager.ts)

Follow terminal hook pattern ([use-terminal.ts:61-63](web/src/features/terminal/use-terminal.ts#L61-L63)):

- `useRef` for active transfer: `{ name: string; mode: 'download' | 'view'; accumulator: DownloadAccumulator | null }`
- `useEffect` → `setOnFileFrame(handler)`:
  - First frame: create `DownloadAccumulator(frame.total_size)`, add chunk
  - Subsequent: add chunk, `setDownloadProgress(name, progress)`
  - On complete:
    - `mode === 'download'`: `triggerBrowserSave(name, blob)` → `clearDownload`
    - `mode === 'view'`: read blob as text → `setViewingFile(name, text)` → `clearDownload`
- `requestDownload(path)`: set ref `mode: 'download'`, send `FileDownloadRequest`
- `requestView(path)`: set ref `mode: 'view'`, send `FileDownloadRequest`
- Helper `triggerBrowserSave`: `URL.createObjectURL` → `<a>` click → `URL.revokeObjectURL`

**Design note**: `FileFrame` has no `path` field → one active transfer at a time via ref. Matches agent behavior.

### A4: Tests for UI buttons + viewer (TDD)

**Modify**: [FileManagerView.test.tsx](web/src/features/file-manager/FileManagerView.test.tsx)

- File entries have View and Download buttons (directories do not)
- Clicking download/view sends `FileDownloadRequest` with correct full path
- Buttons disabled while transfer is in progress
- Viewer panel renders when `viewingFile` is set; close button clears it

### A5: Add View/Download buttons + file viewer in UI

**Modify**: [FileManagerView.tsx](web/src/features/file-manager/FileManagerView.tsx)

- Destructure `requestDownload` and `requestView` from `useFileManager()`
- Add View + Download buttons for files (not directories)
- Full path: `${currentPath === '/' ? '' : currentPath}/${entry.name}`
- Disable buttons when `downloads[entry.name] !== undefined`
- File viewer panel: `<pre>` with content, filename header, close button

### A6: Agent empty-file fix

**Modify**: [file_ops.rs](agent/crates/mesh-agent-core/src/file_ops.rs)

After line 95 (`let data = tokio::fs::read(&file_path).await?;`), before chunking loop — if `data.is_empty()`, send a single `FileFrame { offset: 0, total_size: 0, data: vec![] }` and return. Add test `test_stream_download_empty_file`.

---

## Part B: Headless Capability Detection (Full Stack)

The agent currently hardcodes `[RemoteDesktop, Terminal, FileManager]`. On headless Linux (no DISPLAY/WAYLAND_DISPLAY), RemoteDesktop should be omitted. Chat depends on a GUI context too (meaningless without desktop session), so it gets a new capability.

### B1: Add `has_display()` detection to platform-linux

**Modify**: [platform-linux/src/lib.rs](agent/crates/platform-linux/src/lib.rs)

Add pub function:
```rust
/// Returns true if a graphical display server is available.
pub fn has_display() -> bool {
    std::env::var_os("DISPLAY").is_some() || std::env::var_os("WAYLAND_DISPLAY").is_some()
}
```

Add tests: returns false when neither env var set, returns true when DISPLAY set, returns true when WAYLAND_DISPLAY set.

### B2: Conditionally register capabilities in agent

**Modify**: [main.rs](agent/crates/mesh-agent/src/main.rs) lines 420-425

```rust
let mut caps = vec![
    AgentCapability::Terminal,
    AgentCapability::FileManager,
];
if platform_linux::has_display() {
    caps.push(AgentCapability::RemoteDesktop);
}
```

(On Windows, always include RemoteDesktop — Windows always has a desktop.)

### B3: DB migration — add `capabilities` column

**New**: `server/internal/db/migrations/008_device_capabilities.up.sql`
```sql
ALTER TABLE devices ADD COLUMN capabilities TEXT NOT NULL DEFAULT '[]';
```
**New**: `server/internal/db/migrations/008_device_capabilities.down.sql`
```sql
ALTER TABLE devices DROP COLUMN capabilities;
```

JSON text column storing capability array, e.g. `'["Terminal","FileManager"]'`.

### B4: Update Device model + Store

**Modify**: [models.go](server/internal/db/models.go) — add `Capabilities []string` field to `Device`

**Modify**: [sqlite.go](server/internal/db/sqlite.go):
- `UpsertDevice`: serialize `Capabilities` as JSON, include in INSERT/UPDATE
- `scanDevice`: deserialize JSON capabilities column
- All device query functions pick up the new column

**Modify**: [store.go](server/internal/db/store.go) — no interface changes needed (Device struct change propagates)

### B5: Persist capabilities on agent registration

**Modify**: [agentapi/conn.go](server/internal/agentapi/conn.go) `handleRegister()`:
- Convert `msg.Capabilities` ([]AgentCapability) to `[]string`
- Set `device.Capabilities` before `UpsertDevice`

### B6: Expose capabilities in API

**Modify**: [openapi.yaml](api/openapi.yaml) — add `capabilities` array of strings to Device schema

**Regenerate**: `web/src/types/api.d.ts` and `server/internal/api/openapi_gen.go` (via oapi-codegen)

**Modify**: [converters.go](server/internal/api/converters.go) `deviceToAPI()` — map `d.Capabilities`

### B7: Web client — hide unsupported session tabs

**Modify**: [SessionView.tsx](web/src/features/session/SessionView.tsx):
- Accept device capabilities (pass from DeviceDetail via route state, or fetch device in SessionView)
- Filter `TABS` based on capabilities:
  - `Desktop` tab: only if capabilities includes `"RemoteDesktop"`
  - `Chat` tab: only if capabilities includes `"RemoteDesktop"` (chat is meaningless without desktop)
  - `Terminal` and `Files` always shown (always advertised)
- Default active tab to first available tab

**Modify**: [DeviceDetail.tsx](web/src/features/devices/DeviceDetail.tsx):
- Pass device capabilities to session route state so SessionView can use them

---

## Key Files

| File | Action |
|------|--------|
| **Part A — File Manager** | |
| `web/src/state/file-store.ts` | Add `viewingFile` state |
| `web/src/features/file-manager/use-file-manager.ts` | Wire FileFrame handler + view/download |
| `web/src/features/file-manager/use-file-manager.test.ts` | New — hook tests |
| `web/src/features/file-manager/FileManagerView.tsx` | View/Download buttons + viewer panel |
| `web/src/features/file-manager/FileManagerView.test.tsx` | Button + viewer tests |
| `agent/crates/mesh-agent-core/src/file_ops.rs` | Empty-file edge case |
| **Part B — Capability Detection** | |
| `agent/crates/platform-linux/src/lib.rs` | `has_display()` function |
| `agent/crates/mesh-agent/src/main.rs` | Conditional capability registration |
| `server/internal/db/migrations/008_*` | New migration for capabilities column |
| `server/internal/db/models.go` | Add `Capabilities` to Device |
| `server/internal/db/sqlite.go` | Persist/read capabilities JSON |
| `server/internal/agentapi/conn.go` | Store capabilities on register |
| `api/openapi.yaml` | Add capabilities to Device schema |
| `server/internal/api/converters.go` | Map capabilities in API response |
| `server/internal/api/openapi_gen.go` | Regenerated |
| `web/src/types/api.d.ts` | Regenerated |
| `web/src/features/session/SessionView.tsx` | Filter tabs by capabilities |
| `web/src/features/devices/DeviceDetail.tsx` | Pass capabilities to session |

## Reuse

- `DownloadAccumulator` from [file-transfer.ts](web/src/features/file-manager/file-transfer.ts)
- `setOnFileFrame` pattern from [use-terminal.ts:61-71](web/src/features/terminal/use-terminal.ts#L61-L71)
- `has_display()` check mirrors existing `create_screen_capture()` pattern in platform-linux
- Existing `scanDevice()` pattern in sqlite.go for adding new columns

## Verification

1. `make test` — all Rust + Go + TS tests pass
2. `make lint` — clippy + eslint + go vet clean
3. `make golden` — cross-language compat (if protocol types changed)
4. Manual E2E:
   - File Manager: navigate → View text file → content in viewer → close → Download file → browser save
   - Headless: agent on headless Linux → device API shows `["Terminal","FileManager"]` only → SessionView hides Desktop/Chat tabs
   - GUI: agent with DISPLAY set → device shows `["RemoteDesktop","Terminal","FileManager"]` → all tabs visible
