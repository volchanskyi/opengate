# Audit Plan — Frontend Security & Performance Sweep (web/)

**Skill:** `/frontend-audit` (run in diagnostic/plan mode — no in-place fixes applied).
**Branch:** `dev`. **Owner:** engineer (React/TS). **TDD:** security fixes ship a test first.
**Date:** 2026-06-27. **Status:** Ready for review.

## Scope & method

Ran the `/frontend-audit` checklist read-only over `web/src/`. XSS sinks are
already gated by `make taint-web` (`eslint-plugin-no-unsanitized` +
`eslint-plugin-security`) and DOM source→sink traces; those are recorded clean,
not re-reported.

## Confirmed clean (evidence)

- **XSS (§1/§2):** zero `dangerouslySetInnerHTML` / `.innerHTML` /
  `insertAdjacentHTML` / `document.write` in `web/src/`; backed by `make taint-web`.
- **Code splitting (§11b):** every route is `React.lazy` + `Suspense`
  ([`router.tsx:10-57`](../../web/src/router.tsx#L10)) — dashboard, devices,
  session (xterm/WebRTC), admin, profile all split. No eager-import finding.
- **TS strictness (§19):** `"strict": true` in
  [`tsconfig.app.json`](../../web/tsconfig.app.json) +
  [`tsconfig.node.json`](../../web/tsconfig.node.json); **zero `any`** in
  production code (grep count 0).
- **Re-render hygiene (§14a):** zero full-store Zustand reads (`useXStore()`
  without a selector) — all store access is selector-based.
- **Accessibility (§18):** zero clickable `<div>`/`<span>` (semantic `<button>`
  used); axe is exercised in E2E
  ([`e2e/a11y.spec.ts`](../../web/e2e/a11y.spec.ts) + `e2e/a11y-baseline.json`).
- **Headers/CSP (§5):** CSP + security headers set at the ingress
  ([`ingress.yaml`](../../deploy/helm/opengate/templates/ingress.yaml)) **and**
  app-level `SecurityHeaders` middleware on the server
  ([`api.go:238`](../../server/internal/api/api.go#L238)).
- **ErrorBoundary (§8b):** present
  ([`ErrorBoundary.tsx`](../../web/src/components/ErrorBoundary.tsx)) with a
  graceful fallback UI + reload.

## Accepted risk (documented, not a fix)

- **JWT in `localStorage` (§3a):** token stored in `localStorage`
  ([`auth-store.ts:33,46`](../../web/src/state/auth-store.ts#L33)), cleared on
  logout (line 53). This is the standard accepted XSS tradeoff for a Bearer-token
  SPA; the compensating control is the zero-XSS-sink posture above. No change —
  recorded so a future reviewer does not re-flag it.

## Findings

| # | Sev | Finding | Location | CI-caught? |
|---|-----|---------|----------|-----------|
| 1 | MEDIUM | No frontend error observability: `ErrorBoundary.componentDidCatch` only `console.error`s; no `window.onerror` / `unhandledrejection` handler; no `sendBeacon` to the server. Production crashes are invisible to operators (no Loki ingestion). | [`ErrorBoundary.tsx:18-20`](../../web/src/components/ErrorBoundary.tsx#L18) | No |
| 2 | LOW | No list virtualization (`@tanstack/react-virtual`/`react-window` absent). Currently bounded by server-side pagination; becomes a perf issue if page caps rise or infinite scroll is added (device list, audit log). | `web/src/features/devices/`, `web/src/features/admin/AuditLog.tsx` | No |

## Remediation plan

### Phase A — frontend error observability (MEDIUM; ~half day, TDD)

1. **F1 (server):** add a small `POST /api/v1/client-errors` endpoint (unauth or
   lightly rate-limited) that logs the payload via `slog` for Loki ingestion;
   bound the body size and rate-limit it. Test the handler (happy path + oversize
   rejection).
2. **F1 (web):** in `ErrorBoundary.componentDidCatch` (and a global
   `window.addEventListener('unhandledrejection', …)`), `navigator.sendBeacon`
   the error to `/api/v1/client-errors`, gated on `import.meta.env.PROD`. Payload
   rules: **no** token/email/PII; stack truncated to 500 chars; client-side rate
   limit (≤10/min). **Do not** add Sentry/LogRocket — self-hosted only. Test the
   boundary calls the reporter once per caught error and omits PII.

### Phase B — virtualization (LOW; ~half day, only if needed)

3. **F2:** if/when a list can exceed ~200 rendered rows, introduce
   `@tanstack/react-virtual` for the device list and audit log. Until then,
   document the pagination dependency in the component. *(Defer unless a perf
   regression is measured.)*

## File inventory

**Create:** `server/internal/api/handlers_client_errors.go` (+ test),
`web/src/lib/report-error.ts` (+ test).
**Modify:** `web/src/components/ErrorBoundary.tsx`,
`web/src/components/ErrorBoundary.test.tsx`, `web/src/main.tsx` (global
`unhandledrejection` listener), `api/openapi.yaml` (new endpoint) → regenerate.

## Acceptance criteria

1. A thrown render error and an unhandled rejection both produce a server-side
   log line in PROD; payload carries no token/email/PII; stack ≤ 500 chars.
2. `make taint-web`, `npm run lint`, `npm test`, and the gauntlet stay green.

## Reviewer checklist

- [ ] Error reporter is PROD-gated, rate-limited, PII-free, uses `sendBeacon`.
- [ ] New `/api/v1/client-errors` endpoint is size-bounded + rate-limited.
- [ ] No SaaS error tracker introduced.
- [ ] Virtualization deferred with an explicit pagination-dependency note.
