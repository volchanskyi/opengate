# Web UI API Coverage Gap Closure

## Context

The OpenGate backend exposes 39 REST + 1 WebSocket endpoints. The web UI covers ~30 of them but has no UI for AMT power actions, signing key display, user self-edit profile, or device group reassignment. The landing page drops users directly into the device list with no overview dashboard. There is no search, no toast notification system, and no breadcrumb navigation.

This plan closes every actionable API gap, adds missing UX primitives, and restructures navigation to match device-management-platform conventions.

---

## API Coverage Audit

### Fully Covered (30 endpoints)
- Auth: login, register, get-me
- Devices: list, get, delete
- Groups: list, create, get, delete
- Sessions: list, create, delete + WebSocket relay
- Users (admin): list, update (admin toggle only), delete
- Audit: query
- Updates: list/publish manifests, push update
- Enrollment: list/create/delete tokens
- Push: subscribe, unsubscribe, vapid-key
- Security Groups: all 6 CRUD+membership endpoints
- Server: install.sh (used in setup page curl command)

### Not Covered — Needs UI (5 endpoints)
| Endpoint | Gap |
|----------|-----|
| `GET /api/v1/amt/devices` | AMT device list (integrate into Devices page) |
| `GET /api/v1/amt/devices/{uuid}` | AMT device detail (integrate into DeviceDetail) |
| `POST /api/v1/amt/devices/{uuid}/power` | AMT power actions (add to DeviceDetail) |
| `GET /api/v1/updates/signing-key` | Signing key display (fold into Agent Updates page) |
| `PATCH /api/v1/users/{id}` | Self-service profile edit (new ProfilePage) |

### Intentionally Excluded from UI
| Endpoint | Reason |
|----------|--------|
| `GET /api/v1/health` | Monitoring-only (Uptime Kuma, Prometheus) — no end-user benefit |
| `GET /api/v1/server/ca` | No need to expose CA cert download in UI |
| `POST /api/v1/enroll/{token}` | Agent-facing — not for browser UI |

### Missing Backend (needs new API)
| Feature | Why |
|---------|-----|
| Device group reassignment | No `PATCH /api/v1/devices/{id}` endpoint exists |

---

## Current Navigation

```
Top navbar: "OpenGate" | "Add Device" | "Admin" (admin) || Bell | Username | Logout

Routes:
  /              → redirect /devices
  /devices       → DeviceList + GroupSidebar
  /devices/:id   → DeviceDetail (shows both regular + AMT sessions)
  /sessions/:tok → SessionView (Desktop/Terminal/Files/Chat tabs)
  /setup         → AgentSetupPage
  /admin         → redirect /admin/users
  /admin/users         → UserManagement
  /admin/audit         → AuditLog
  /admin/updates       → AgentUpdates
  /admin/security/permissions → Permissions
```

## Proposed Navigation

```
Top navbar: "OpenGate" (→ Dashboard) | "Devices" | "Settings" (admin) || Bell | Username (→ Profile) | Logout

Routes:
  /              → Dashboard (NEW)
  /devices       → DeviceList + GroupSidebar + SearchBar + "Add Device" / "Add AMT Device" buttons (ENHANCED)
  /devices/:id   → DeviceDetail + AMT power actions + group reassignment (ENHANCED)
  /sessions/:tok → SessionView (unchanged)
  /setup         → AgentSetupPage (unchanged, linked from "Add Device" button)
  /profile       → ProfilePage (NEW)
  /settings              → redirect /settings/users
  /settings/users        → UserManagement (unchanged)
  /settings/audit        → AuditLog (unchanged)
  /settings/updates      → AgentUpdates + Signing Key section (ENHANCED)
  /settings/security/permissions → Permissions (unchanged)

Settings sidebar sections:
  Management: Users | Audit Log | Agent Updates
  Security:   Permissions
```

Key changes:
- **"Admin" → "Settings"** in navbar and all routes (`/admin/*` → `/settings/*`)
- **"Add Device" moves** from top navbar into DeviceList page as action button alongside new "Add AMT Device" button
- **No separate AMT admin page** — AMT devices appear in the main device list; power actions live on DeviceDetail
- **Signing key** folded into existing Agent Updates page as a new section
- **Health & CA cert** excluded from UI (monitoring-only and unnecessary respectively)

---

## Implementation Phases

### Phase 1: Toast Notification System

**Why:** Every subsequent phase needs success/error feedback. Currently all feedback is inline `{error && ...}` blocks. Foundation for all other work.

