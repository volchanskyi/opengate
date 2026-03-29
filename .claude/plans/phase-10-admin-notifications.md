# Phase 10: Admin Dashboard, Web Push Notifications & Service Worker

## Context

Phases 0–9 built the full session lifecycle: protocol, transport, relay, WebRTC, agent session handler, and web UI. The project now needs its admin layer — user/group management, audit visibility, push notifications for device events, and a service worker for offline caching + native push. The master plan calls this "Phase 9: Admin, Notifications, Service Worker"; memory tracks it as "MPS, notifications". MPS stays as a stub this phase.

**Previous commit**: `edb70cd feat: Phase 9 — agent session handler + WebRTC signaling`

---

## Part A: Notification Service (`server/internal/notifications/`)

### New files
- `server/internal/notifications/notifier.go` — `Notifier` interface + types
- `server/internal/notifications/vapid.go` — VAPID key generation/loading from `{dataDir}/vapid.json`
- `server/internal/notifications/push.go` — `PushNotifier` implementation (sends via Web Push RFC 8030 + VAPID)
- `server/internal/notifications/noop.go` — `NoopNotifier` for tests/disabled mode

### Tests first (TDD)
- `server/internal/notifications/notifier_test.go` — event→payload mapping, stale subscription cleanup (410 → delete), non-410 errors logged not fatal, NoopNotifier returns nil
- `server/internal/notifications/vapid_test.go` — generates keys on first call, loads on second, corrupt file errors

### Types
```go
type EventType string
const (
    EventDeviceOnline  EventType = "device_online"
    EventDeviceOffline EventType = "device_offline"
    EventSessionStarted EventType = "session_started"
    EventSessionEnded   EventType = "session_ended"
)

type Event struct {
    Type           EventType
    DeviceID       uuid.UUID
    DeviceHostname string
    UserID         uuid.UUID
    Timestamp      time.Time
}

type Notifier interface {
    Notify(ctx context.Context, event Event) error
    VAPIDPublicKey() string
}
```

### Store interface addition
Add to `server/internal/db/store.go`:
```go
ListAllWebPushSubscriptions(ctx context.Context) ([]*WebPushSubscription, error)
```
Implement in `sqlite.go`. No migration needed — just a new SELECT.

### Dependency
- `github.com/SherClockHolmes/webpush-go` (pure-Go VAPID/Web Push)

---

## Part B: OpenAPI + Handlers

