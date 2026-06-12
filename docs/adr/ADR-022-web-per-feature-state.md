# ADR-022: Web per-feature state ownership

Date: 2026-05-19
Status: Accepted

## Context

[ADR-020](ADR-020-modular-monolith-full-hexagonal.md) adopted full hexagonal architecture across OpenGate. For the React web frontend the equivalent question is state-store ownership.

Current state (verified against [`web/`](../../web/)):

- **12 Zustand stores** live in [`web/src/state/`](../../web/src/state/) — one global namespace.
- **11 feature folders** under [`web/src/features/`](../../web/src/features/) — none has a `state/` subdirectory or an `index.ts` barrel.
- **81 cross-feature store imports** across the codebase. Most-imported: `useAuthStore` (20), `useConnectionStore` (14), `useDeviceStore` (10), `useToastStore` (9), `useUpdateStore` (8).
- **Only `useAuthStore` is hydration-coupled** to app bootstrap ([`web/src/App.tsx:4,9-10`](../../web/src/App.tsx#L4-L10) — the `hydrated` flag blocks render). Earlier plan drafts assumed `useConnectionStore` and `useToastStore` were also bootstrap-coupled; verification proved they are not.
- **No `eslint-plugin-boundaries` or `dependency-cruiser` installed** — boundary tooling is greenfield.

Representative coupling: [`web/src/features/devices/DeviceDetail.tsx:3-7`](../../web/src/features/devices/DeviceDetail.tsx#L3-L7) imports 5 stores spanning 4 features.

The plan ([`.claude/plans/modular-monolith-evaluation.md`](../../.claude/plans/archive/modular-monolith-evaluation.md) §4.3, R1 Q4) resolved scope; this ADR codifies it.

## Decision

### Move 11 of 12 stores into their feature folder

Each store lives at `web/src/features/<x>/state/`. Each feature exports its public surface via a `web/src/features/<x>/index.ts` barrel. Stores, hooks, types, and components are public ONLY if re-exported from the barrel. Deep imports into `features/<x>/state/*` from outside `features/<x>/` are denied by the boundaries lint rule.

| Store | New location |
|---|---|
| `useAuthStore` | **stays at `web/src/state/auth-store.ts`** (only hydration-gated store) |
| `useDeviceStore` | `features/devices/state/device-store.ts` |
| `useSessionStore` | `features/session/state/session-store.ts` |
| `useConnectionStore` | `features/session/state/connection-store.ts` (connection lifecycle belongs to session) |
| `useAMTStore` | `features/devices/state/amt-store.ts` (AMT is a device-detail concern) |
| `useUpdateStore` | `features/devices/state/update-store.ts` |
| `useChatStore` | `features/messenger/state/chat-store.ts` |
| `useFileStore` | `features/file-manager/state/file-store.ts` |
| `usePushStore` | `features/profile/state/push-store.ts` (push subscription is a user-profile concern) |
| `useSecurityGroupsStore` | `features/admin/state/security-groups-store.ts` |
| `useAdminStore` | `features/admin/state/admin-store.ts` |
| `useToastStore` | `web/src/lib/feedback/toast-store.ts` (cross-cutting utility, not a feature) |

`useToastStore` is treated as a `lib` element-group member, not a feature member. Any feature may import from `lib`; `lib` may not import from any feature.

### Cross-feature access pattern

A feature's `index.ts` re-exports the surface it intends to share. Example for `features/devices/index.ts`:

```ts
export { useDeviceStore } from './state/device-store';
export type { Device, DeviceStatus } from './types';
```

Consumers in other features import via the barrel:

```ts
import { useDeviceStore } from '@/features/devices';
```

NOT:

```ts
import { useDeviceStore } from '@/features/devices/state/device-store'; // denied
```

The boundaries lint rule enforces this. Each feature's `state/` is considered private.

### Tooling

- **`eslint-plugin-boundaries`** — three element groups: `app` (`web/src/main.tsx`, `App.tsx`), `feature` (`features/<x>/`), `lib` (`lib/<x>/`). Rules:
  - `app` may import from any group.
  - `feature` may import from `lib` and from sibling features only via the sibling's `index.ts`.
  - `lib` may not import from `feature`.
- **`dependency-cruiser`** — snapshot of cross-feature edge count, stored at `web/dependency-cruiser.snapshot.json`. CI fails if the snapshot grows without a barrel-export justification.

Both run inside the existing `web/eslint.config.js` chain in [`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh) and in CI.

### Bootstrap-coupled exception: `useAuthStore` only

Earlier drafts assumed `auth`, `connection`, `toast` were all hydration-coupled. Verification proved only `auth` is — [`web/src/App.tsx`](../../web/src/App.tsx) gates render on `useAuthStore.hydrated`. `useConnectionStore` calls `useToastStore.getState().addToast(...)` directly but neither blocks render. `useAuthStore` stays at `web/src/state/auth-store.ts` and is treated as an `app`-group member (importable from anywhere).

### Migration order

Per the opportunistic-trigger model (plan §9): each feature's stores move when the next PR touches >2 stores in that feature. The boundary lint starts in warn mode so the 81 current cross-feature imports do not block CI on day one; auto-flip to error when violations reach zero (per [ADR-020](ADR-020-modular-monolith-full-hexagonal.md) §"Warn → error flip criterion").

## Out of scope

- **No state-framework change.** Zustand 5.0.12 stays. No Redux, no Jotai, no Context-based replacement.
- **No micro-frontend split.** Per-feature ownership enables it; this ADR does not commit to it.
- **No BFF layer.** Per-feature backend-for-frontend is a future microservice-extraction concern (plan §14).
- **No path-alias rewrite beyond what `eslint-plugin-boundaries` requires.** Existing imports keep their style until touched.
- **No file-level inline ESLint suppression directives.** Exceptions live in the `eslint-plugin-boundaries` config with an ADR reference.

## Consequences

**Positive.**

- Each feature owns its data shape. Code review can verify a feature does not reach into another's internals.
- The 81 current cross-feature imports become measurable and shrinkable — `dependency-cruiser`'s snapshot is the trend baseline.
- Future micro-frontend extraction (if needed) starts with clean boundaries.

**Accepted trade-offs.**

- Each feature gains a `state/` subdirectory and an `index.ts` barrel — small but new structural overhead.
- The 81 cross-feature imports must be migrated through their respective barrels opportunistically. Months-long tail. Warn-mode start absorbs this; auto-flip to error when clean.
- `useAuthStore` keeps its global location for bootstrap reasons — one documented exception to the per-feature rule.

## References

- Plan: [`.claude/plans/modular-monolith-evaluation.md`](../../.claude/plans/archive/modular-monolith-evaluation.md) §2.3 (verified state), §4.3 (scope), §5.3 (tooling)
- Upstream: [ADR-020](ADR-020-modular-monolith-full-hexagonal.md) — modular-monolith scope and style
- Tooling: [`eslint-plugin-boundaries`](https://github.com/javierbrea/eslint-plugin-boundaries), [`dependency-cruiser`](https://github.com/sverweij/dependency-cruiser)
- Constraints: [`.claude/rules/sonarcloud.md`](../../.claude/rules/sonarcloud.md) (no-suppression rule extends here), [`.claude/hooks/pretooluse-write-guard.sh`](../../.claude/hooks/pretooluse-write-guard.sh) (already blocks inline ESLint suppression directives)
