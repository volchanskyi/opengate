---
name: frontend-audit
description: |
  Audit frontend application code for XSS vulnerabilities, insecure data storage,
  missing security headers, unsafe rendering, dependency vulnerabilities, performance
  bottlenecks, bundle inefficiencies, and accessibility gaps. Fixes issues in-place
  and reports findings.
---

# Frontend Security & Performance Audit

Systematically audit every component, store, transport layer, build configuration, and rendering path in the frontend application. Fix every issue you find in-place. Write tests for every security fix. Report a summary at the end.

**Severity levels:** CRITICAL (exploitable vulnerability), HIGH (missing security control or major performance regression), MEDIUM (defense-in-depth gap or moderate perf issue), LOW (best-practice improvement).

**Reference:** OWASP Top 10 (2021), OWASP Client-Side Security Checklist, Web Vitals (LCP/INP/CLS), React Performance Best Practices.

**Tech stack:** React 19, TypeScript (strict), Vite 7, Zustand, Tailwind CSS 4, xterm.js, openapi-fetch, MessagePack, WebSocket/WebRTC.

---

## PART 1 — SECURITY

---

### 1. Cross-Site Scripting (XSS) Prevention (OWASP A03)

#### 1a. Unsafe HTML rendering

Scan ALL `.tsx` and `.ts` files under `web/src/` for dangerous patterns:

```bash
grep -rn 'dangerouslySetInnerHTML\|\.innerHTML\|\.outerHTML\|insertAdjacentHTML\|document\.write' web/src/ --include='*.ts' --include='*.tsx'
```

ANY occurrence is **CRITICAL** unless the content is provably static or sanitized with DOMPurify. If dynamic user content is rendered unsafely, either:
- Replace with React JSX (preferred — React escapes by default)
- Add `dompurify` sanitization: `DOMPurify.sanitize(html)` before rendering

#### 1b. URL-based XSS vectors

Scan for `href`, `src`, and `window.location` assignments that accept user input:

```bash
grep -rn 'href={.*\|src={.*\|window\.location\s*=\|location\.href\s*=' web/src/ --include='*.tsx' --include='*.ts' | grep -v 'node_modules'
```

Verify:
- No `href={userInput}` that could contain `javascript:` URIs
- No `src={userInput}` that could load malicious scripts
- All dynamic URLs are validated against an allowlist of schemes (`https:`, `wss:`)
- `window.open()` targets are validated — never pass user-controlled URLs without scheme validation

#### 1c. DOM-based XSS

Search for direct DOM manipulation that bypasses React's virtual DOM:

```bash
grep -rn 'document\.getElementById\|document\.querySelector\|document\.createElement\|\.textContent\s*=\|\.innerText\s*=' web/src/ --include='*.ts' --include='*.tsx' | grep -v '_test\.\|\.test\.'
```

Direct DOM writes with user-controlled content are **HIGH**. Acceptable: library integrations (xterm.js, canvas) that handle their own escaping.

#### 1d. Template literal injection

Search for template literals that build HTML strings:

