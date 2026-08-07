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

Definitions in Postgres would move the program's highest-impact gate out of CI — cost-bounding a
predicate before it reaches 5 000 customer endpoints is mandatory and free in CI, and a validator
bug in a runtime API path is a production incident. Definitions with no DB layer cannot express a
kill switch, canary state or coverage (the wall Netdata hit before adding DYNCFG).

**Bindings resolve down the tenancy levels.** `(organization_id, rule_id)` alone gives Contoso one threshold for
its whole estate, while FS01 wants `disk-critical` at 95 and DAL-WS-012 wants 90. Resolution is
**device → site → organization → tenant → embedded default**, and the ordering is not defined here:
it is [`internal/settings`](../../server/internal/settings/settings.go), shipped by the tenancy
rework. The walk and the tie-break live in one place; the *values* stay in `rule_bindings` where the
rule that declares them can validate them against its own bounds. So the file-server-at-95 case
needs no special machinery: put the file servers in a site. The kill switch is the one setting that
does not resolve this way — a customer-wide stop must not be undone by a value on one machine, so it
reads the ladder the other way up (`settings.BroadestWins`) and lives on `rule_rollout`.
A binding also carries an optional **cross-cutting device-tag selector** with an explicit operator-set
`precedence` breaking ties between two tag selectors at the same level — never an invisible tie-break.
Across levels the narrower level always wins.

## File inventory

- **Create:** `server/internal/rules/` — embedded catalogue loader, schema validation, cost analysis,
  binding/selector resolution, rollout state. Arch-lint component with `mayDependOn: [dbtx]`,
  mirroring `inventory` in [.go-arch-lint.yml](../../server/.go-arch-lint.yml#L185-L189).
- **Create:** `server/internal/rules/catalogue/*.yaml` — the curated pack, `go:embed`-ed.
- **Create:** `server/internal/db/migrations/013_rules.{up,down}.sql` — the tenancy rework took
  010 through 012, so **013 is the next free number**; confirm at implementation time. Forced RLS on `tenant_id`, with `tenant_id`- and `organization_id`-leading
  indexes, mirroring
  [005_inventory](../../server/internal/db/migrations/005_inventory.up.sql).
- **Modify:** [alert_rules.go](../../server/internal/agentapi/alert_rules.go) — the
  `AlertRuleProvider` resolves catalogue + bindings + rollout instead of returning a hardcoded
  literal.
- **Modify:** [.go-arch-lint.yml](../../server/.go-arch-lint.yml), the scoped-SQL tenant-table gate
  ([scoped_sql_test.go](../../server/internal/dbtx/scoped_sql_test.go)), and the migration rehearsal
  ([store_part4_test.go](../../server/internal/db/store_part4_test.go)).
- **Docs:** [Database.md](../../docs/Database.md), [Monitoring.md](../../docs/Monitoring.md).

## Schema (from §7.4)

`rule_bindings` — `id, tenant_id, organization_id, rule_id, level, level_key, selector jsonb,
precedence, params jsonb, updated_at, updated_by`,
`UNIQUE (organization_id, rule_id, level, level_key, selector)`. `level` is one of
`device | site | organization`; `selector` is a **bounded** tag predicate; `params` holds
only values the rule declares tunable, validated against the rule's declared bounds **on write**.

`rule_rollout` — `tenant_id, organization_id, rule_id, enabled, canary_group, rollout_percent, kill,
stage_entered_at, updated_at, updated_by`, `PRIMARY KEY (organization_id, rule_id)`.

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
8. Implement; docs.

## Traps

- **Confirm the free migration number at implementation time.** 010 is free today; a parallel
  micro-plan landing a migration first would silently collide. `ls server/internal/db/migrations/`
  before writing the file.
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
- [ ] Migration 010 up/down + forced RLS + cross-tenant deny + rehearsal + tenant-table gate.
- [ ] Params validated against declared bounds on write; undeclared params rejected.
- [ ] Selector resolution most-specific-wins **and** deterministic on ties (FS01 95 / DAL-WS-012 90).
- [ ] Legacy metric names still fire end to end.
- [ ] `go-arch-lint` clean with the new component.

## Verification

`cd server && go test ./internal/rules/... ./internal/db/... ./internal/agentapi/... ./internal/dbtx/...`,
`make lint`, `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row.
