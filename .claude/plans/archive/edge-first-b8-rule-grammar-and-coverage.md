# EF-B8 — Rule grammar extension, metric-name aliasing, and explicit coverage

**Master plan:** `edge-first-telemetry-and-investigations.md` §6.5 (grammar, vocabulary, coverage),
§7.6 (I7, I8), D31, E6, step 11.
**Acceptance criteria owned:** **B8**.
**Dependencies:** EF-B2 (canonical vitals names must exist), EF-B4 + EF-B5 (their vitals join the
vocabulary; without `disk.await_ms` the DAL-WS-012 wear-out case is collectable but not alertable).
**Blocks:** EF-B9 (the CI cost-bound analysis analyses *this* grammar), EF-B10, EF-B11.

## Context

The shipped rule is a four-field threshold
([control.rs:41](../../../agent/crates/mesh-protocol/src/control.rs#L41)) evaluated by a
Clear→Pending→Firing state machine
([evaluator.rs](../../../agent/crates/mesh-agent-core/src/alerts/evaluator.rs)) over three metric names:
`cpu.total`, `mem.used`, `disk.used`
([alert_rules.go:63-65](../../../server/internal/agentapi/alert_rules.go#L63-L65)).

Two things are wrong with that as the base for this program: the vitals are named
`mem.used_percent` / `disk.used_percent`, and the grammar cannot express any of the shapes the
curated pack needs.

**Rules are data in a bounded grammar — never shipped code (I7, D11).** An RMM agent executing
server-supplied code is a supply-chain weapon aimed at every customer estate. Everything here must
stay statically analysable and cost-boundable before it reaches an endpoint.

## Design (fixed)

**Grammar additions:** rate-of-change; window aggregate (`max` / `mean` over N); cross-dimension
conjunction. Existing comparators, `sustain_secs` and hysteresis `clear` are retained unchanged.

**Vocabulary:** the **vitals names are canonical**; `mem.used` and `disk.used` are accepted as
**aliases** so rules already pushed to the fleet keep firing across the upgrade. The vocabulary
extends to the full vitals set including `disk.await_ms` and `disk.queue_depth`. A rule naming an
unknown metric **never fires and is counted `unsupported`** — never silently skipped.

**Coverage (I8):** per rule, every device is exactly one of `active` (evaluating), `unsupported`
(the metric or predicate cannot be evaluated on this host — no PSI, no diskstats, unknown metric) or
`unknown` (never reported, e.g. offline). `active + unsupported + unknown == fleet size`, always.

## Gap this plan closes (flag to the owner)

§7.5 defines only `AgentAlert` and `AlertEvidence` on the wire — **coverage has no transport**. This
plan adds an additive `rule_coverage[]` to `AgentHealthSummary`, exactly as WS-19's `breaches` rides
it today ([control.rs:231](../../../agent/crates/mesh-protocol/src/control.rs#L231)). Cheapest correct
option, precedented, no new message type.

Likewise §7.4's migrations carry no `rule_coverage` table. **Recommendation: no table.** Coverage is
a liveness view: hold reported states in memory per connected agent and derive `unknown` as
`fleet size − reported`, which is exactly right after a server restart. If the owner wants coverage
to survive a restart, it becomes a column set in `013_rules` (EF-B9) — say so before EF-B9 lands.

## File inventory

- **Modify:** [control.rs](../../../agent/crates/mesh-protocol/src/control.rs) — grammar fields on
  `ThresholdRule` (additive, defaulted), `RuleCoverage` entry, `rule_coverage[]` on
  `AgentHealthSummary`.
- **Modify:** [evaluator.rs](../../../agent/crates/mesh-agent-core/src/alerts/evaluator.rs) — the new
  predicate kinds, alias resolution, support classification, and a **cost estimate** per rule that
  EF-B9's CI gate consumes.
- **Modify:** [control_encode.go](../../../server/internal/protocol/control_encode.go) — per-field
  encoder arms; bump `controlFieldCount` (**86 today** — read it at implementation time).
- **Modify:** [alert_rules.go](../../../server/internal/agentapi/alert_rules.go) — defaults renamed to
  canonical names.
- **Create:** `server/internal/agentapi/conn_coverage.go` + an in-memory coverage store.
- **Create/Modify:** [testdata/golden/](../../../testdata/golden/) — a **forward** fixture
  (`control_agent_health_summary*`) carrying coverage, **and** the **reverse** fixture
  `go_control_push_alert_rules.bin`, which changes the moment `ThresholdRule` grows a field. That
  reverse golden is named in the
  [completeness guard](../../../server/internal/agentapi/golden_completeness_test.go) under
  `SendPushAlertRules`; regenerate it or the agent's decoder rejects the new rule shape.
- **Docs:** [Wire-Protocol.md](../../../docs/architecture/Wire-Protocol.md), [Monitoring.md](../../../docs/infrastructure/Monitoring.md).

## Steps (TDD-first)

1. **Test first — aliases (B12's alias half, proven here):** a rule naming `mem.used` fires against
   `mem.used_percent`; `disk.used` against `disk.used_percent`; both resolve to the canonical name in
   the emitted breach so nothing downstream sees two names for one thing.
2. **Test first — unknown metric:** never fires, classified `unsupported`, and is **counted** — assert
   the classification, not just the absence of a breach.
3. **Test first — each new predicate,** positive and negative: rate-of-change (rising and falling,
   and a flat series that must not fire); window aggregate `max`/`mean` over N with a partial window
   (fewer than N samples → not enough data, not a fire); conjunction where each side alone is false
   and both together are true, plus the case where one side is `unsupported` → the whole rule is
   `unsupported`, not false.
4. **Test first — hysteresis and sustain still hold** for the new predicates (a rate rule must not
   flap either); the existing flap-suppression tests are the template.
5. **Test first — cost bound:** each predicate reports a bounded evaluation cost (samples touched ×
   window), monotone in the window size, so EF-B9's CI gate has something exact to compare against a
   budget. A predicate whose cost cannot be computed statically must be **impossible to express**.
6. **Test first — coverage on the wire:** golden round-trip both directions for a summary carrying
   `rule_coverage[]`; a legacy agent that omits the field decodes fine (forward-compat).
7. **Test first (B8) — the fleet identity:** with a fleet of N devices, some connected, some
   reporting `unsupported`, some never seen, the server's aggregate satisfies
   `active + unsupported + unknown == N`, and a device that disconnects moves from `active` to
   `unknown` rather than vanishing.
8. Implement; regenerate goldens; docs.

## Traps

- **`controlFieldCount` collision.** EF-C1 also adds wire fields. Whoever lands second rebases and
  re-runs the per-field differential encoder test — the hand-written encoder
  ([ADR-060](../../../docs/adr/ADR-060-control-message-hand-written-encoder.md)) is byte-identity
  guarded, so a missed arm fails loudly, but only if the goldens are regenerated after the rebase,
  not before.
- Rust decodes an internally-tagged enum whose variant fields are **required**
  ([ADR-063](../../../docs/adr/ADR-063-server-to-agent-control-message-completeness.md)): any new
  server→agent field needs `#[serde(default)]` agent-side or a server that always emits it. Getting
  this wrong drops the agent's control stream — the exact defect ADR-063 documents.
- **`unsupported` must be a first-class state, not an error path.** The temptation is to treat "no
  PSI" as a failed evaluation; that produces `unknown` and hides a permanent platform gap behind a
  transient-looking label.
- Alias resolution belongs in **one** place. Two mappings (agent and server) drift; put the canonical
  map in the protocol crate and have both sides use it.

## Out of scope

The catalogue, bindings, selectors and the CI gate (EF-B9). Rollout stages and the kill switch
(EF-B11). The Prometheus export of coverage (EF-C4 owns `opengate_rule_coverage`).

## Reviewer checklist

- [ ] Canonical names everywhere; both legacy aliases proven to still fire.
- [ ] Every new predicate has a positive, a negative, and a not-enough-data case.
- [ ] `unsupported` propagates through conjunction; unknown metric counted, never skipped.
- [ ] Cost is statically computable for every expressible predicate.
- [ ] Coverage rides the summary additively; goldens both directions; forward-compat proven.
- [ ] `active + unsupported + unknown == fleet size` holds across connect/disconnect.

## Verification

`cd agent && cargo test -p mesh-agent-core -p mesh-protocol`,
`cd server && go test ./internal/protocol/... ./internal/agentapi/...`, `make golden`, `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row.
