# Agent Download, Enrollment & Update Management UI

## Context

When users log into the OpenGate web UI, they see only a Groups sidebar and device list with no guidance on how to add devices. There is no way to download the agent binary, get installation instructions, or manage agent updates. The server-side update system (Phase 14) is fully implemented but has zero web UI coverage.

**Goal**: Add an enrollment token flow for one-liner agent installation, an Agent Setup page for download/instructions, an Admin Agent Updates page for manifest management, and improve DeviceList empty states with CTAs.

---

## Workstream 1: DB — Enrollment Tokens

### Migration `004_enrollment_tokens.up.sql`
```sql
CREATE TABLE enrollment_tokens (
    id        TEXT PRIMARY KEY,
    token     TEXT NOT NULL UNIQUE,
    label     TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL REFERENCES users(id),
    max_uses  INTEGER NOT NULL DEFAULT 0,  -- 0 = unlimited
    use_count INTEGER NOT NULL DEFAULT 0,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### Model: `server/internal/db/models.go`
Add `EnrollmentToken` struct: ID (uuid), Token (string), Label, CreatedBy (UserID), MaxUses, UseCount (int), ExpiresAt, CreatedAt (time.Time).

### Store interface: `server/internal/db/store.go`
Add under `// Enrollment Tokens`:
- `CreateEnrollmentToken(ctx, *EnrollmentToken) error`
- `GetEnrollmentTokenByToken(ctx, token string) (*EnrollmentToken, error)`
- `ListEnrollmentTokens(ctx, createdBy UserID) ([]*EnrollmentToken, error)`
- `DeleteEnrollmentToken(ctx, id uuid.UUID) error`
- `IncrementEnrollmentTokenUseCount(ctx, id uuid.UUID) error`

### SQLite impl: `server/internal/db/sqlite.go`
Follow exact patterns of existing CRUD methods (AMTDevice, WebPush, etc.).

### Tests: `server/internal/db/sqlite_test.go`
Table-driven tests: create, get, list, delete, increment use_count, get expired token, get exhausted token.

---

## Workstream 2: OpenAPI Spec + Code Generation

### `api/openapi.yaml` — Add schemas
- `EnrollmentToken`: id, token, label, created_by, max_uses, use_count, expires_at, created_at
- `CreateEnrollmentTokenRequest`: label (optional), max_uses (default 0), expires_in_hours (default 24)
- `EnrollResponse`: ca_pem, server_addr (QUIC), server_domain
- `CACertResponse`: pem

### `api/openapi.yaml` — Add paths
- `GET /api/v1/enrollment-tokens` (admin) — list tokens
- `POST /api/v1/enrollment-tokens` (admin) — create token
- `DELETE /api/v1/enrollment-tokens/{id}` (admin) — revoke token
- `POST /api/v1/enroll/{token}` (public, no auth) — returns CA PEM + server addr; increments use_count; validates expiry & max_uses
- `GET /api/v1/server/ca` (auth required) — returns CA PEM
- `GET /api/v1/server/install.sh` (public) — serves the install script

### Regenerate
- Go: `cd server && go generate ./internal/api/...` (uses `oapi-codegen.yaml`)
- TS: `cd web && npx openapi-typescript ../api/openapi.yaml -o src/types/api.d.ts`

This also fixes the stale `Device` type — `agent_version` already exists in the OpenAPI spec but is missing from the current generated TS types.

---

## Workstream 3: Go Server — Enrollment & CA Endpoints

### `server/internal/api/api.go`
- Add `CertProvider` interface: `CACertPEM() []byte`
- Add `cert CertProvider` field to `ServerConfig` and `Server`
- Wire in `NewServer`
- Reuse: `cert.Manager` already implements `CACertPEM() []byte` at `server/internal/cert/cert.go:49`

