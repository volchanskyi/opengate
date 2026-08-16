# EF-C5 — Investigations API: list, detail, transitions, evidence, coverage

**Master plan:** `edge-first-telemetry-and-investigations.md` §6.6, §7.1, §9.1 (Q10), §9.2, step 20.
**Acceptance criteria owned:** **C12**.
**Dependencies:** EF-C3 (the engine), EF-C2 (the store), EF-B8/EF-B9 (coverage and the catalogue for
the rules view).
**Blocks:** EF-C6.

## Surface

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/investigations` | Triage queue — filter by status, severity, `rule_id`, device, assignee; paginated |
| `GET` | `/investigations/{id}` | Incident + timeline + folded alerts |
| `POST` | `/investigations/{id}/status` | Transition; `cause_code` **required** on manual resolution |
| `POST` | `/investigations/{id}/assignee` | Assignment |
| `POST` | `/investigations/{id}/comments` | Comment → an `incident_events` row |
| `GET` | `/investigations/{id}/alerts/{alertId}/evidence` | Decoded evidence (server decompresses `deflate-1`) |
| `GET` | `/devices/{id}/incidents` | The device page's incidents strip |
| `GET` | `/rules` | Curated catalogue + per-rule coverage `active/unsupported/unknown` (I8's "surfaced in the UI") |

## Authorization — a decision the master plan leaves open (state it, don't drift)

[ADR-062](../../../docs/adr/ADR-062-tenant-scoped-reads-and-fleet-summary.md) settles the shape:
the **tenant** is the visibility boundary, `is_admin` the mutation boundary for *configuration*,
and device *commands* need tenant membership only. (That ADR predates the tenancy rework and says
"organization" for what is now the tenant — EF-Z1 reconciles its wording.)

Applied here: reading incidents, transitioning status, assigning, and commenting are **operational**
work on the tenant's own resources → **tenant membership**, narrowed by the selected organization.
Mutating **rule bindings and rollout state** is configuration → **admin**. Put this in the ADR (EF-Z1) rather than leaving it implicit in
handler code, and use the named-guard idiom the ADR-027 pen-test rules recognise.

## File inventory

- **Modify:** [api/openapi.yaml](../../../api/openapi.yaml) — the paths above; regen Go
  (`oapi-codegen`) and TS (`npm run generate:api`) in the same commit.
- **Create:** `server/internal/api/handlers_incidents.go` (+ tests).
- **Modify:** [api.go](../../../server/internal/api/api.go) — wire the store/engine ports.
- **Docs:** [API-Reference.md](../../../docs/API-Reference.md).

## Steps (TDD-first)

1. **Test first — tenancy on every route:** an org-B caller gets *not found* for an org-A incident,
   its alerts, its evidence and its events — including with a crafted id. Route-by-route, because a
   single missed guard is the whole class of defect ADR-062 exists to close.
2. **Test first (C12):** seed **10 000 open incidents** in `testpg`, then assert the list query's
   plan uses the `organization_id`-leading index with **no sequential scan** (`EXPLAIN`), for the default sort
   and for each filter combination the UI can produce. A wall-clock assertion in the unit suite is
   flaky by construction; the plan assertion is deterministic and is what actually keeps p99 flat.
   Record the measured p99 separately as evidence (EF-Z1).
3. **Test first — pagination is stable** under concurrent inserts: a keyset/cursor page does not skip
   or repeat when a new incident arrives between pages. Offset pagination over a live triage queue
   silently loses rows; pick keyset and prove it.
4. **Test first — transitions:** the API rejects an illegal transition with a typed 4xx, requires
   `cause_code` on manual resolution, rejects a code outside the closed set (the DB check constraint
   is the backstop, not the message), and appends an `incident_events` row for every accepted change.
5. **Test first — evidence read:** the server decompresses `deflate-1` and returns structured
   evidence; a blob with an unknown `evidence_codec` returns a typed error rather than raw bytes; a
   `truncated: true` blob is served with the flag intact so the UI can say so.
6. **Test first — evidence never leaks:** the response cannot carry an unredacted line (C10 is
   enforced at the edge; this asserts the read path does not undo it, e.g. by echoing a raw label).
7. **Test first — coverage view:** `GET /rules` reports `active + unsupported + unknown == fleet
   size` per rule (the API form of B8).
8. Implement; regenerate Go + TS; docs.

## Traps

- **Regenerate both languages in the same commit.** A stale `web/src/types/api.d.ts` compiles and
  lies; the OpenAPI drift check in the pen-test gate is what catches it.
- Evidence responses can be tens of KB. Bound the list endpoint so it **never** embeds evidence —
  detail and evidence are separate calls, or the triage queue drags megabytes.
- The device-page strip must not become a second list implementation; share the query and the filter
  types.
- `GET /rules` reads the embedded catalogue plus DB bindings — do not expose the raw predicate in a
  form that implies it is editable (§4.2: no rule-authoring surface).
- The pen-test gate (ADR-027) expects the named in-scope guards; new handlers that resolve a device
  or an incident must use them, not an ad-hoc check.

## Out of scope

The web UI (EF-C6). Rule binding/rollout **mutation** endpoints unless EF-B9/EF-B11 already need
them — if they do, they are admin-gated and belong to those plans, not this one.

## Reviewer checklist

- [ ] Every route tenancy-tested, including crafted ids.
- [ ] 10 000-row plan assertion for every filter the UI can produce; keyset pagination proven stable.
- [ ] Illegal transitions and out-of-set cause codes rejected at the API **and** the DB.
- [ ] Evidence decoded server-side; unknown codec is a typed error; `truncated` preserved.
- [ ] List endpoint never embeds evidence.
- [ ] OpenAPI + Go + TS regenerated together; spec-drift check clean.

## Verification

`cd server && go test ./internal/api/...` (testpg-backed), `cd web && npm run generate:api && npm test`,
`/pentest-review`, `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row.
