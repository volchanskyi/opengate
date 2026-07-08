# WS-11 — Log server ingest + on-demand raw broker + audit

**Objective:** Ingest log-rate dims into VictoriaMetrics (reuse the WS-4 client) and broker
on-demand **raw** log retrieval — bounded, **audited**, elevated-permission, with **nothing raw
persisted centrally**.

**Dependencies:** WS-4 (VM telemetry client + scoped reader), WS-10 (messages). **Blocks:** WS-12.

## Context

Handlers live in [`conn.go`](../../../server/internal/agentapi/conn.go); the on-demand path already
flows through `SendRequestDeviceLogs` / `handleDeviceLogsResponse`
([conn.go:144](../../../server/internal/agentapi/conn.go#L144)). Audit events have a home in the
existing `audit_events` table. The intended end state: raw lines are **transient**
(request→response), with **no RLS table for raw** — only the rate series live (in VM, `org_id`
label).

**Hard precondition — the "nothing raw persisted centrally" claim is false against today's code.**
The reused `handleDeviceLogsResponse`
([conn.go:318-334](../../../server/internal/agentapi/conn.go#L318)) calls
`deviceLogs.Upsert`, which DELETE-then-INSERTs raw `message` text into the central `device_logs`
table ([postgres_logs.go:22-40](../../../server/internal/device/postgres_logs.go#L22);
[001_initial.up.sql:143-155](../../../server/internal/db/migrations/001_initial.up.sql)). This is a
privacy/compliance gap, not wording. **Resolve as the first step of WS-11, before the broker is
wired** — pick one and record it in the raw-log-privacy ADR:

- **(a) Retire the central cache** — split the on-demand broker off `handleDeviceLogsResponse` so it
  streams the bounded response through without persisting; migrate/drop `device_logs` (check the web
  log viewer's read path first — it may need a transient-fetch replacement).
- **(b) Reclassify as bounded central raw** — keep `device_logs` but treat it as short-lived raw
  PII: `org_id` + RLS, retention/TTL deletion (wire into WS-20), audit on read, elevated permission;
  amend the privacy claim from "nothing central" to "bounded, RLS-protected, TTL-bound central".

## File inventory

- **Modify:** [`conn.go`](../../../server/internal/agentapi/conn.go) — rate dims → the WS-4 VM client
  (scoped by connection device→org); the raw query **broker** (bounded fan-out to the agent).
- **Modify:** [`api/openapi.yaml`](../../../api/openapi.yaml) + [`api.go`](../../../server/internal/api/api.go)
  — a logs query endpoint behind an **elevated permission**; **write an audit event** on every raw
  pull (who/which device/window/filters). Regenerate Go + TS.
- **Modify:** server redaction guard (defense-in-depth on the raw response, reusing WS-2/WS-13
  patterns).

## Steps (TDD-first)

0. **Precondition (decide + test first):** resolve the `device_logs` raw-persistence gap above —
   either prove the broker path persists nothing (a) or land the RLS/TTL/audit reclassification (b).
   A cross-tenant-deny + no-raw-persist (or RLS-deny) test pins whichever option is chosen.
1. **Test first:** rate dims land in VM with the `org_id` label; the scoped reader denies
   cross-tenant access → wire rate ingest to the WS-4 client.
2. **Test first:** raw broker is **bounded** (max lines/bytes/time), **writes an audit event**, and
   **enforces the elevated permission** (a viewer without it is denied) → implement the endpoint +
   handler.
3. **Test first:** server redaction guard strips known secrets from the raw response even if agent
   redaction is off → implement; regen OpenAPI both sides.

## Gotchas / constraints

- **Nothing raw is centrally persisted** — the broker streams the bounded response through; no
  central raw store ⇒ isolation is the connection scope, not an RLS table.
- Audit + elevated permission are **mandatory** (raw logs are secret-dense); length/time caps bound
  exposure.
- Authz scope from the connection's enrolled device→org — never trust agent-supplied org.

## Reviewer checklist

- [ ] Rate dims → VM (scoped label); cross-tenant deny tested.
- [ ] `device_logs` raw-persistence gap resolved (retired **or** RLS+TTL+audit reclassified); claim in docs matches code.
- [ ] Raw broker bounded; **audit event written**; elevated permission enforced; central raw store retired or fail-closed.
- [ ] Server redaction guard tested; OpenAPI regen committed (Go + TS); `/precommit` green.

## Verification

`cd server && go test ./internal/agentapi/... ./internal/api/...` (broker bounds, audit, RLS/label
scope, redaction). `/precommit` green. `/docs`: API + Monitoring pages.
