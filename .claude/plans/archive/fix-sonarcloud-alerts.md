# Security Groups & Permissions UI

## Context

The current admin system uses a simple `is_admin` boolean on the `users` table. There's no bootstrap mechanism for the first admin, and no UI for managing admin membership as a group. This plan introduces a **security groups** system starting with a built-in "Administrators" group, adds a **Security > Permissions** section to the admin UI, and auto-promotes the first registered user to admin.

## Database: Migration 005

**Create:** `server/internal/db/migrations/005_security_groups.up.sql` + `.down.sql`

```sql
-- security_groups: named permission groups
CREATE TABLE IF NOT EXISTS security_groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    is_system INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- security_group_members: many-to-many users <-> groups
CREATE TABLE IF NOT EXISTS security_group_members (
    group_id TEXT NOT NULL REFERENCES security_groups(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    added_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_sgm_user_id ON security_group_members(user_id);

-- Seed built-in Administrators group (well-known UUID)
INSERT OR IGNORE INTO security_groups (id, name, description, is_system)
VALUES ('00000000-0000-0000-0000-000000000001', 'Administrators', 'Full system access', 1);

-- Migrate existing is_admin=1 users into Administrators group
INSERT OR IGNORE INTO security_group_members (group_id, user_id, added_at)
SELECT '00000000-0000-0000-0000-000000000001', id, datetime('now')
FROM users WHERE is_admin = 1;
```

Key decisions:
- `users.is_admin` column **stays** — kept in sync via `syncIsAdmin()` for backward compat
- `is_system=1` prevents deletion of Administrators group via API
- Well-known UUID `00000000-0000-0000-0000-000000000001` avoids name lookups

## Go Backend

### Step 1: Models (`server/internal/db/models.go`)

Add types + constant:
```go
type SecurityGroupID = uuid.UUID
var AdminGroupID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

type SecurityGroup struct { ID, Name, Description, IsSystem, CreatedAt, UpdatedAt }
type SecurityGroupMember struct { GroupID, UserID, AddedAt }
```

### Step 2: Store Interface (`server/internal/db/store.go`)

Add under `// Security Groups` section:
- `CreateSecurityGroup(ctx, *SecurityGroup) error`
- `GetSecurityGroup(ctx, SecurityGroupID) (*SecurityGroup, error)`
- `ListSecurityGroups(ctx) ([]*SecurityGroup, error)`
- `DeleteSecurityGroup(ctx, SecurityGroupID) error`
- `AddSecurityGroupMember(ctx, groupID, userID) error`
- `RemoveSecurityGroupMember(ctx, groupID, userID) error`
- `ListSecurityGroupMembers(ctx, groupID) ([]*User, error)`
- `IsUserInSecurityGroup(ctx, userID, groupID) (bool, error)`
- `CountSecurityGroupMembers(ctx, groupID) (int, error)`

### Step 3: SQLite Implementation (`server/internal/db/sqlite.go`)

Implement all 9 methods. Critical details:
- `AddSecurityGroupMember` and `RemoveSecurityGroupMember` call `syncIsAdmin()` when `groupID == AdminGroupID` to keep `users.is_admin` in sync
- `DeleteSecurityGroup` returns error if `is_system=1`
- `RemoveSecurityGroupMember` checks `CountSecurityGroupMembers` to prevent removing the last admin

### Step 4: OpenAPI Spec (`api/openapi.yaml`)

Add schemas: `SecurityGroup`, `SecurityGroupMember`

Add 6 endpoints (all admin-only):
- `GET /api/v1/security-groups` — list all groups
- `POST /api/v1/security-groups` — create group (name, description)
- `GET /api/v1/security-groups/{id}` — get group with members
- `DELETE /api/v1/security-groups/{id}` — delete (403 for system groups)
- `POST /api/v1/security-groups/{id}/members` — add user `{user_id}`
- `DELETE /api/v1/security-groups/{id}/members/{userId}` — remove user

