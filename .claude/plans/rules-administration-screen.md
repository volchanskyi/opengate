# Rules administration — the operator screen for curated detection

**Status:** specified from a scoping session with the project owner, 2026-08-06. Not started.
**Depends on:** `archive/tenancy-organizations-and-sites.md` (the tenant → organization → site model, landed),
`edge-first-b9-rule-catalogue-and-bindings.md` (settings storage),
`edge-first-b11-rollout-safety.md` (rollout machinery),
`edge-first-c2-alert-store-and-ingest.md` (alert records for the noise badge).
**Sequencing:** tenancy rework (landed) → edge-first work → this.

## Intent

The edge-first work builds a full rule configuration system whose only possible operator is a
database console. This plan gives it a front door: see every curated rule, how noisy it is, how
far it has rolled out, which machines it covers, and change what is meant to be changeable.

## Why it cannot wait

Three things in the edge-first design have no operator path without it:

1. **A rule tuned wrong stays wrong.** The "false alarm" resolution code exists to tell you which
   rule needs moving. Nothing can act on that answer.
2. **The stop switch is unreachable.** It is the mitigation for the one High-impact risk in the
   programme — a bad rule degrading five thousand machines — and it is a database row nobody
   can flip.
3. **The alert ceilings were set from an estimate.** 500 an hour was chosen as roughly twelve
   times a rate that has never been measured. Being unable to move it without a software release
   turns a wrong guess into an outage.

## Decisions taken in the scoping session — settled, do not re-litigate

| # | Decision |
|---|---|
| R1 | **Top-level Rules section.** Everyone in a tenant reads; only admins edit. Read access matters — a tech resolving something as a false alarm must be able to see the rule that produced it |
| R2 | **Platform staff do all tuning.** The admin flag is per tenant, not per customer (see the tenancy note below), so editing is a platform-operator job and technicians read only. Revisit when a customer needs to self-serve |
| R3 | **Both alert ceilings editable, each with a hard maximum** that cannot be exceeded. The per-machine ceiling is enforced on the machine, so it travels down with the rules |
| R4 | **Rollout populations and waiting periods adjustable per rule.** The automatic pull-back is **never** switchable off |
| R5 | **Stop switch at two scopes** — per **organization** (one customer), and one tenant-wide action that stops a rule for every customer at once |
| R6 | **Device tags**, flat labels chosen from a list each organization maintains. A **cross-cutting** dimension, not a tenancy level — `production` and `finance` describe machines spanning sites. New concept, built here |
| R7 | **Rule upgrades apply automatically and keep the customer's tuning**, with the rule page showing that the definition changed |
| R8 | **A tuned value outside a new version's allowed range is moved to the nearest allowed value** and flagged until an admin acknowledges it — never silently dropped, never left invalid |
| R9 | **A coloured badge on each rule** shows its recent alert count, counted from stored alert records for the **organization currently selected** in the customer picker. Colour is relative to **that rule's own usual rate**, so a rule meant to be chatty does not sit permanently red |
| R10 | **Every configuration change lands in the existing audit log** with who and when |

### Tenancy note — the constraint behind R2

After the tenancy rework the levels are **tenant → organization → site → device**. The database
enforces isolation at the **tenant** (the MSP); an **organization** is one customer and is a
scoping level, not a second wall.

`users.is_admin` is a single flag meaning "can change configuration anywhere in this tenant" —
there is no administrator scoped to one customer. R2 records that as accepted for now rather than
worked around here; adding one belongs in the tenancy work, not on this screen.

### One judgement made without asking

**Tag conflicts within one level resolve by an explicit precedence the admin sets, shown in the
UI.** Two tags can both match one machine, and the edge-first plan requires a deterministic
tie-break. Across tenancy levels there is no ambiguity — the narrower level always wins — so
precedence only settles ties *inside* a level.

Every invisible rule (newest wins, alphabetical, row order) produces a threshold nobody can
predict from the screen. An explicit, visible order costs one column and removes the class of
question entirely. Flagging it because it was decided rather than asked.

## Screen shape

**The list.** One row per curated rule: name, what it watches, on or off, rollout stage with
progress, machines covered, and the R9 noise badge. Sorted so anything red or halted floats up.

**The rule page**, four sections:

1. **What it does** — read-only. What it watches, what counts as bad, how long it must persist,
   how its alerts group, how serious it is. Rendered as description, never as anything that
   implies the logic is editable (a rule-authoring surface is forbidden by the master plan).
2. **Tuning** — the adjustable numbers, each showing its allowed range, laid out by level:
   organization default, then site overrides, then machine overrides, plus any cross-cutting
   tag overrides in explicit precedence order. The resolved value for any given machine must be
   inspectable — "why is FS01 at 95?" is the question this section exists to answer.
3. **Coverage** — machines running it, machines that cannot, machines not heard from. The three
   must always add to the fleet size.
4. **Rollout** — current stage, population, waiting period, what the advance is waiting on,
   whether it has pulled itself back, and the stop switch. Stop is visually separate from the
   on/off toggle: they are different actions with different urgency.

**Alert limits** live on their own page, not per rule — they are a per-organization safety net
rather than a rule property. Set per customer, never per tenant.

**Tags** get a management page: the organization's label list, and assignment including bulk
assignment from the machine list.

## New storage

Migration number: **confirm the next free one at implementation time** — the tenancy rework lands
first and takes the next free numbers, with the edge-first work after it.

