# Tenancy rework: tenant, organization, site

**Status:** specified from scoping sessions with the project owner, 2026-08-06. Not started.
**Blocks:** `rules-administration-screen.md`, and it revises assumptions inside the whole
`edge-first-telemetry-and-investigations.md` programme (see "What this changes elsewhere").
**Sequencing:** `strip-windows-agent-implementation.md` → **this** → edge-first work → rules screen.

## The model

```
Tenant        the MSP. The wall the database enforces.        [today's "organization", renamed]
  └─ Organization   one customer                              [NEW]
       └─ Site      a location or department                  [today's groups_, renamed + reparented]
            └─ Device
```

Chosen to match what the rest of the market means. In NinjaOne an *organization* is the customer
and a *location* is a site or department inside it, with settings resolving device → location →
organization. OpenGate currently uses "organization" for the opposite thing — the MSP wall — so
every technician hired from another product would read the word backwards.

**Why "tenant" for the outer ring:** the codebase already says it everywhere — `dbtx.Tenant`,
`WithTenant`, the `tenant_isolation_*` database policies, the "tenant-table gate" test. Renaming
the table and column to match the vocabulary the code already uses is the smaller change, and it
frees the word *organization* for the customer.

## Why now, and not later

- **One seeded row exists** and no screen anywhere displays the word. This is the cheapest the
  rename will ever be, by a wide margin.
- **The rules screen cannot be built without it.** Its settings, its noise badge and its stop
  switch all key on a customer that does not exist.
- **The edge-first programme is specified against the wrong boundary.** Its hourly alert ceiling
  and its incident grouping both key on "organization" meaning the MSP, which would give one
  customer's storm the power to silence every other customer, and would fold two customers'
  unrelated incidents into one.

## Decisions taken — settled, do not re-litigate

| # | Decision |
|---|---|
| N1 | **Match the industry.** Today's organization becomes the **tenant**; the new customer entity is the **organization** |
| N2 | **Three levels.** Today's machine groups **retire into** the site level rather than surviving alongside it. **No data migration** — there is no real group data yet |
| N3 | **No per-technician customer restriction now**, but **every read filters by organization** so adding it later is a change in one place rather than every screen |
| N4 | The database-enforced wall **stays at the tenant**. Organization and site are structural and for targeting, not additional walls |
| N5 | Settings resolve **device → site → organization → tenant default → what shipped** |

### Two judgements made without asking

**The rename and the new entity are separate phases.** Roughly 850 references move in the
rename. Mixing a mechanical rename with a new concept produces a diff nobody can review, and a
failure afterwards cannot be attributed to either half. Phase 1 must be behaviour-preserving and
provable as such.

**Only the tenant enforces isolation.** N3 asks that reads filter by organization, which is a
*query* concern, not a second wall. Adding a second database-enforced boundary would double the
policy surface on every table for a restriction nobody has asked for yet.

## Phase 1 — rename, behaviour-preserving

Nothing gains a capability. Every test that passed before passes after.

| From | To | Approximate references |
|---|---|---|
| `organizations` table | `tenants` | 150 in SQL |
| `org_id` column, `OrgID` field | `tenant_id`, `TenantID` | 321 in Go |
| `orgId` in the API and web | `tenantId` | 43 |
| The `/orgs/{orgId}/purge` endpoint | `/tenants/{tenantId}/purge` | 1 path |

The database isolation policies keep their current shape and meaning — only the column name
moves. `dbtx.Tenant`, `WithTenant` and the tenant-table gate already use the target vocabulary
and need no rename at all, which is the evidence this is the right direction.

## Phase 2 — the organization entity

`organizations` (the name, now free) — a customer inside a tenant: id, tenant id, name,
timestamps. Database-isolated by tenant, tenant-leading indexes, registered in the tenant-table
gate.

Devices gain an organization. Every read that lists or filters devices, incidents, alerts or
rules narrows by organization when one is selected (N3).

Management: create, rename, list, archive. Move a device between organizations. Screens for all
of it. A tenant with no organizations yet gets a default one so nothing is ever orphaned.

**A customer picker** in the interface, since a technician sees every organization in their
tenant. Without it a thirty-customer device list is unusable regardless of anything else, and the
rules screen's noise badge has no customer to count for.

## Phase 3 — sites

`sites` — id, tenant id, organization id, name. Today's `groups_` retires into this: the device's
`group_id` becomes `site_id`, and a site belongs to an organization.

**Do not touch `security_groups` or `security_group_members`.** Those are user permission groups,
an unrelated concept that merely shares the word. Confirm before every rename pass.

## What this changes elsewhere

The edge-first specification and both sibling plans need revising once this lands:

- **Hourly alert ceiling** — currently 500 per "organization" meaning the MSP. Moves to the
  **organization** in the new sense, so each customer gets its own budget.
