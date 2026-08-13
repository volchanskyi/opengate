# EF-B9 — Embedded rule catalogue, CI cost gate, and Postgres bindings/rollout (`013_rules`)

**Master plan:** `edge-first-telemetry-and-investigations.md` §6.5, §7.1, §7.4, D24, step 12.
**Acceptance criteria owned:** **B12**.
**Dependencies:** EF-B8 (the grammar and its cost model are what the CI gate analyses).
**Blocks:** EF-B11 (rollout stages read `rule_rollout`).

## Design (fixed — §6.5 rejects both obvious alternatives; do not re-litigate)

| Layer | Home | Mutable at runtime? |
|---|---|---|
| **Rule definition** — predicate, grammar, evidence spec, coverage predicate, `group_by`, `group_window` | Versioned YAML `go:embed`-ed into the server | **No.** Immutable per `(rule_id, version)` |
| **RuleBinding** — customer parameter overrides, keyed `(organization_id, rule_id, level, level_key, selector)` | Postgres | Yes |
| **RuleRollout** — `enabled`, `canary_group`, `rollout_percent`, `kill` | Postgres | Yes |
| **Unsupported coverage** — which devices a rule *cannot* be evaluated on | Postgres | Yes (agent-reported) |

Definitions in Postgres would move the program's highest-impact gate out of CI — cost-bounding a
predicate before it reaches 5 000 customer endpoints is mandatory and free in CI, and a validator
bug in a runtime API path is a production incident. Definitions with no DB layer cannot express a
kill switch, canary state or coverage (the wall Netdata hit before adding DYNCFG).

**Bindings resolve down the tenancy levels.** `(organization_id, rule_id)` alone gives Contoso one threshold for
its whole estate, while FS01 wants `disk-critical` at 95 and DAL-WS-012 wants 90. Resolution is
**device → site → organization → tenant → embedded default**, and the ordering is not defined here:
it is [`internal/settings`](../../../server/internal/settings/settings.go), shipped by the tenancy
rework. The walk and the tie-break live in one place; the *values* stay in `rule_bindings` where the
rule that declares them can validate them against its own bounds. So the file-server-at-95 case
needs no special machinery: put the file servers in a site. The kill switch is the one setting that
does not resolve this way — a customer-wide stop must not be undone by a value on one machine, so it
reads the ladder the other way up (`settings.BroadestWins`) and lives on `rule_rollout`.
A binding also carries an optional **cross-cutting device-tag selector** with an explicit operator-set
`precedence` breaking ties between two tag selectors at the same level — never an invisible tie-break.
Across levels the narrower level always wins.

## Coverage: persist the durable half only (owner decision, 2026-08-13)

EF-B8 holds all three coverage states in memory
([`conn_coverage.go`](../../../server/internal/agentapi/conn_coverage.go)) and derives `unknown` as
`fleet − reported`. That is right for two of the three states and wrong for the third, because they
are not the same kind of fact:

- **`active` / `unknown` are liveness.** They are *supposed* to reset when the server loses its view
  of the fleet. FS01, a file server Contoso decommissioned three weeks ago without telling anyone, is
  `unknown` — which is exactly true. A row saying `active` because that is what FS01 last reported
  before it was unplugged would claim 500/500 machines watched when the real number is 499. **These
  stay in memory. Do not persist them.**
- **`unsupported` is a durable fact about the estate.** Contoso's 40 containerized agents can never
  evaluate `io-stalled` — the kernel's pressure accounting is not per-container — so that is a
  standing 8 % hole in Contoso's monitoring that belongs on a remediation list, not something that
  evaporates on a deploy. Today "how much of Contoso is `io-stalled` blind to?" answers differently
  depending on when the server last restarted, which makes a monthly coverage report
  non-reproducible. **This one is persisted here.**

Two rules make the persisted half safe, and both are the FS01 case in disguise:

1. **Only `unsupported` is ever written.** A device reporting `active` for a rule it has a row for
   **deletes** that row — never flips it to an `active` state. There is no stored `active`, so there
   is nothing that can go stale into a lie. This also keeps steady-state write volume at **zero**:
   a write happens on a state *change*, not on every summary.
