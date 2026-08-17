# EF-C6 — Investigations triage workspace in the web client

**Master plan:** `edge-first-telemetry-and-investigations.md` §4.1 (WS-C), §6.6, §8 (Web rows),
§14.2 (product views read the API), step 21.
**Acceptance criteria owned:** none in §12 — **the master plan defines no acceptance criterion for
step 21**. That is a gap; this plan defines W1–W6 below and they should be folded into §12 at
close-out (EF-Z1).
**Dependencies:** EF-C5 (the API), EF-B2 (the vitals the chart renders).
**Blocks:** nothing.

## Context

**Grafana has no ingress cluster-wide** (§2.2) — it is a platform-operator tool, never a product
surface. Every product view reads the OpenGate API, which is why the triage workspace exists here.

**The incident is the room** (D18): an incident in `new` **is** the triage queue, so there is no
separate promotion step, no "create investigation" button, and nothing to convert.

## Plan-local acceptance criteria

- **W1** `/investigations` lists open incidents with status, severity, rule, `occurrences`,
  `device_count`, first/last seen; filters by status, severity, rule and device; keyset pagination.
- **W2** `/investigations/:id` shows the timeline (`incident_events` in order), the folded alerts,
  and each alert's evidence — ranked dimensions, the three series, processes, redacted log lines —
  rendered from the frozen snapshot, with **no** call back to the device.
- **W3** Status transitions, assignment and comments are issued from the room; resolving requires a
  cause code from the closed set; an illegal transition is not offerable in the UI.
- **W4** The device page carries an **incidents strip** linking into the room.
- **W5** A truncated evidence blob says so; an incident whose device was purged renders without it,
  not as an error.
- **W6** Per-rule coverage (`active / unsupported / unknown`) is visible — I8's "surfaced in the UI".
  Silent partial coverage is the failure class WS-A exists to eliminate, and a number nobody can see
  is silent.

## File inventory

- **Create:** `web/src/features/investigations/` — list, detail, timeline, evidence panels, and a
  Zustand store (`web/src/features/investigations/state/`), mirroring the
  [devices](../../../web/src/features/devices/) feature's layout.
- **Modify:** [router.tsx](../../../web/src/router.tsx) — `investigations` and `investigations/:id` as
  lazy routes under the authenticated layout, matching the existing `withSuspense` idiom
  ([:44-48](../../../web/src/router.tsx#L44-L48)).
- **Modify:** the device detail page — the incidents strip.
- **Modify:** [DeviceMetrics.tsx](../../../web/src/features/devices/DeviceMetrics.tsx) — render the
  vitals set at 60 s (the drag-to-correlate UX is retired by EF-B7, not here).
- **Docs:** [Architecture.md](../../../docs/Architecture.md) (web client features).

## Steps (TDD-first)

1. **Test first (W1):** the list renders from a fixture response, filters compose, and an empty queue
   renders a real empty state rather than a spinner that never resolves. Loading, empty and error
   states are three distinct assertions.
2. **Test first (W2):** the detail view renders timeline order, folded alerts, and evidence; assert
   that opening a room issues **no** device-directed request — self-contained evidence is the whole
   architecture, and a stray fetch would quietly reintroduce the on-demand pull D2 forbids.
3. **Test first (W3):** the cause-code selector offers exactly the closed set; resolve is disabled
   until one is chosen; an illegal transition is not rendered as an option; a rejected transition
   surfaces the server's error rather than optimistically mutating local state.
4. **Test first (W4):** the strip appears on a device with open incidents, is absent (not empty-boxed)
   without them, and links to the room.
5. **Test first (W5):** `truncated: true` renders an explicit notice; an incident referencing a purged
   device renders the remaining devices and no error boundary.
6. **Test first (W6):** the coverage view shows all three states per rule and sums to the fleet size
   the API reports.
7. **Test first — polling:** any polling uses
   [`useVisibleInterval`](../../../web/src/lib/use-visible-interval.ts) so a hidden tab issues nothing —
   the established pattern; do not add a bare `setInterval`.
8. Implement; keep the initial JS bundle inside its budget — **250 KB gzipped, excluding the lazy
   charts chunk**, enforced by [`.size-limit.json`](../../../web/.size-limit.json) in CI. Lazy-load the
   feature the way the existing routes do.

## Traps

- Strict TypeScript, **no `any` in production code**; use the generated API types, do not hand-roll
  interfaces that will drift from the spec.
- Tailwind only — no custom CSS files.
- Dynamic keying (e.g. per-rule coverage maps) must use `Map.get`/`set`, not object index access, or
  `security/detect-object-injection` fires in `make taint-web`.
- Evidence contains user-controlled strings (log lines, process names). Render as **text**; never
  `dangerouslySetInnerHTML`, never a link built from an evidence field — the link-scheme allowlist
  exists for exactly this.
- No notification UI, no remediation buttons (restart/script/isolate/session) in the room — §4.2
  forbids both, and a "helpful" restart button is the scope creep the risk table names.
- Mutation floor: web ≥ 85 %. Assertions must be specific (rendered text, roles, request payloads),
  not existence checks, or the floor slips.

## Out of scope

Any on-demand pull from a device (D2). Notifications (§4.2). Rule authoring (§4.2).

## Reviewer checklist

- [ ] W1–W6 each have named tests; loading/empty/error states covered.
- [ ] Opening a room issues no device-directed request.
- [ ] Cause-code closed set enforced in the UI; illegal transitions not offerable.
- [ ] Evidence rendered as text; no raw HTML, no evidence-derived links.
- [ ] `useVisibleInterval` for any polling; bundle within budget.
- [ ] `npm test`, `make taint-web`, mutation floor ≥ 85 %.

## Verification

`cd web && npm test && npm run build`, `make taint-web`, `make e2e` (a full triage flow — use
`make e2e`, never bare `npx playwright test`), `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row, and fold W1–W6 into the master plan's §12 (or record the gap's
resolution in EF-Z1).