Regenerate: `go generate ./...`

### Step 5: API Handlers (`server/internal/api/handlers_security_groups.go`)

New file with 6 handlers. Each:
- Checks `isAdmin(ctx)` → 403
- Performs operation
- Calls `s.auditLog()` with actions: `security_group.create`, `.delete`, `.add_member`, `.remove_member`

Special rules:
- Delete: 403 if `is_system`
- Remove member: 409 if last member of Administrators group

### Step 6: Auth Handlers (`server/internal/api/handlers_auth.go`)

**Login** (line 69): Derive `isAdmin` from group membership instead of `user.IsAdmin`:
```go
isAdmin, _ := s.store.IsUserInSecurityGroup(ctx, user.ID, db.AdminGroupID)
token, err := s.jwt.GenerateToken(user.ID, user.Email, isAdmin)
```

**Register** (line 40): Same derivation + auto-first-user bootstrap:
```go
users, _ := s.store.ListUsers(ctx)
if len(users) == 1 {
    s.store.AddSecurityGroupMember(ctx, db.AdminGroupID, user.ID)
}
isAdmin, _ := s.store.IsUserInSecurityGroup(ctx, user.ID, db.AdminGroupID)
token, err := s.jwt.GenerateToken(user.ID, user.Email, isAdmin)
```

### Step 7: User Handlers (`server/internal/api/handlers_users.go`)

When `UpdateUser` toggles `is_admin`, also add/remove from Administrators group to keep both systems in sync.

## React Frontend

### Step 1: Admin Sidebar (`web/src/features/admin/AdminLayout.tsx`)

Restructure sidebar with section headers:

```
Management
  Users
  Audit Log
  Agent Updates
Security
  Permissions
```

Section headers: small uppercase gray text (`text-xs uppercase text-gray-500`).

### Step 2: Permissions Page (`web/src/features/admin/Permissions.tsx`)

New component showing security groups (initially just Administrators):
- Group card with name, description, "System" badge for built-in groups
- Members table: email, display name, "Remove" button
- "Add Member" section: dropdown/autocomplete of registered users (filtered to non-members)
- Cannot remove last admin (button disabled or server returns 409)

### Step 3: Zustand Store (`web/src/state/security-groups-store.ts`)

```typescript
interface SecurityGroupsState {
  groups: SecurityGroup[];
  selectedGroup: SecurityGroupWithMembers | null;
  users: User[];  // all users for the add-member picker
  isLoading: boolean;
  error: string | null;
  fetchGroups: () => Promise<void>;
  fetchGroupDetail: (id: string) => Promise<void>;
  addMember: (groupId: string, userId: string) => Promise<void>;
  removeMember: (groupId: string, userId: string) => Promise<void>;
}
```

### Step 4: Router (`web/src/router.tsx`)

Add route under admin children:
```tsx
{ path: 'security/permissions', element: <Permissions /> }
```

### Step 5: OpenAPI Types

Regenerate after spec update: `npm run generate:api` (or equivalent)

## Test Plan (TDD — tests first)

### Layer 1: Go Unit Tests (DB)

| Test | File | What |
|------|------|------|
| `TestSecurityGroup_CRUD` | `sqlite_test.go` | Create, get, list, delete |
| `TestSecurityGroup_Members` | `sqlite_test.go` | Add, list, remove, is-member |
| `TestSecurityGroup_SystemCannotDelete` | `sqlite_test.go` | Error on system group delete |
| `TestSecurityGroup_CascadeOnUserDelete` | `sqlite_test.go` | Membership removed when user deleted |
| `TestSecurityGroup_LastAdminProtection` | `sqlite_test.go` | Cannot remove last admin |
| `TestSecurityGroup_SyncIsAdmin` | `sqlite_test.go` | users.is_admin stays in sync with group membership |
| Handler tests (6 endpoints) | `handlers_security_groups_test.go` | 200/403/404/409 cases, table-driven |
| `TestRegister_FirstUserIsAdmin` | `handlers_auth_test.go` | First user auto-joins Administrators |
| `TestLogin_AdminFromGroup` | `handlers_auth_test.go` | JWT IsAdmin derived from group membership |