- `device_tags` — the label list per organization, and machine-to-label assignment. Forced
  isolation on `tenant_id`, organization-leading indexes, registered in the tenant-table gate.
- Alert ceilings per organization, with the hard maximum as a constant in code, not a stored row.
- Rollout population and waiting-period settings per rule.
- An explicit precedence column on rule settings rows.
- Acknowledgement state for an R8 clamp.

## API

All tenant-isolated and narrowed by the selected organization; every write admin-gated with the
named guards the pen-test gate recognises.

| Purpose | Access |
|---|---|
| List rules with coverage, rollout state and noise count | Organization member |
| One rule's full detail | Organization member |
| Change tuning, targeting, precedence | Admin |
| Enable, disable, stop — per organization and tenant-wide | Admin |
| Change rollout population and waiting period | Admin |
| Read and change alert ceilings | Read: member. Write: admin |
| Manage tag list and assignments | Admin |
| Acknowledge an R8 clamp | Admin |

## Steps (TDD-first)

1. **Test first — tags:** a label list scoped to one organization; a label cannot be assigned
   across organizations; deleting a label used by a rule setting is refused or detaches
   explicitly, never silently orphans the setting. Then build the storage.
2. **Test first — targeting resolution:** device beats site, site beats organization, organization
   beats what shipped. Two tags at the same level resolve by the explicit precedence. Proven with
   the master plan's own case — one disk rule, a file-server **site** at 95 and a named workstation
   at 90 — plus a case where a tag override and a site override collide, asserting the level wins.
   Then the resolver.
3. **Test first — tuning bounds:** a value outside the rule's declared range is refused on write
   with a typed error; a value for something the rule does not declare adjustable is refused.
4. **Test first (R8):** a new rule version that narrows a range moves an existing value to the
   nearest allowed one, records that it did, and surfaces it until acknowledged. Assert the alert
   **keeps firing** at the clamped value — going quiet is the failure this guards against.
5. **Test first (R3) — ceilings:** a value above the hard maximum is refused; the per-machine
   figure reaches the agent and is applied there; suppression is still counted at whatever value
   is set. Reuse the existing rule-delivery path, do not invent a second one.
6. **Test first (R4) — rollout settings:** population and waiting period are adjustable and take
   effect on the next evaluation; **the automatic pull-back cannot be switched off through any
   route** — assert the absence, not just the default.
7. **Test first (R5) — stop switch:** the per-organization stop affects one customer and no other;
   the tenant-wide action stops every organization in the tenant and none outside it; both are
   effective without a software release,
   through the reconnect path as well as the next push.
8. **Test first (R9) — noise badge:** the count is scoped to the selected organization and
   never includes another's alerts; the colour is computed against that rule's own recent history;
   a rule with no history yet renders neutral rather than alarming.
9. **Test first (R10) — audit:** every write in the API table above produces an audit record with
   actor and timestamp. Table-driven over the full endpoint list so a new endpoint added later
   without auditing fails the test.
10. **Test first — permissions:** an ordinary member reads everything and cannot write anything;
    each write endpoint refused individually rather than in one combined case.
11. Build the screens. Charts and heavy panels lazy-loaded; the bundle budget still applies.
12. Docs.

## Traps

- **Never render the rule's logic as editable.** The read-only section is description. A form
  control around a predicate is the authoring surface the master plan forbids.
- **The per-machine ceiling is enforced on the machine.** A screen that only writes a database
  row has changed nothing. The test that matters is the one asserting the agent applies it.
- **The noise count leaks across customers easily.** Isolation stops it crossing a tenant, but
  **nothing in the database stops it crossing an organization** — that scoping is the query's own
  job. A query missing it returns a plausible number that belongs to another customer. Assert with
  two organizations' data present inside one tenant.
- **Deleting a tag is not a free action.** It can silently widen a threshold across an estate by
  removing a targeted override. Refuse or make the consequence explicit.
- **Stop must not require the rule to be pushable.** A machine that is offline is stopped when it
  reconnects. Test the offline-then-reconnect path, not only the connected one.
- The web rules: strict types with no loose typing in production code, Tailwind only, keyed
  lookups rather than object indexing, and everything from a rule or a tag rendered as text.
- Mutation floor still applies to the new web feature and the new server package.

## Out of scope

Creating or managing organizations, and moving users or machines between them — the separate
piece of work that lands first. A customer-scoped administrator (R2). Authoring new rules.
Notifications of any kind. Any action against a machine from this screen.

## Reviewer checklist

- [ ] Rule logic is read-only and not rendered as a form.
- [ ] Resolution order proven, including the explicit precedence on an equal-specificity conflict.
- [ ] Out-of-range and undeclared values refused on write.
- [ ] A narrowed range clamps, flags, and **keeps firing**.
- [ ] Both ceilings honour the hard maximum; the per-machine one proven applied on the agent.
- [ ] Automatic rollback unreachable through every route.
- [ ] Stop proven per customer, platform-wide, and through reconnect.
- [ ] Noise count proven tenant-scoped with two organizations present; colour relative to the
      rule's own history; no-history renders neutral.
- [ ] Audit coverage table-driven over every write endpoint.
- [ ] Ordinary members refused on each write endpoint individually.

## Verification

`cd server && go test ./internal/rules/... ./internal/api/... ./internal/dbtx/...` (Postgres-backed),
`cd web && npm run generate:api && npm test`, `make taint-web`, `make e2e`, `/pentest-review`,
then `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal relative links one `../`
deeper, and add the `phases.md` **Completed** row linking the archived path.