### `server/internal/api/handlers_enrollment.go` (new)
- `CreateEnrollmentToken` — admin only; generates crypto/rand token (32 bytes hex); stores in DB; returns token object
- `ListEnrollmentTokens` — admin only; returns all tokens for the current user
- `DeleteEnrollmentToken` — admin only; deletes by ID
- `Enroll` — public (no auth); validates token not expired/exhausted; increments use_count; returns `{ca_pem, server_addr, server_domain}`
  - `server_addr`: derive from the HTTP request's Host header + `:9090` (QUIC port)
  - `ca_pem`: from `s.cert.CACertPEM()`
- `GetServerCA` — auth required; returns `{pem: string(s.cert.CACertPEM())}`

### `server/internal/api/handlers_install.go` (new)
- Serve the install script at `GET /api/v1/server/install.sh`
- Script is embedded via `//go:embed` from `server/internal/api/install.sh`
- Content-Type: `text/x-shellscript`
- No auth required (script itself is not sensitive; enrollment token provides auth)

### `server/cmd/meshserver/main.go`
- Wire `certMgr` into `api.ServerConfig{Cert: certMgr}`

### Tests: `server/internal/api/handlers_enrollment_test.go`
- TestCreateEnrollmentToken — success, non-admin 403
- TestListEnrollmentTokens — returns created tokens
- TestDeleteEnrollmentToken — success, not found
- TestEnroll — success (returns CA PEM + server addr), expired token 410, exhausted token 410, invalid token 404
- TestGetServerCA — success 200, unauthenticated 401

---

## Workstream 4: Install Script

### `server/internal/api/install.sh` (new, embedded via go:embed)

Bash script that:
1. Detects OS (`uname -s` → linux) and arch (`uname -m` → amd64/arm64)
2. Accepts enrollment token as first argument: `curl ... | bash -s -- <TOKEN>`
3. Calls `POST /api/v1/enroll/<TOKEN>` on the server (derives server URL from `$OPENGATE_SERVER` env or the curl source URL)
4. Receives JSON: `{ca_pem, server_addr, server_domain}`
5. Finds latest agent binary URL from `GET /api/v1/updates/manifests` (filters by OS/arch, picks newest version)
6. Downloads binary from the manifest URL (GitHub Releases)
7. Verifies SHA256 checksum
8. Installs to `/usr/local/bin/mesh-agent`
9. Creates `/etc/opengate-agent/ca.pem` from response
10. Creates systemd unit `/etc/systemd/system/mesh-agent.service`:
    ```ini
    [Unit]
    Description=OpenGate Agent
    After=network-online.target
    Wants=network-online.target

    [Service]
    ExecStart=/usr/local/bin/mesh-agent \
      --server-addr <server_addr> \
      --server-ca /etc/opengate-agent/ca.pem \
      --data-dir /var/lib/opengate-agent
    Restart=on-failure
    RestartForceExitStatus=42
    User=root

    [Install]
    WantedBy=multi-user.target
    ```
11. `systemctl daemon-reload && systemctl enable --now mesh-agent`
12. Prints success message with device ID from agent logs

**Error handling**: verify root, verify systemd, verify curl/wget available, clean up on failure.

---

## Workstream 5: Web Client — Zustand Store

### `web/src/state/update-store.ts` (new)
Follow pattern of `web/src/state/admin-store.ts`:
- `manifests: AgentManifest[]`, `enrollmentTokens: EnrollmentToken[]`, `caPem: string | null`
- `isLoading`, `error`
- Actions:
  - `fetchManifests()` → `api.GET('/api/v1/updates/manifests')`
  - `publishManifest(body)` → `api.POST('/api/v1/updates/manifests', {body})`
  - `pushUpdate(body)` → `api.POST('/api/v1/updates/push', {body})`
  - `fetchEnrollmentTokens()` → `api.GET('/api/v1/enrollment-tokens')`
  - `createEnrollmentToken(body)` → `api.POST('/api/v1/enrollment-tokens', {body})`
  - `deleteEnrollmentToken(id)` → `api.DELETE('/api/v1/enrollment-tokens/{id}', ...)`
  - `fetchCACert()` → `api.GET('/api/v1/server/ca')`

### Tests: `web/src/state/update-store.test.ts`
Mock `../lib/api`, test each action for success + error paths.