2. **Deleting a device erases its coverage rows** (I9), or a decommissioned machine inflates the
   `unsupported` count forever.

The read then becomes: `unsupported` = persisted rows for devices still in the fleet (so an offline
container stays counted — that is the point), `active` = what memory currently holds, `unknown` =
`fleet − active − unsupported`. The identity still holds.

Rejected: persisting all three with `last_reported_at` plus a staleness window on read. It buys
nothing this does not, pays a write per summary per device, and reintroduces the FS01 lie in a form
that now needs a staleness rule to suppress.

## File inventory

- **Create:** `server/internal/rules/` — embedded catalogue loader, schema validation, cost analysis,
  binding/selector resolution, rollout state. Arch-lint component with `mayDependOn: [dbtx]`,
  mirroring `inventory` in [.go-arch-lint.yml](../../../server/.go-arch-lint.yml#L185-L189).
- **Create:** `server/internal/rules/catalogue/*.yaml` — the curated pack, `go:embed`-ed.
- **Create:** `server/internal/db/migrations/013_rules.{up,down}.sql` — the tenancy rework took
  010 through 012, so **013 is the next free number**; confirm at implementation time. Forced RLS on `tenant_id`, with `tenant_id`- and `organization_id`-leading
  indexes, mirroring
  [005_inventory](../../../server/internal/db/migrations/005_inventory.up.sql).
- **Modify:** [alert_rules.go](../../../server/internal/agentapi/alert_rules.go) — the
  `AlertRuleProvider` resolves catalogue + bindings + rollout instead of returning a hardcoded
  literal.
- **Modify:** [conn_coverage.go](../../../server/internal/agentapi/conn_coverage.go) — `Report` writes
  through to `rule_coverage_unsupported` on a state *change* (insert on newly unsupported, delete on
  newly active) and `Aggregate` reads the persisted rows for the unsupported count. The in-memory
  store stays the home of `active`; do not move it.
- **Modify:** [internal/lifecycle](../../../server/internal/lifecycle/) — device erasure drops the
  device's coverage rows.
- **Modify:** [.go-arch-lint.yml](../../../server/.go-arch-lint.yml), the scoped-SQL tenant-table gate
  ([scoped_sql_test.go](../../../server/internal/dbtx/scoped_sql_test.go)), and the migration rehearsal
  ([store_part4_test.go](../../../server/internal/db/store_part4_test.go)).
- **Docs:** [Database.md](../../../docs/Database.md), [Monitoring.md](../../../docs/Monitoring.md).

## Schema (from §7.4)

`rule_bindings` — `id, tenant_id, organization_id, rule_id, level, level_key, selector jsonb,
precedence, params jsonb, updated_at, updated_by`,
`UNIQUE (organization_id, rule_id, level, level_key, selector)`. `level` is one of
`device | site | organization`; `selector` is a **bounded** tag predicate; `params` holds
only values the rule declares tunable, validated against the rule's declared bounds **on write**.

`rule_rollout` — `tenant_id, organization_id, rule_id, enabled, canary_group, rollout_percent, kill,
stage_entered_at, updated_at, updated_by`, `PRIMARY KEY (organization_id, rule_id)`.

`rule_coverage_unsupported` — `tenant_id, organization_id, device_id, rule_id, since, updated_at`,
`PRIMARY KEY (device_id, rule_id)`, `ON DELETE CASCADE` from `devices`. Presence *is* the state, so
there is no `state` column to go stale; `since` is what makes "this estate has been 8 % blind to
`io-stalled` since March" answerable.

## Steps (TDD-first)

1. **Test first — the CI cost gate (B12, first clause):** a catalogue fixture containing a rule whose
   EF-B8 cost estimate exceeds the per-agent budget **fails the build**. Write the failing case
   first; a gate that has never been seen to fail is not a gate.
2. **Test first — catalogue schema:** an unknown field, a missing `group_by`, an out-of-vocabulary
   metric, a duplicate `(rule_id, version)`, and a mutated definition for an existing version are all
   rejected at load. Immutability per version is an assertion, not a convention.
3. **Test first — migration:** `013_rules` up/down round-trips; forced RLS denies cross-tenant read
   and write; `tenant_id`- and `organization_id`-leading indexes exist; the down path reverses
   cleanly. Extend the existing
   rehearsal rather than writing a second one.
4. **Test first — params validation (B12, second clause):** a binding whose `params` fall outside the
   rule's declared bounds is **rejected on write** with a typed error; a binding naming a param the
   rule does not declare tunable is rejected; a valid binding round-trips.
5. **Test first — selector resolution (B12, third clause):** one Contoso `disk-critical` rule, a
   binding at 95 selecting FS01's role and a binding at 90 selecting DAL-WS-012's, plus an org
   default → each device resolves to its own value, most-specific first. Add the **ambiguity** case:
   two selectors of equal specificity matching one device must resolve deterministically (define the
   tie-break and test it) rather than depending on row order.
6. **Test first (B12, fourth clause):** a rule pushed under a legacy metric name (`mem.used`,
   `disk.used`) still fires after the vitals rename — the end-to-end form of EF-B8's alias test,
   through the real provider.
7. **Test first — `PushAlertRules` delivery** stays authoritative-organization-scoped: Contoso's
   bindings never reach a Fabrikam agent, **including when both sit inside one tenant** — that is
   the case the database wall does not catch (the existing WS-19 property; do not regress it while
   replacing the provider).
8. **Test first — durable `unsupported` (owner decision above):** a device reporting `unsupported`
   for `io-stalled` survives a server restart and is still counted while that device is **offline**;
   the same device later reporting `active` **deletes** the row rather than storing an `active`
   state; a summary that changes nothing writes nothing (assert the write count, or "zero writes in
   steady state" is a claim rather than a property); deleting the device erases its rows; and the
   `active + unsupported + unknown == fleet` identity still holds across all of it. FS01 — offline
   three weeks, no coverage row — must read `unknown`, never `active`.
9. Implement; docs.

## Traps

- **Confirm the free migration number at implementation time.** 013 is free today (the tenancy
  rework took 010–012); a parallel micro-plan landing a migration first would silently collide.
  `ls server/internal/db/migrations/` before writing the file.
- **Never store an `active` coverage state.** The temptation is a `state` column mirroring the wire
  enum. That is the FS01 bug: a machine unplugged three weeks ago keeps asserting it is watched.
  Presence of a row means unsupported; absence means ask memory.
- The catalogue is `go:embed`-ed, so **a YAML typo is a build failure, not a runtime 500** — keep it
  that way. Do not add a "load from disk if present" fallback; that is the runtime-mutable
  definitions path this design rejects.
- `selector` and `params` are `jsonb` — bound them. An unbounded selector grammar is an authoring
  surface, which §4.2 excludes.
- RLS is **forced**; the tenant-table gate test enumerates scoped tables, so a new table that is not
  registered there passes tests while being readable cross-tenant. Register both tables.
- Rollout rows are `(organization_id, rule_id)`; a rule with no row is not "disabled" — define the default
  explicitly and test it, or a fresh org silently gets nothing.

## Out of scope

Stage advancement, throttling and the kill-switch *behaviour* (EF-B11 — this plan stores the state).
A rule-authoring UI (§4.2 forbids it). Retroactive evaluation (EF-B10).

## Reviewer checklist

- [ ] Cost gate proven to fail on an over-budget rule.
- [ ] Catalogue immutability per `(rule_id, version)` asserted.
- [ ] Migration 013 up/down + forced RLS + cross-tenant deny + rehearsal + tenant-table gate, for
      all three new tables.
- [ ] Params validated against declared bounds on write; undeclared params rejected.
- [ ] Selector resolution most-specific-wins **and** deterministic on ties (FS01 95 / DAL-WS-012 90).
- [ ] Legacy metric names still fire end to end.
- [ ] `unsupported` persists across a restart and while the device is offline; `active` is never
      stored; a no-change summary writes nothing; device deletion erases the rows.
- [ ] `go-arch-lint` clean with the new component.

## Verification

`cd server && go test ./internal/rules/... ./internal/db/... ./internal/agentapi/... ./internal/dbtx/... ./internal/lifecycle/...`,
`make lint`, `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row.