### Layer 2: Go Integration Tests (extend existing)

**File:** `server/tests/integration/security_groups_test.go` (new)

| Test | What |
|------|------|
| `TestSecurityGroup_AdminCanListGroups` | Admin lists security groups via `GET /api/v1/security-groups`, sees Administrators |
| `TestSecurityGroup_AdminCanAddMember` | Admin adds regular user to Administrators via `POST .../members`, user can then access admin endpoints with new token |
| `TestSecurityGroup_AdminCanRemoveMember` | Admin removes user from Administrators, user loses admin access on re-login |
| `TestSecurityGroup_NonAdminBlocked` | Regular user gets 403 on all security group endpoints |
| `TestSecurityGroup_CannotDeleteSystemGroup` | Admin gets 403 when trying `DELETE /api/v1/security-groups/{AdminGroupID}` |
| `TestSecurityGroup_CannotRemoveLastAdmin` | Admin gets 409 removing themselves when they're the only member |
| `TestSecurityGroup_AuditLogging` | Add/remove member actions appear in audit log |

**File:** `server/tests/integration/admin_test.go` (extend)

| Test | What |
|------|------|
| `TestAdminFirstUserBootstrap` | First registered user (via `POST /api/v1/auth/register`) gets admin JWT, can access `/api/v1/users` |
| `TestAdminSecondUserNotAdmin` | Second registered user does NOT get admin JWT |

**Modify:** `server/internal/testutil/testutil.go`
- Update `SeedAdminUser()` to also call `s.AddSecurityGroupMember(ctx, db.AdminGroupID, u.ID)` so existing integration tests continue working

### Layer 3: Web Component Tests (Vitest + React Testing Library)

| Test | File |
|------|------|
| Renders group list with Administrators | `Permissions.test.tsx` |
| Shows members table with email and display name | `Permissions.test.tsx` |
| Add member dropdown filters out existing members | `Permissions.test.tsx` |
| Remove member button triggers API call | `Permissions.test.tsx` |
| Remove button disabled when last admin | `Permissions.test.tsx` |
| System badge shown for system groups | `Permissions.test.tsx` |
| Store fetch/add/remove actions | `security-groups-store.test.ts` |

### Layer 4: Playwright E2E Tests (extend existing)

The first-user-auto-admin bootstrap unlocks real admin E2E testing. Previously `admin.spec.ts` couldn't test admin flows because there was no API-based way to create an admin.

**Key insight:** In Docker Compose test env, the DB starts empty (tmpfs). The first `createTestUser()` call registers the first user, who auto-becomes admin. This enables full admin E2E without server-side seeding hacks.

**File:** `web/e2e/helpers/api-helper.ts` (extend)

Add new helpers:
```typescript
export async function listSecurityGroups(request, token): Promise<SecurityGroup[]>
export async function getSecurityGroup(request, token, id): Promise<SecurityGroupWithMembers>
export async function addGroupMember(request, token, groupId, userId): Promise<void>
export async function removeGroupMember(request, token, groupId, userId): Promise<void>
export async function getMe(request, token): Promise<User>
```

**File:** `web/e2e/fixtures.ts` (extend)

Add `adminUser` fixture — registers the first user in a fresh DB (auto-admin):
```typescript
adminUser: async ({ request }, use) => {
  // First registered user becomes admin via bootstrap
  const email = uniqueEmail();
  const password = 'TestPass123!';
  const token = await register(request, email, password);
  await use({ email, password, token });
}
```

**Note:** Test isolation — each Playwright E2E test suite runs against a fresh Docker Compose environment (tmpfs DB), so "first user" is deterministic per test run. Tests within a single suite share the same server instance, so `adminUser` fixture should be used carefully (only one test can be "first").