---

## Workstream 6: Web Client — Agent Setup Page

### `web/src/features/agent-setup/AgentSetupPage.tsx` (new)

Accessible at `/setup` (all authenticated users). Sections:

1. **Quick Install** — the primary CTA:
   - If enrollment tokens exist (admin created one), show the one-liner:
     ```
     curl -sL https://<domain>/api/v1/server/install.sh | sudo bash -s -- <TOKEN>
     ```
   - "Copy" button next to it
   - For admins: inline "Create Token" button if no tokens exist

2. **Platform selector** — buttons: "Linux x86_64" / "Linux ARM64"
   - Shows download link from latest manifest for selected platform
   - If no manifests published: "No agent binaries published yet" message

3. **Manual Install** — expandable section with step-by-step:
   - Download binary (link from manifest)
   - Save CA certificate (if admin, show "Download CA" button via `fetchCACert`)
   - Run command with flags
   - Set up systemd service

4. **What happens next** — brief explanation: agent connects via QUIC, appears in device list under a group

### Tests: `web/src/features/agent-setup/AgentSetupPage.test.tsx`
- Renders platform selector
- Shows one-liner with enrollment token
- Shows "no versions published" when manifests empty
- Shows manual install section
- Admin sees "Create Token" button

---

## Workstream 7: Web Client — Admin Agent Updates Page

### `web/src/features/admin/AgentUpdates.tsx` (new)

Admin-only page at `/admin/updates`. Three sections:

1. **Enrollment Tokens** — table: Label, Token (masked, click to reveal), Uses (count/max), Expires, Created. Actions: Copy, Delete. "Create Token" button with form: label, max_uses, expires_in_hours.

2. **Published Manifests** — table: Version, OS, Arch, URL (truncated link), SHA256 (truncated), Created At. "Publish New Version" form: version, os (dropdown), arch (dropdown), url, sha256.

3. **Push Updates** — select manifest from list, optionally filter by device IDs, "Push to Agents" button, shows pushed_count result.

Reuse table/form styling patterns from `web/src/features/admin/UserManagement.tsx` and `web/src/features/admin/AuditLog.tsx`.

### Tests: `web/src/features/admin/AgentUpdates.test.tsx`
- Renders enrollment token table
- Renders manifest table
- Publish form submits correctly
- Create token form works
- Empty states

---

## Workstream 8: Web Client — Routes, Layout, Empty States

### `web/src/router.tsx`
- Add `{ path: 'setup', element: <AgentSetupPage /> }` inside AuthGuard > Layout children
- Add `{ path: 'updates', element: <AgentUpdates /> }` inside AdminGuard > AdminLayout children

### `web/src/components/Layout.tsx`
Add "Add Device" link in navbar (visible to all authenticated users):
```tsx
<Link to="/setup" className="text-sm text-gray-400 hover:text-white">Add Device</Link>
```

### `web/src/features/admin/AdminLayout.tsx`
Add to `navItems`: `{ to: '/admin/updates', label: 'Agent Updates' }`

### `web/src/features/devices/DeviceList.tsx`
Replace empty states with CTAs:
- "Select a group" → "Welcome to OpenGate" + "Add Device" button linking to `/setup`
- "No devices in this group" → "Download and install the agent" + "Download Agent" button linking to `/setup`

### `web/src/features/devices/DeviceDetail.tsx`
Add `agent_version` field to the detail grid (available after type regeneration).

### Test updates
- `DeviceList.test.tsx` — update assertions for new empty-state text, assert CTA links present
- `DeviceCard.test.tsx` / `DeviceDetail.test.tsx` — add `agent_version` to test fixtures

---

## Dependency Order

```
Workstream 1 (DB migration + store)
    ↓
Workstream 2 (OpenAPI spec + codegen)
    ↓
Workstream 3 (Go enrollment handlers)  +  Workstream 4 (install script)
    ↓
Workstream 5 (TS update store)
    ↓
Workstreams 6, 7, 8 (UI pages — independent of each other)
```

---

## Files Summary