**Create:**
- `web/src/state/toast-store.ts` — `toasts: Toast[]`, `addToast(msg, type)`, `removeToast(id)`. Type: `{ id, message, type: 'success'|'error'|'info', duration? }`. Auto-dismiss via setTimeout (5s default).
- `web/src/components/ToastContainer.tsx` — Fixed bottom-right, renders toast stack, max 5 visible. Colors match existing patterns: green-900/700 success, red-900/700 error, blue-900/700 info.

**Modify:**
- `web/src/components/Layout.tsx` — Add `<ToastContainer />` after `<Outlet />`

---

### Phase 2: Navigation Restructure (Admin → Settings)

**Why:** Rename "Admin" to "Settings" across all routes and labels. Move "Add Device" from navbar into DeviceList. Prerequisite for clean integration of later phases.

**Modify:**
- `web/src/router.tsx` — Change all `/admin/*` paths to `/settings/*`
- `web/src/components/Layout.tsx` — Rename "Admin" nav link to "Settings" (href `/settings`), remove "Add Device" nav link (moves to DeviceList)
- `web/src/features/admin/AdminLayout.tsx` — Update `navSections` route paths from `/admin/*` to `/settings/*`
- `web/src/features/admin/AdminGuard.tsx` — Update redirect path if it references `/admin`
- `web/src/features/devices/DeviceList.tsx` — Add "Add Device" button (links to `/setup`) and "Add AMT Device" button above the device grid

---

### Phase 3: Dashboard

**Why:** Device management platforms all have a landing overview. Currently `/` redirects straight to `/devices` with no summary.

**Create:**
- `web/src/features/dashboard/Dashboard.tsx` — Stat cards row + recent activity
- `web/src/features/dashboard/StatCard.tsx` — Reusable: label/count card

**Layout:**
- Top row: 4 stat cards — Total Devices, Online Devices, Device Groups, Active Sessions
- Below (admin only): Recent Activity — last 10 audit events (reuses `GET /api/v1/audit`)
- Quick action links: "View All Devices", "Add Device"
- All data from existing endpoints, counts computed client-side

**Modify:**
- `web/src/router.tsx` — Index route → `<Dashboard />` instead of redirect to `/devices`
- `web/src/components/Layout.tsx` — "OpenGate" title links to `/`, add "Devices" nav link

---

### Phase 4: Device Search & Filter

**Why:** No way to find a device by name/OS in a large fleet. Only group filtering exists.

**Create:**
- `web/src/features/devices/DeviceSearchBar.tsx` — Search input with debounce (300ms), clear button, result count ("Showing X of Y")

**Modify:**
- `web/src/state/device-store.ts` — Add `searchQuery: string`, `setSearchQuery()`, derived filtering by `hostname`/`os` (case-insensitive contains)
- `web/src/features/devices/DeviceList.tsx` — Render `<DeviceSearchBar />` above the grid, use filtered devices

---

### Phase 5: AMT Power Actions on DeviceDetail

**Why:** 3 backend endpoints with no UI. Device detail already shows both regular and AMT sessions — power actions are the missing piece.

**Create:**
- `web/src/state/amt-store.ts` — `amtDevices`, `selectedAmtDevice`, `fetchAmtDevices()`, `fetchAmtDevice(uuid)`, `sendPowerAction(uuid, action)`. Uses `apiAction` pattern.

**Modify:**
- `web/src/features/devices/DeviceDetail.tsx` — For AMT-capable devices, add a "Power Actions" section with buttons: Power On, Soft Off, Power Cycle, Hard Reset. Confirmation dialog before destructive actions (power_cycle, hard_reset). Toast feedback on success/failure.
- `web/src/features/devices/DeviceList.tsx` — Fetch AMT devices alongside regular devices to show AMT status indicators on device cards

---

### Phase 6: Signing Key in Agent Updates

**Why:** `GET /api/v1/updates/signing-key` has no UI. Naturally belongs alongside manifests on the Agent Updates page.

**Modify:**
- `web/src/state/update-store.ts` — Add `signingKey: string | null`, `fetchSigningKey()` calling `GET /api/v1/updates/signing-key`
- `web/src/features/admin/AgentUpdates.tsx` — Add "Signing Key" section at bottom: monospace code block displaying the Ed25519 public key, copy-to-clipboard button

---

### Phase 7: User Profile Self-Edit