**File:** `web/e2e/security-permissions.spec.ts` (new — 6 tests)

| Test | What |
|------|------|
| `admin sees Security > Permissions in sidebar` | Navigate to `/admin`, verify "Security" section header and "Permissions" link visible |
| `Permissions page shows Administrators group` | Navigate to `/admin/security/permissions`, verify "Administrators" group card with "System" badge |
| `admin sees themselves in Administrators group` | Verify current user's email appears in members table |
| `admin can add a user to Administrators` | Register second user, add via UI dropdown, verify appears in members list |
| `admin can remove a user from Administrators` | Add second user, click Remove, verify removed from list |
| `cannot remove last admin` | Try to remove self (only admin), verify error message or disabled button |

**File:** `web/e2e/admin.spec.ts` (upgrade existing 3 tests)

The existing tests are shallow due to the old limitation. With first-user-auto-admin bootstrap, upgrade them:

| Test (existing → upgraded) | What |
|------|------|
| `non-admin is blocked from /admin` | Keep as-is — register second user (non-admin), verify redirect |
| `admin can see user list` | Upgrade: use `adminUser` fixture, actually navigate to `/admin/users`, verify user table renders with at least one user |
| `audit log page loads` | Upgrade: use `adminUser` fixture, navigate to `/admin/audit`, verify table or "no events" renders |

### Layer 5: Web Integration Tests (Vitest, mocked API)

**File:** `web/tests/integration/permissions-flow.test.tsx` (new)

| Test | What |
|------|------|
| Permissions page renders groups from mocked API | Verify group cards render |
| Add member flow shows success | Mock add member API, verify UI updates |
| Remove member shows confirmation | Verify confirmation dialog or immediate removal |
| Non-admin redirect | Verify AdminGuard blocks access |

## Verification
1. `make test` — all new + existing tests pass (unit + integration)
2. `make lint` — no new warnings
3. `npx playwright test` (in web/) — E2E suite passes against Docker Compose env
4. Manual smoke: register first user → auto-admin → navigate to Security > Permissions → see self in Administrators → register second user → add to Administrators via UI → second user sees admin section → remove second user → they lose admin
5. Verify: try removing last admin → blocked with error

## Files to Create
- `server/internal/db/migrations/005_security_groups.up.sql`
- `server/internal/db/migrations/005_security_groups.down.sql`
- `server/internal/api/handlers_security_groups.go`
- `server/internal/api/handlers_security_groups_test.go`
- `server/tests/integration/security_groups_test.go`
- `web/src/features/admin/Permissions.tsx`
- `web/src/features/admin/Permissions.test.tsx`
- `web/src/state/security-groups-store.ts`
- `web/src/state/security-groups-store.test.ts`
- `web/e2e/security-permissions.spec.ts`
- `web/tests/integration/permissions-flow.test.tsx`

## Files to Modify
- `server/internal/db/models.go` — SecurityGroup types, AdminGroupID constant
- `server/internal/db/store.go` — 9 new interface methods
- `server/internal/db/sqlite.go` — implement methods + syncIsAdmin
- `server/internal/db/sqlite_test.go` — security group DB tests
- `server/internal/api/handlers_auth.go` — first-user bootstrap, derive IsAdmin from group
- `server/internal/api/handlers_users.go` — sync group membership on is_admin toggle
- `server/internal/testutil/testutil.go` — update SeedAdminUser to add to Administrators group
- `api/openapi.yaml` — SecurityGroup schemas + 6 endpoints
- `web/src/features/admin/AdminLayout.tsx` — sectioned sidebar
- `web/src/router.tsx` — add security/permissions route
- `web/e2e/admin.spec.ts` — upgrade shallow tests to use admin fixture
- `web/e2e/fixtures.ts` — add adminUser fixture
- `web/e2e/helpers/api-helper.ts` — add security group API helpers