```bash
grep -rn '`.*<.*>.*\$\{' web/src/ --include='*.ts' --include='*.tsx' | grep -v '_test\.\|\.test\.'
```

Any template literal that builds HTML from user input is **HIGH** — replace with React JSX.

#### 1e. React-specific escaping verification

Verify that React's default escaping is not bypassed:
- No `dangerouslySetInnerHTML` (check 1a)
- No rendering of `React.createElement` with user-controlled `type` argument
- No dynamic component rendering from user input (e.g., `const Component = userMap[input]; <Component />`)

---

### 2. Secure Data Storage & Sessions

#### 2a. Token storage mechanism

Locate all token storage operations:

```bash
grep -rn 'localStorage\|sessionStorage\|document\.cookie\|indexedDB' web/src/ --include='*.ts' --include='*.tsx' | grep -v 'node_modules\|_test\.\|\.test\.'
```

Audit each occurrence:
- JWT tokens in `localStorage` are accessible via XSS — document this accepted risk
- Verify NO sensitive data beyond the auth token is stored client-side (no passwords, PII, session details)
- Verify NO API keys, secrets, or credentials are stored in `localStorage` or `sessionStorage`
- If `indexedDB` is used, verify no sensitive data is stored unencrypted

#### 2b. Token lifecycle

Verify the auth token lifecycle in `web/src/state/auth-store.ts`:
- Token is set ONLY on successful login/register response
- Token is cleared on logout (`localStorage.removeItem`)
- Token is cleared on 401 responses (session expiry)
- Token is NOT logged anywhere (console.log, slog, error messages)
- Token is NOT included in error reports or analytics payloads

#### 2c. Sensitive data in component state

Scan all Zustand stores (`web/src/state/*.ts`) for sensitive data retention:
- Password fields are NOT stored in state after form submission
- Sensitive API responses are cleared when no longer needed
- No store persists sensitive data to `localStorage` via Zustand middleware

#### 2d. Memory cleanup

Verify sensitive data is cleaned up:
- Password input values cleared after auth requests
- WebSocket binary frames not retained in memory after processing
- File download blobs revoked after use (`URL.revokeObjectURL`)
- Form data containing credentials is not cached by the browser (check `autocomplete` attributes)

---

### 3. Secure Communication & Transport

#### 3a. HTTPS enforcement

Verify all API and WebSocket connections use secure protocols:

```bash
grep -rn 'http://\|ws://' web/src/ --include='*.ts' --include='*.tsx' | grep -v 'localhost\|127\.0\.0\.1\|node_modules\|_test\.\|\.test\.'
```

Any hardcoded `http://` or `ws://` URL targeting production is **CRITICAL**. Verify:
- API base URL uses relative paths (no protocol-specific URLs)
- WebSocket connections construct `wss://` from the current page protocol
- WebRTC ICE server URLs use `turns:` for TURN over TLS when applicable

#### 3b. WebSocket authentication

Audit `web/src/lib/transport/ws-transport.ts`:
- Authentication token is transmitted securely during WebSocket upgrade
- If token is passed as a query parameter (`?auth=`), verify the server does NOT log query strings (query params appear in access logs — **HIGH** risk)
- Prefer: upgrade to protocol-level auth (subprotocol header or first-message auth) to avoid token in URL
- Token is NOT included in WebSocket frame payloads after initial handshake

#### 3c. WebRTC security

Audit `web/src/lib/transport/` for WebRTC usage:
- ICE candidates are exchanged only via authenticated signaling channel
- DTLS is enabled for all data channels (browser default, verify not disabled)
- No STUN/TURN credentials hardcoded in source (should come from server)
- Data channel configuration does not disable encryption

#### 3d. Binary protocol security

Audit `web/src/lib/protocol/codec.ts` and `web/src/lib/protocol/types.ts`:
- Frame size validation: reject frames exceeding `MAX_FRAME_SIZE` before allocating buffers
- Frame type validation: reject unknown frame type bytes
- MessagePack decode: catch decode errors gracefully (no unhandled exceptions)
- No deserialization into `eval()` or `Function()` — decoded data used as typed objects only
- Buffer overread protection: verify payload length matches actual data available

---

### 4. Content Security Policy (CSP) & Security Headers

#### 4a. CSP meta tag or header

Check if CSP is configured in the frontend:

```bash
grep -rn 'Content-Security-Policy\|<meta.*http-equiv.*Content-Security' web/index.html web/public/ web/src/ --include='*.html' --include='*.ts' --include='*.tsx' 2>/dev/null
```

If CSP is handled by the reverse proxy (Caddy), verify the Caddyfile includes a `Content-Security-Policy` header with AT MINIMUM:
- `default-src 'self'`
- `script-src 'self'` (no `'unsafe-inline'` or `'unsafe-eval'`)
- `style-src 'self' 'unsafe-inline'` (Tailwind may need inline styles)
- `connect-src 'self' wss:` (for WebSocket connections)
- `img-src 'self' data: blob:` (for canvas/blob URLs)
- `worker-src 'self'` (for service worker)
- `frame-ancestors 'none'` (prevent clickjacking)

If no CSP exists anywhere, flag as **HIGH** and add a meta tag to `web/index.html`:

```html
<meta http-equiv="Content-Security-Policy" content="default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' wss:; img-src 'self' data: blob:; worker-src 'self'; frame-ancestors 'none';">
```

#### 4b. Additional security headers verification

If headers are set by the reverse proxy, verify these exist in the Caddyfile. If the app can run without a proxy, add them via Vite dev server config or a middleware:

| Header | Required Value | Purpose |
|--------|---------------|---------|
| `X-Content-Type-Options` | `nosniff` | Prevent MIME-type sniffing |
| `X-Frame-Options` | `DENY` | Prevent clickjacking |
| `Referrer-Policy` | `strict-origin-when-cross-origin` | Privacy |
| `Permissions-Policy` | `camera=(), microphone=(), geolocation=()` | Restrict browser APIs |
| `Strict-Transport-Security` | `max-age=63072000; includeSubDomains` | Force HTTPS |

#### 4c. Subresource Integrity (SRI)

Check if any external resources are loaded via CDN (script tags, link tags):

```bash
grep -rn '<script.*src=.*http\|<link.*href=.*http' web/index.html web/public/ --include='*.html' 2>/dev/null
```

Any external resource without `integrity="sha384-..."` and `crossorigin="anonymous"` attributes is **HIGH**. Since this project bundles all dependencies locally, there should be none — verify this remains true.

---

### 5. Cross-Site Request Forgery (CSRF) Protection

#### 5a. Authentication mechanism review

Verify the app uses Bearer token authentication (not cookies):

```bash
grep -rn 'cookie\|Cookie\|csrf\|CSRF\|xsrf\|XSRF' web/src/ --include='*.ts' --include='*.tsx' | grep -v 'node_modules\|_test\.\|\.test\.'
```

If Bearer tokens via `Authorization` header are used (not cookies), CSRF is inherently mitigated. Verify:
- No `withCredentials: true` on fetch/XHR requests (unless same-origin cookies are needed)
- No `credentials: 'include'` on fetch requests (same reason)
- API calls use `Authorization: Bearer` header, not cookie-based auth

#### 5b. State-changing GET requests

Verify no GET request triggers a state mutation (create, update, delete):

```bash
grep -rn '\.GET\s*(' web/src/ --include='*.ts' --include='*.tsx' | grep -v 'node_modules\|_test\.\|\.test\.'
```

All state-changing operations MUST use POST, PUT, PATCH, or DELETE. A GET that mutates state is **MEDIUM** because it can be triggered by a simple link or image tag.

---

### 6. Dependency Security

#### 6a. Known vulnerabilities

Run the dependency audit:

```bash
cd web && npm audit --audit-level=high 2>&1
```

Flag any HIGH or CRITICAL vulnerability. For each:
- Check if an upgrade is available (`npm audit fix`)
- If no fix exists, assess if the vulnerable code path is reachable
- Document accepted risks for unfixable vulnerabilities

#### 6b. Outdated dependencies

Check for significantly outdated packages:

```bash
cd web && npm outdated 2>&1 | head -30
```

Flag any dependency more than 1 major version behind. Security patches often only apply to the latest major version.

#### 6c. Unnecessary dependencies

Verify no unused dependencies exist:

```bash
cd web && npx depcheck --ignores='@types/*,@testing-library/*,@vitest/*,tailwindcss,@tailwindcss/*,eslint*,typescript,jsdom,vite,@vitejs/*' 2>&1
```

Unused dependencies increase attack surface. Remove any that are not imported.

#### 6d. Supply chain safety

Verify `package-lock.json` exists and is committed to version control:

```bash
git ls-files web/package-lock.json
```

If missing, `npm install` resolves versions non-deterministically, risking supply chain attacks. Flag as **HIGH**.

---

### 7. Secure Error Handling

#### 7a. Error information leakage

Scan for error displays that might expose internals:

```bash
grep -rn 'err\.message\|error\.message\|err\.stack\|error\.stack\|console\.error\|console\.warn\|console\.log' web/src/ --include='*.ts' --include='*.tsx' | grep -v 'node_modules\|_test\.\|\.test\.'
```

Verify:
- Stack traces are NEVER displayed to users in the UI
- `console.error` in production code does not log sensitive data (tokens, passwords, PII)
- API error responses shown to users use the server's sanitized message, not raw error objects
- Network errors show generic messages ("Connection failed"), not technical details

#### 7b. React error boundaries

Check if error boundaries exist to prevent white-screen crashes:

```bash
grep -rn 'ErrorBoundary\|componentDidCatch\|getDerivedStateFromError' web/src/ --include='*.tsx' --include='*.ts'
```

A missing error boundary is **MEDIUM** — an unhandled error in any component crashes the entire app, potentially exposing the last rendered state or leaving the user stranded.

#### 7c. Unhandled promise rejections

Verify async operations have proper error handling:

```bash
grep -rn '\.then(\|await ' web/src/ --include='*.ts' --include='*.tsx' | grep -v 'catch\|try\|_test\.\|\.test\.' | head -20
```

Unhandled rejections can leak error details to the console. All async operations should use try/catch or `.catch()`.

---

### 8. Input Validation & Sanitization

#### 8a. Form input validation

For every form in `web/src/features/`:
- Login form: email format validation, password length limits
- Register form: email, password, display name validation
- Profile edit: field length limits, character validation
- Device/group management: name length limits

Verify:
- Client-side validation provides user feedback (not security — server re-validates)
- No input allows unbounded length (could cause UI overflow or memory issues)
- Email inputs use `type="email"` for basic browser validation
- Password inputs use `type="password"` with `autocomplete="current-password"` or `autocomplete="new-password"`

#### 8b. File upload validation

If file upload exists, verify:
- File type validation (MIME type AND extension)
- File size limits enforced client-side (before upload)
- File name sanitization (no path traversal characters: `../`, `\`, null bytes)
- Uploaded files are NOT rendered as HTML (prevent stored XSS via SVG/HTML uploads)

#### 8c. Search and filter inputs

Verify search/filter fields do not inject into:
- URL query parameters without encoding (`encodeURIComponent`)
- API request bodies without validation
- DOM elements without escaping

---

### 9. Third-Party Integration Security

#### 9a. Service Worker security

Audit `web/public/sw.js`:
- Service worker scope is restricted to the app origin
- No `importScripts()` from external origins
- Push notification data is validated before display (no unsanitized HTML in notifications)
- `notification.data` fields are validated before use in navigation
- Cache strategy does not cache sensitive API responses (auth tokens, user data)
- Service worker updates properly (`skipWaiting` + cache purge)

#### 9b. xterm.js security

Audit terminal component usage:
- Terminal input is sent as binary protocol frames, not interpreted as HTML
- Terminal output rendering is handled by xterm.js (safe — renders to canvas/DOM text nodes)
- No terminal escape sequence injection from server to client that could execute JS
- Clipboard operations (`navigator.clipboard`) require user gesture

#### 9c. Canvas/WebGL security

If remote desktop uses canvas rendering:
- Canvas `toDataURL()` or `toBlob()` output is not sent to third parties
- No `eval()` or dynamic code execution based on canvas data
- Frame data is rendered as pixels only, not interpreted as HTML or scripts

---

## PART 2 — PERFORMANCE

---

### 10. Bundle Size & Code Splitting

#### 10a. Bundle analysis

Run the production build and analyze output:

```bash
cd web && npx vite build 2>&1 | tail -30
```

Check total bundle size. Thresholds:
- Main JS bundle > 250 KB (gzipped): **HIGH** — needs code splitting
- Main CSS bundle > 50 KB (gzipped): **MEDIUM** — check for unused styles
- Any single chunk > 500 KB (gzipped): **HIGH** — split further

#### 10b. Route-based code splitting

Check if routes use lazy loading:

```bash
grep -rn 'React\.lazy\|lazy(' web/src/router.tsx
```

If ALL routes are eagerly imported, flag as **HIGH**. Implement route-based splitting:

```tsx
const Dashboard = lazy(() => import('./features/dashboard/DashboardPage'));
const DeviceDetail = lazy(() => import('./features/devices/DeviceDetailPage'));
```

Priority routes to split (by payload size and access frequency):
- Terminal/Desktop session views (heavy: xterm.js, WebRTC, canvas)
- Admin pages (settings, audit log, user management)
- Profile page
- Agent setup page

Keep synchronous: Login, Register, Layout (needed on first load).

#### 10c. Tree shaking verification

Verify Vite's tree shaking is effective:
- No barrel exports (`index.ts` re-exporting everything) that defeat tree shaking
- No side-effect imports (`import './module'` without using exports)
- `package.json` has `"sideEffects": false` or specific side-effect files listed

Check for barrel files:

```bash
find web/src -name 'index.ts' -o -name 'index.tsx' | head -20
```

#### 10d. Dynamic imports for heavy libraries

Check if large libraries are dynamically imported:

```bash
grep -rn "from '@msgpack/msgpack'\|from '@xterm/xterm'\|from '@xterm/addon" web/src/ --include='*.ts' --include='*.tsx'
```

Libraries used only in specific routes should be dynamically imported:
- `@xterm/xterm` — only needed in terminal session view
- `@xterm/addon-fit` — only needed with xterm
- `@msgpack/msgpack` — needed for all transport, acceptable in main bundle

---

### 11. Asset Optimization

#### 11a. Image format and optimization

Scan for image assets:

```bash
find web/src web/public -name '*.png' -o -name '*.jpg' -o -name '*.jpeg' -o -name '*.gif' -o -name '*.svg' -o -name '*.webp' -o -name '*.avif' 2>/dev/null
```

For each image:
- Raster images (PNG/JPG) > 100 KB: convert to WebP or AVIF (**MEDIUM**)
- SVGs: verify no embedded scripts or event handlers (`onload`, `onerror`)
- Verify `loading="lazy"` on below-the-fold `<img>` tags
- Verify `width` and `height` attributes are set (prevents CLS)

#### 11b. Font optimization

Check for custom font loading:

```bash
grep -rn '@font-face\|font-display\|preload.*font\|woff2\|woff' web/src/ web/public/ --include='*.css' --include='*.html' 2>/dev/null
```

If custom fonts are used:
- Use `font-display: swap` or `optional` to prevent FOIT (Flash of Invisible Text)
- Preload critical fonts: `<link rel="preload" as="font" crossorigin>`
- Use WOFF2 format (best compression)
- Subset fonts to include only needed character sets

---

### 12. Critical Rendering Path

#### 12a. Script loading strategy

Check `web/index.html` for script loading:

```bash
grep -n '<script' web/index.html
```

Verify:
- Main entry script uses `type="module"` (Vite default — deferred by spec)
- No render-blocking scripts in `<head>` without `async` or `defer`
- Critical CSS is inlined or loaded with high priority

#### 12b. Resource hints

Check for preload/prefetch directives:

```bash
grep -rn 'rel="preload"\|rel="prefetch"\|rel="preconnect"\|rel="dns-prefetch"' web/index.html web/src/ --include='*.html' --include='*.tsx' 2>/dev/null
```

Recommended resource hints:
- `<link rel="preconnect" href="...">` for API origin (if cross-origin)
- `<link rel="preload" as="font">` for critical fonts
- Vite handles CSS/JS preloading automatically for production builds

#### 12c. CSS delivery

Verify Tailwind CSS delivery is optimized:
- Tailwind's JIT compiler generates only used classes (verify via build output size)
- No large unused CSS files imported
- No render-blocking CSS imports in component files

---

### 13. React Rendering Performance

#### 13a. Unnecessary re-renders

Scan Zustand store usage for common anti-patterns:

```bash
grep -rn 'useDeviceStore()\|useAuthStore()\|useConnectionStore()\|useSessionStore()\|useAdminStore()' web/src/ --include='*.tsx' --include='*.ts' | grep -v '_test\.\|\.test\.'
```

Using the entire store (`useStore()` without a selector) causes re-renders on ANY store change. Flag as **MEDIUM**. Fix by using selectors:

```tsx
// BAD — re-renders on every store change
const store = useDeviceStore();
// GOOD — re-renders only when devices change
const devices = useDeviceStore((s) => s.devices);
```

#### 13b. Expensive computations in render

Search for computations that should be memoized:

```bash
grep -rn '\.filter(\|\.map(\|\.sort(\|\.reduce(\|Object\.keys(\|Object\.entries(' web/src/ --include='*.tsx' | grep -v '_test\.\|\.test\.'
```

For each, check if:
- The computation runs on every render (not wrapped in `useMemo`)
- The input array/object is large (>100 items)
- The result is used as a dependency in other hooks or passed as props

Flag large unmemorized computations as **MEDIUM**.

#### 13c. Event handler creation in render

Search for inline arrow functions in JSX that create new references each render:

Common patterns that cause unnecessary child re-renders when passed as props:
- `onClick={() => handler(id)}` inside `.map()` loops — creates N new functions per render
- Callbacks passed to memoized children without `useCallback`

This is **LOW** unless measured to cause visible jank (>50ms render time).

#### 13d. Component key usage

Verify list rendering uses stable, unique keys:

```bash
grep -rn '\.map(' web/src/ --include='*.tsx' | head -30
```

Check that:
- Array index is NOT used as key for lists that can reorder, insert, or delete (`key={index}` is **LOW**)
- Keys are derived from stable identifiers (IDs, unique names)
- No duplicate keys in sibling elements

---

### 14. List Virtualization & Large Dataset Handling

#### 14a. Large list rendering

Identify components that render potentially large lists:
- Device list (could grow to hundreds)
- Audit log entries (could grow to thousands)
- File browser listings
- User management list

For each list, check:
- If the list can exceed ~100 items without virtualization: **MEDIUM**
- If the list renders all items regardless of viewport visibility
- If pagination or infinite scroll is implemented

Recommendation: use `@tanstack/react-virtual` or `react-window` for lists exceeding 100 items.

#### 14b. Table rendering efficiency

For table components (audit log, user list, device list):
- Verify pagination is enforced (max page size <= 200)
- Check if sorting/filtering happens server-side for large datasets
- Verify no full-table re-render on single-row updates

---

### 15. Network & Caching Efficiency

#### 15a. API request deduplication

Check for duplicate API calls on mount:

```bash
grep -rn 'useEffect.*fetch\|useEffect.*load\|useEffect.*get' web/src/ --include='*.tsx' | grep -v '_test\.\|\.test\.'
```

Verify:
- React strict mode double-mount doesn't trigger duplicate API calls
- Navigation between routes doesn't refetch unchanged data
- Polling intervals (if any) are cleaned up on unmount

#### 15b. HTTP caching headers

Verify the server/proxy sets appropriate caching headers:
- Static assets (JS, CSS, images with hash): `Cache-Control: public, max-age=31536000, immutable`
- API responses: `Cache-Control: no-store` for authenticated data
- HTML entry point: `Cache-Control: no-cache` (revalidate on each visit)

Vite adds content hashes to asset filenames by default — verify this is not disabled in `vite.config.ts`.

#### 15c. WebSocket connection management

Audit WebSocket lifecycle:
- Only one WebSocket connection per session (no duplicate connections)
- Reconnection logic uses exponential backoff (not tight loop)
- Connection is closed on component unmount or session end
- Ping/pong keeps connection alive (check interval)
- No unnecessary re-subscriptions on re-renders

#### 15d. Service Worker caching strategy

Audit `web/public/sw.js` caching behavior:
- Navigation requests: network-only (correct for SPA)
- Static assets: cache-first with hash-based invalidation
- API responses: network-only (never cache authenticated responses)
- Stale caches are purged on service worker activation

---

### 16. Web Vitals & Core Metrics

#### 16a. Largest Contentful Paint (LCP)

Check for LCP bottlenecks:
- Is the main content rendered server-side or does it require JS hydration?
- Are critical resources preloaded (fonts, hero images)?
- Is the initial HTML response fast (no blocking API calls before render)?
- Does the login page render immediately or wait for auth check?

#### 16b. Interaction to Next Paint (INP)

Check for interaction responsiveness:
- Click handlers that trigger expensive synchronous operations
- Form submissions that block the UI during API calls (should show loading state)
- Modal/dialog open/close transitions that cause layout recalculation
- Large state updates that trigger cascading re-renders

#### 16c. Cumulative Layout Shift (CLS)

Check for layout instability:
- Images without explicit dimensions (`width`/`height`)
- Dynamic content inserted above the fold (toasts, banners, loading spinners)
- Font swaps causing text reflow
- Conditional rendering that shifts existing content

```bash
grep -rn '<img\|<video\|<iframe' web/src/ --include='*.tsx' | grep -v 'width\|height\|_test\.\|\.test\.'
```

Images/media without dimensions are **MEDIUM** CLS risk.

---

### 17. Accessibility (a11y) Baseline

#### 17a. Semantic HTML

Verify core semantic elements are used:
- Navigation uses `<nav>`, not `<div>`
- Main content uses `<main>`, not `<div>`
- Headings follow hierarchy (`h1` > `h2` > `h3`) — no skipped levels
- Buttons use `<button>`, not `<div onClick>`
- Links use `<a>`, not `<span onClick>`

```bash
grep -rn 'onClick.*<div\|onClick.*<span' web/src/ --include='*.tsx' | grep -v '_test\.\|\.test\.'
```

Clickable `<div>` or `<span>` without `role="button"` and keyboard handlers is **MEDIUM**.

#### 17b. Keyboard navigation

Verify:
- All interactive elements are focusable (`tabIndex` where needed)
- Focus trapping in modals/dialogs (focus doesn't escape to background)
- `Escape` key closes modals and dropdowns
- Tab order follows visual layout (no unexpected jumps)
- No keyboard traps (can always tab away from an element)

#### 17c. ARIA attributes

Verify:
- Form inputs have associated `<label>` elements or `aria-label`
- Error messages are linked to inputs via `aria-describedby`
- Loading states use `aria-busy="true"` on containers
- Dynamic content updates use `aria-live` regions for screen readers
- Icons used as buttons have `aria-label` (not just visual meaning)

#### 17d. Color contrast and visibility

Verify:
- Text meets WCAG 2.1 AA contrast ratios (4.5:1 for normal text, 3:1 for large text)
- Error states don't rely solely on color (add icons or text labels)
- Focus indicators are visible (not removed with `outline: none` without replacement)
- Interactive elements have visible hover/focus states

---

### 18. TypeScript Strict Mode Compliance

#### 18a. Strict mode verification

Verify `web/tsconfig.json` has `"strict": true` enabled. Check sub-options:

```bash
grep -n 'strict\|noImplicitAny\|strictNullChecks\|strictFunctionTypes' web/tsconfig.json web/tsconfig.app.json 2>/dev/null
```

If `strict` is not `true`, flag as **HIGH** — type safety prevents entire categories of bugs.

#### 18b. Type safety violations

Search for type safety escapes:

```bash
grep -rn 'as any\| any;\| any,\|: any\|<any>' web/src/ --include='*.ts' --include='*.tsx' | grep -v 'node_modules\|_test\.\|\.test\.' | grep -v '\.d\.ts'
```

Each `any` usage is a potential security and correctness risk:
- `any` in API response handling: **HIGH** (bypasses type checking on external data)
- `any` in internal utilities: **MEDIUM**
- `any` in type assertions (`as any`): **MEDIUM** (usually papering over a real bug)

#### 18c. Non-null assertions

Search for non-null assertions that could cause runtime errors:

```bash
grep -rn '[a-zA-Z]!' web/src/ --include='*.ts' --include='*.tsx' | grep -v 'node_modules\|_test\.\|\.test\.\|\.d\.ts\|import\|from\|!=\|!=' | head -30
```

Non-null assertions (`value!.prop`) bypass TypeScript's null checks — each should be verified or replaced with proper null handling.

---

## Summary Report

After completing all checks, print a table:

```
+----------+-------+-----------------------------------------------------------+--------+
| Section  | Count | Finding                                                   | Status |
+----------+-------+-----------------------------------------------------------+--------+
| 1  XSS   |   0   | No unsafe rendering, URL injection, or DOM manipulation   | PASS   |
| 2  Store  |   0   | Token lifecycle secure, no sensitive data retained        | PASS   |
| 3  Comms  |   0   | HTTPS enforced, WS auth secure, protocol validated        | PASS   |
| 4  CSP    |   0   | CSP and security headers configured                      | PASS   |
| 5  CSRF   |   0   | Bearer auth used, no cookie-based auth                    | PASS   |
| 6  Deps   |   0   | No known vulnerabilities, deps current                    | PASS   |
| 7  Errors |   0   | No info leakage, error boundaries present                 | PASS   |
| 8  Input  |   0   | Forms validated, file uploads sanitized                   | PASS   |
| 9  3rdPty |   0   | Service worker safe, xterm safe, no external scripts      | PASS   |
| 10 Bundle |   0   | Code splitting, tree shaking, bundle size within limits   | PASS   |
| 11 Assets |   0   | Images optimized, fonts loaded efficiently                | PASS   |
| 12 CRP    |   0   | No render-blocking resources, resource hints present      | PASS   |
| 13 React  |   0   | No unnecessary re-renders, selectors used properly        | PASS   |
| 14 Lists  |   0   | Large lists virtualized or paginated                      | PASS   |
| 15 Net    |   0   | API calls deduplicated, caching configured                | PASS   |
| 16 Vitals |   0   | LCP, INP, CLS within acceptable thresholds                | PASS   |
| 17 a11y   |   0   | Semantic HTML, keyboard nav, ARIA, contrast               | PASS   |
| 18 TS     |   0   | Strict mode, no `any`, proper null handling               | PASS   |
+----------+-------+-----------------------------------------------------------+--------+
```

Status values: **PASS** (no issues), **FIXED** (issues found and remediated in-place with tests), **FAIL** (issues found, cannot auto-fix — explain why and provide exact remediation steps).

If ANY section is FAIL or FIXED, list every finding with:
- File path and line number
- CWE identifier (for security issues) or Web Vital metric (for performance issues)
- Severity (CRITICAL / HIGH / MEDIUM / LOW)
- Description of the vulnerability or performance issue
- Proof of concept or reproduction steps
- Fix applied or recommended

### Gate Criteria

The audit **FAILS** if any CRITICAL or HIGH finding remains unfixed. All security fixes MUST include tests.