**Why:** `PATCH /api/v1/users/{id}` exists but requires admin. Users can't change their own display name.

**Backend change** (`server/internal/api/handlers_users.go`):
- Relax `UpdateUser`: allow when `request.Id == ContextUserID(ctx)` AND only `display_name` is set (no `is_admin` change). Keep admin-only guard for `is_admin` changes.

**Create:**
- `web/src/features/profile/ProfilePage.tsx` — Card with: Email (read-only), Display Name (editable), Created At (read-only), Save button. Toast on success.

**Modify:**
- `web/src/router.tsx` — Add `{ path: 'profile', element: <ProfilePage /> }` under AuthGuard
- `web/src/components/Layout.tsx` — Username in navbar becomes link to `/profile`
- `web/src/state/auth-store.ts` — Add `updateProfile(displayName)` calling `PATCH /api/v1/users/{id}` with current user ID, then re-fetch `fetchMe()`

---

### Phase 8: Device Group Reassignment (New API)

**Why:** Devices can't be moved between groups. No PATCH device endpoint exists.

**Backend:**
1. `api/openapi.yaml` — Add `PATCH /api/v1/devices/{id}` with body `{ group_id?: string | null }`
2. Regenerate Go server code (`oapi-codegen`)
3. `server/internal/db/store.go` + `sqlite.go` — `UpdateDeviceGroup(ctx, id, groupID)`
4. `server/internal/api/handlers_devices.go` — `UpdateDevice` handler
5. Regenerate `web/src/types/api.ts`

**Frontend modify:**
- `web/src/state/device-store.ts` — Add `updateDeviceGroup(deviceId, groupId)`
- `web/src/features/devices/DeviceDetail.tsx` — "Group" section with dropdown of available groups + "Move" button. Toast on success.

---

### Phase 9: Breadcrumb Navigation

**Why:** Deep pages (DeviceDetail, SessionView) lose navigation context. Manual back links are inconsistent.

**Create:**
- `web/src/components/Breadcrumbs.tsx` — Route-based breadcrumbs. Separator: `>`. Last segment = current page (not clickable). text-sm, gray-400 links / white current.

**Modify:**
- `web/src/components/Layout.tsx` — Add `<Breadcrumbs />` between `<nav>` and `<Outlet />`
- `web/src/features/devices/DeviceDetail.tsx` — Remove manual back link (replaced by breadcrumbs)

**Mapping:** `/` → Dashboard, `/devices` → Devices, `/devices/:id` → `[hostname]`, `/sessions/:tok` → Session, `/settings/*` → Settings > ...

---

## Dependency Graph

```
Phase 1 (Toast)        ← foundation for all phases
Phase 2 (Nav Restructure) ← sets up /settings routes, moves buttons
  ├── Phase 3 (Dashboard)       — frontend only
  ├── Phase 4 (Search)          — frontend only
  ├── Phase 5 (AMT Power)       — frontend only
  ├── Phase 6 (Signing Key)     — frontend only
  ├── Phase 7 (Profile)         — backend guard change + frontend
  ├── Phase 8 (Group Assign)    — new API endpoint + frontend
  └── Phase 9 (Breadcrumbs)     — frontend only
```

Phases 1 and 2 first. Then 3-6 and 9 are independent frontend work. Phases 7-8 need backend changes.

---

## Key Files

| File | Phases |
|------|--------|
| `web/src/router.tsx` | 2, 3, 7, 9 |
| `web/src/components/Layout.tsx` | 1, 2, 3, 7, 9 |
| `web/src/features/admin/AdminLayout.tsx` | 2 |
| `web/src/features/devices/DeviceList.tsx` | 2, 4, 5 |
| `web/src/features/devices/DeviceDetail.tsx` | 5, 8, 9 |
| `web/src/state/device-store.ts` | 4, 8 |
| `web/src/state/update-store.ts` | 6 |
| `web/src/features/admin/AgentUpdates.tsx` | 6 |
| `web/src/state/api-action.ts` | Pattern reference for all new stores |
| `server/internal/api/handlers_users.go` | 7 |
| `api/openapi.yaml` | 8 |

## Verification

After each phase:
1. `make build` — verify compilation
2. `make test` — all existing tests pass
3. `make lint` — no lint errors
4. New component tests pass (`cd web && npx vitest run`)
5. Manual verification: start dev server (`cd web && npm run dev`), navigate to new pages, confirm API calls work
6. E2E: `npx playwright test` against staging