### New endpoints in `api/openapi.yaml`

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/v1/push/subscribe` | JWT | Save browser push subscription |
| DELETE | `/api/v1/push/subscribe` | JWT | Remove push subscription |
| GET | `/api/v1/push/vapid-key` | JWT | Get VAPID public key |
| GET | `/api/v1/audit` | JWT+admin | Query audit log (filters: user_id, action, limit, offset) |
| PATCH | `/api/v1/users/{id}` | JWT+admin | Update user (is_admin, display_name) |

### New schemas in `api/openapi.yaml`
- `WebPushSubscribeRequest` (endpoint, p256dh, auth)
- `WebPushUnsubscribeRequest` (endpoint)
- `VapidKeyResponse` (public_key)
- `AuditEvent` (id, user_id, action, target, details, created_at)
- `UpdateUserRequest` (is_admin?, display_name?)

### Handler files
- **New**: `server/internal/api/handlers_push.go` — SubscribePush, UnsubscribePush, GetVapidKey
- **New**: `server/internal/api/handlers_audit.go` — ListAuditEvents (admin-only check)
- **Modify**: `server/internal/api/handlers_users.go` — add UpdateUser handler
- **Modify**: `server/internal/api/converters.go` — add auditEventToAPI converter

### Test files (TDD)
- **New**: `server/internal/api/push_handlers_test.go` — subscribe/unsubscribe/vapid-key, 401 checks
- **New**: `server/internal/api/audit_handlers_test.go` — admin-only, filters, pagination, empty array not null
- **Extend**: `server/internal/api/user_handlers_test.go` — UpdateUser tests (toggle admin, update name, 403/404)
- **Extend**: `server/internal/api/helpers_test.go` — update `newTestServer` to accept NoopNotifier

### Server struct changes
- **Modify**: `server/internal/api/api.go` — add `notifier Notifier` field to Server, update NewServer signature:
  ```go
  func NewServer(store, jwtCfg, agents, relay, sigTracker, notifier, logger)
  ```
- Run `go generate ./internal/api/` after OpenAPI changes to regenerate `openapi_gen.go`

---

## Part C: Notification Hook Points

### Agent lifecycle hooks
- **Modify**: `server/internal/agentapi/server.go`
  - Add `notifier notifications.Notifier` field
  - Update `NewAgentServer` signature: add notifier param
  - In agent disconnect defer: `notifier.Notify(EventDeviceOffline)`
  - After successful registration: `notifier.Notify(EventDeviceOnline)`

### Session lifecycle hooks
- **Modify**: `server/internal/api/handlers_sessions.go`
  - After CreateSession: `notifier.Notify(EventSessionStarted)`
  - After DeleteSession: `notifier.Notify(EventSessionEnded)`

### Audit event writes
Add fire-and-forget audit writes (goroutine + 5s timeout context) to:
- `handlers_auth.go` — login, register events
- `handlers_users.go` — user.delete, user.update events
- `handlers_sessions.go` — session.create, session.delete events

### Wiring in main.go
- **Modify**: `server/cmd/meshserver/main.go`
  ```go
  vapidPriv, vapidPub, err := notifications.LoadOrGenerateVAPID(*dataDir)
  notifier := notifications.NewPushNotifier(store, vapidPriv, vapidPub, contactEmail, logger)
  agentSrv := agentapi.NewAgentServer(certMgr, store, agentRelay, notifier, logger)
  srv := api.NewServer(store, jwtCfg, agentSrv, agentRelay, sigTracker, notifier, logger)
  ```
- Add `--vapid-contact` flag (default: `""`, optional)

---

## Part D: Admin Dashboard (Web Client)

### New Zustand stores
- `web/src/state/admin-store.ts` — users[], auditEvents[], fetchUsers, updateUser, deleteUser, fetchAuditEvents
- `web/src/state/push-store.ts` — permission, isSubscribed, vapidKey, subscribe, unsubscribe

### Test files (TDD)
- `web/src/state/admin-store.test.ts`
- `web/src/state/push-store.test.ts`

### New feature: `web/src/features/admin/`
| File | Purpose |
|------|---------|
| `AdminGuard.tsx` | Route guard — checks `user.is_admin`, redirects non-admins |
| `AdminLayout.tsx` | Sidebar nav (Users, Groups, Audit, Settings) |
| `UserManagement.tsx` | User table: list, toggle admin, delete |
| `AuditLog.tsx` | Filterable/paginated audit event table |
| `ServerSettings.tsx` | VAPID key (copyable), agent count, server info |
| `NotificationCenter.tsx` | Bell icon in navbar (all users), permission toggle, push status |

### Test files (TDD — Vitest + RTL)
- `web/src/features/admin/AdminGuard.test.tsx`
- `web/src/features/admin/UserManagement.test.tsx`
- `web/src/features/admin/AuditLog.test.tsx`
- `web/src/features/admin/NotificationCenter.test.tsx`

### Router changes
- **Modify**: `web/src/App.tsx` or `web/src/router.tsx` — add `/admin` route with AdminGuard + child routes (users, audit, settings)

### Layout changes
- **Modify**: existing Layout component — add admin nav link (conditional on is_admin), add NotificationCenter bell icon

---

## Part E: Service Worker

### New files
- `web/public/sw.js` (plain JS, not TS — standard for Vite projects in `public/`)
  - Install: precache app shell
  - Activate: clean old caches
  - Fetch: network-first for `/api/` and `/ws/`, cache-first for static assets
  - Push: parse JSON payload → `self.registration.showNotification()`
  - Notification click: focus existing window or open new one

### Registration
- **Modify**: `web/src/main.tsx` — add SW registration on load:
  ```typescript
  if ('serviceWorker' in navigator) {
    window.addEventListener('load', () => {
      navigator.serviceWorker.register('/sw.js');
    });
  }
  ```

---

## Implementation Order

1. **Part A** — notifications package (isolated, no deps on other changes)
2. **Part B** — OpenAPI + handlers (depends on A for Notifier interface)
3. **Part C** — hook wiring + main.go (depends on A + B)
4. **Part D** — web admin UI (can start after B's OpenAPI changes)
5. **Part E** — service worker (independent, can parallel with D)

---

## Critical Files to Modify

| File | Change |
|------|--------|
| `server/internal/notifications/notifications.go` | Replace stub → full package |
| `server/internal/db/store.go` | Add ListAllWebPushSubscriptions |
| `server/internal/db/sqlite.go` | Implement ListAllWebPushSubscriptions |
| `api/openapi.yaml` | 5 new endpoints + 5 new schemas |
| `server/internal/api/api.go` | Server struct + NewServer signature |
| `server/internal/api/openapi_gen.go` | Regenerated from openapi.yaml |
| `server/internal/agentapi/server.go` | Add notifier field, update constructor |
| `server/cmd/meshserver/main.go` | VAPID init, notifier wiring |
| `web/src/App.tsx` or router | Admin routes |

## Reusable Existing Code

- `server/internal/db/store.go` — WebPush CRUD methods already defined
- `server/internal/db/models.go` — WebPushSubscription, AuditEvent models exist
- `server/internal/api/middleware.go` — AuthMiddleware for JWT validation
- `server/internal/api/helpers_test.go` — newTestServer pattern, doGet/doPost helpers
- `web/src/features/auth/AuthGuard.tsx` — pattern for AdminGuard
- `web/src/state/auth-store.ts` — pattern for admin-store and push-store

## Verification

```bash
# 1. Go tests
cd server && go test -race ./internal/notifications/...
cd server && go test -race ./internal/api/...
cd server && go test -race ./internal/db/...

# 2. Web tests
cd web && npx vitest run

# 3. Full suite
make lint
make test
make build

# 4. Manual verification
# - Register push subscription via browser, trigger device connect, verify notification
# - Login as admin → /admin/users → toggle admin → check audit log
# - Disconnect network → verify cached UI loads (service worker)
```