- **Incident grouping** — its broadest scope becomes the organization, so Contoso's driver
  rollout never folds together with Fabrikam's unrelated outage. The 312-alerts-to-one-incident
  case still works and now means what it says.
- **Rule settings** — keyed by organization, with site and device as the narrower levels (N5).
  The file-server-at-95 case resolves through the site or device level rather than needing tags,
  though tags remain useful as a cross-cutting label.
- **Erasure** — deleting a device, a site, an organization or a tenant each cascade, and an
  incident spanning several devices survives with the deleted ones removed.
- **`rules-administration-screen.md`** — its R9 noise badge and R5 stop switch both gain a real
  customer to key on; its tag-precedence rule stays, now as the tie-break *within* a level.

## Steps (TDD-first)

**Phase 1**

1. **Test first:** capture the current isolation behaviour as explicit tests if not already
   covered — a caller in tenant A cannot read tenant B's devices, users, groups, audit records,
   or machine inventory. These are the tests that prove the rename changed nothing.
2. Rename in one pass per language, migration included. Regenerate the API types for Go and
   TypeScript together.
3. **Test first:** login tokens carry the renamed claim. **Existing tokens become invalid on
   deploy** — assert the failure is a clean re-login prompt, not a confusing error.
4. Full suite green with no behaviour change. This phase ships on its own.

**Phase 2**

5. **Test first:** an organization belongs to exactly one tenant; it cannot be read or written
   across tenants; a tenant with none gets a default so no device is ever orphaned.
6. **Test first:** moving a device between organizations moves it cleanly — and its open
   incidents, alerts and history follow or are handled explicitly, never left pointing at the
   old one.
7. **Test first:** every list endpoint narrows by organization when one is selected, and returns
   the whole tenant when none is (N3). Table-driven over the endpoint list so a new endpoint
   added later without the filter fails.
8. **Test first:** deleting an organization cascades its sites and devices and leaves nothing
   orphaned; extend the existing lifecycle rehearsal rather than writing a second one.
9. Build the management screens and the customer picker.

**Phase 3**

10. **Test first:** a site belongs to one organization; a device's site must be in the device's
    organization — a mismatch is refused, not silently accepted.
11. **Test first:** settings resolve device → site → organization → tenant default → shipped
    default, proven with one disk rule where a site sets 95 and a single device sets 90.
12. Rename groups to sites, reparent, and confirm the security groups are untouched.
13. Docs, decision record, and revise the edge-first specification per the section above.

## Traps

- **`security_groups` is not `groups_`.** A careless rename pass will hit user permission groups
  and quietly change who can see what. Check every match before replacing.
- **The rename invalidates every existing login token.** Expected, but it must present as a
  re-login rather than an error nobody can interpret.
- **Phase 1 must be provably behaviour-preserving.** If any test needs changing beyond a name,
  something moved that should not have — stop and find it.
- **Migration numbering collides with the edge-first work**, which assumes 010 and 011. This plan
  lands first and takes them. Confirm what is free at implementation time and update the sibling
  plans rather than leaving two plans claiming one number.
- **Do not add a second database-enforced wall at the organization** (N4). Filtering is a query
  concern here; a policy on every table for a restriction nobody has requested is cost without a
  requirement.
- The device's organization must be derivable on the agent connection path too, since alerts and
  vitals arrive there and need the right customer attached.

## Out of scope

Per-technician customer restrictions (N3 defers the permission, not the filtering). A
customer-scoped administrator. Any end-customer login. Rule configuration itself — that is the
rules screen. Anything in the edge-first programme beyond revising its specification.

## Reviewer checklist

- [ ] Phase 1 changed names only — every pre-existing test passes untouched beyond renames.
- [ ] Cross-tenant isolation still proven on every table, including the renamed ones.
- [ ] `security_groups` untouched.
- [ ] Organizations cannot cross tenants; a tenant always has at least one.
- [ ] Device moves handle open incidents and history explicitly.
- [ ] Every list endpoint filters by organization, proven table-driven.
- [ ] Site must belong to its device's organization; mismatch refused.
- [ ] Resolution order proven across all four levels.
- [ ] Deletion cascades cleanly at every level; lifecycle rehearsal extended.
- [ ] Edge-first specification revised for the ceiling, the grouping and the settings keys.

## Verification

`cd server && go test ./...` (Postgres-backed), `cd web && npm run generate:api && npm test`,
`make lint`, `make e2e`, `/pentest-review`, `GO111MODULE=off go run ./scripts/check-doc-links`,
then `/precommit`. Phase 1 additionally: the full suite must pass with **no test edited beyond a
rename**.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit of its final phase, bump internal relative
links one `../` deeper, add the `phases.md` **Completed** row linking the archived path, and add
a decision record for the three-level model with an index row in `decisions.md`.