### New files (16)
| File | Purpose |
|------|---------|
| `server/internal/db/migrations/004_enrollment_tokens.up.sql` | Create enrollment_tokens table |
| `server/internal/db/migrations/004_enrollment_tokens.down.sql` | Drop enrollment_tokens table |
| `server/internal/api/handlers_enrollment.go` | Enrollment token CRUD + enroll + CA endpoints |
| `server/internal/api/handlers_enrollment_test.go` | Enrollment handler tests |
| `server/internal/api/handlers_install.go` | Install script serving (go:embed) |
| `server/internal/api/install.sh` | Agent install script (embedded) |
| `web/src/state/update-store.ts` | Zustand store for manifests + enrollment tokens |
| `web/src/state/update-store.test.ts` | Store tests |
| `web/src/features/agent-setup/AgentSetupPage.tsx` | Agent download + setup page |
| `web/src/features/agent-setup/AgentSetupPage.test.tsx` | Page tests |
| `web/src/features/admin/AgentUpdates.tsx` | Admin manifest + token management |
| `web/src/features/admin/AgentUpdates.test.tsx` | Admin page tests |

### Modified files (11)
| File | Change |
|------|--------|
| `api/openapi.yaml` | Add enrollment, CA, install.sh schemas + paths |
| `server/internal/db/models.go` | Add `EnrollmentToken` struct |
| `server/internal/db/store.go` | Add enrollment token methods to Store interface |
| `server/internal/db/sqlite.go` | Implement enrollment token CRUD |
| `server/internal/db/sqlite_test.go` | Add enrollment token DB tests |
| `server/internal/api/api.go` | Add `CertProvider` interface + `cert` field |
| `server/cmd/meshserver/main.go` | Wire `certMgr` into ServerConfig |
| `web/src/types/api.d.ts` | Regenerated (auto) |
| `web/src/router.tsx` | Add `/setup` and `/admin/updates` routes |
| `web/src/components/Layout.tsx` | Add "Add Device" nav link |
| `web/src/features/admin/AdminLayout.tsx` | Add "Agent Updates" sidebar item |
| `web/src/features/devices/DeviceList.tsx` | Replace empty states with CTAs |
| `web/src/features/devices/DeviceDetail.tsx` | Show agent_version |

### Regenerated files (2)
| File | Command |
|------|---------|
| `server/internal/api/openapi_gen.go` | `cd server && go generate ./internal/api/...` |
| `web/src/types/api.d.ts` | `cd web && npx openapi-typescript ../api/openapi.yaml -o src/types/api.d.ts` |

---

## Key Patterns to Reuse

- **Zustand store**: `web/src/state/admin-store.ts` — exact pattern for `set({ isLoading, error })` / `api.GET` / error handling
- **Admin page layout**: `web/src/features/admin/UserManagement.tsx` — table + form styling
- **DB CRUD**: `server/internal/db/sqlite.go` — AMTDevice methods are closest analog
- **Handler pattern**: `server/internal/api/handlers_updates.go` — admin-only checks, audit logging
- **`cert.Manager.CACertPEM()`**: `server/internal/cert/cert.go:49` — already returns `[]byte`
- **API client**: `web/src/lib/api.ts` — openapi-fetch with auth middleware

---

## Verification

1. **DB**: `cd server && go test ./internal/db/... -run Enrollment -v`
2. **API handlers**: `cd server && go test ./internal/api/... -run Enrollment -v` and `-run ServerCA`
3. **Web store**: `cd web && npx vitest run src/state/update-store.test.ts`
4. **Web components**: `cd web && npx vitest run src/features/agent-setup/ src/features/admin/AgentUpdates`
5. **Full suite**: `make test && make lint`
6. **E2E smoke**: Build Docker image → `docker compose -f deploy/docker-compose.test.yml up` → visit `http://localhost:8080/setup` → verify page loads with platform selector
7. **Install script**: Create enrollment token via API → `curl -sL http://localhost:8080/api/v1/server/install.sh | bash -s -- <TOKEN>` on a test VM
