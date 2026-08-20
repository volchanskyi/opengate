# EF-C4 — Aggregate platform metrics (O(rules)) and the Q12 alert-rate measurement

**Master plan:** `edge-first-telemetry-and-investigations.md` §6.7, §9.1.2, §14.2 (scope of the
non-action), D16, D35, D36, step 19.
**Acceptance criteria owned:** **C11**, **C15**.
**Dependencies:** EF-C2 (alerts exist), EF-C3 (incidents exist), EF-B8 (coverage is computed).
**Blocks:** the curated pack advancing **past canary** (EF-B11's rollout) — that decision needs this
plan's number.

## Context

Server-exported aggregate metrics on the existing `/metrics`, already scraped:

`opengate_alerts_open`, `opengate_alerts_created_total{rule_id}`,
`opengate_alerts_suppressed_total{reason}` (EF-C2 already ships this one),
`opengate_incidents_open{status}`, `opengate_rule_coverage{rule_id,state}`.

**O(rules), not O(devices).** These back the Grafana rule that catches a bad rollout — platform
monitoring, in the platform tool. This is **additive and independent of §14.2**: it is
fleet-aggregate, contributes no per-device series, and stands whatever that deferral resolves to.
The soak dashboard is untouched and this step is **not blocked** by §14.2.

## Q12 — and an honesty problem to put to the owner

§6.6's **0.2 alerts/device/day is an estimate**, and three decisions rest on it: the ~1.8 GB/year
evidence projection, the 500/org/h ceiling (set as ~12× a rate never observed), and one of §14.2's
revisit triggers. §9.1.2 turns it into a gate.

**The obstacle:** §2.2 measures a fleet of **one device**. A five-device canary on a one-device fleet
cannot produce a Contoso-scale rate, and a synthetic loadtest measures the fixture, not the fleet —
using it would be exactly the "assumed number cited as measured" failure D36 exists to stop.

**So the deliverable is a gate, not a fabricated number:** ship the counters that make the rate
observable, define the measurement procedure precisely, and hold the pack at canary until a real
population has produced one. If the owner wants an earlier signal, a synthetic soak is acceptable
**only** if reported as a harness figure with its rule pack and fixture stated — never as the fleet
rate.

| Measured rate | Contoso/day | Evidence/year | Consequence |
|---|---|---|---|
| **0.2/device/day** *(assumed)* | 1 000 | ~1.8 GB | Ceilings hold with ~12× headroom |
| 1/device/day | 5 000 | ~9 GB | Headroom ~2.4×; §14.3's retention sweep needed sooner |
| 5/device/day | 25 000 | ~45 GB | Org ceiling clips normal operation; triage unusable without tighter grouping |

## File inventory

- **Modify:** [metrics.go](../../../server/internal/metrics/metrics.go) — the four remaining counters
  and gauges (namespace `opengate`, as every existing metric).
- **Modify:** `server/internal/alerts/` — a bounded gauge refresh (see the trap).
- **Modify:** [deploy/grafana/provisioning/dashboards/](../../../deploy/grafana/provisioning/dashboards/)
  — a bad-rollout panel/alert over the new series. **Do not touch the Edge-Sentinel soak dashboard**
  (§14.2's scope of non-action).
- **Docs:** [Monitoring.md](../../../docs/infrastructure/Monitoring.md).

## Steps (TDD-first)

1. **Test first (C11):** with 1 device and with 5 000 devices the exported series count is
   **identical**; only rule count moves it. Assert against the real registry (`prometheus/testutil`),
   enumerating series — not by inspecting label declarations.
2. **Test first:** no metric in this set carries a `tenant_id`, `organization_id`, `device_id`, or
   any per-entity label.
   A single misplaced label turns O(rules) into O(rules × tenants) and this is the only test that
   would notice.
3. **Test first:** `alerts_created_total{rule_id}` uses the **rule id vocabulary** (bounded by the
   catalogue), so an agent-echoed unknown rule id cannot mint a new label value — same bounding the
   WS-19 breach path already applies to its `metric` label.
4. **Test first:** `incidents_open{status}` covers all four statuses including zero-valued ones (a
   missing series reads as "no data", not "none open"), and `rule_coverage{rule_id,state}` exports
   all three states per rule.
5. **Test first:** the gauge refresh is **bounded** — one aggregate query per interval, not one per
   scrape and never a full table scan per request. Assert query count, not wall time.
6. Wire the Grafana panel/alert for a bad rollout (a rising `alerts_created_total` rate for one
   `rule_id` alongside `alerts_suppressed_total`).
7. **Q12 (C15):** write the measurement procedure into the docs — which counters, over what window,
   how per-device rate is derived (`increase(alerts_created_total[24h]) / device_count`), and the
   decision that follows. Then take the measurement on the canary population and **stop with the
   number**; §9.1.2's four options go to the owner.

## Traps

- `alerts_open` and `incidents_open` are **gauges over a growing table**. Computing them per scrape
  is a full count on every Prometheus interval; refresh on a timer from one indexed aggregate.
- Prometheus counters must not be reset on restart-adjacent logic; let the process restart be the
  reset and let the query layer handle it (`increase()` tolerates it). Do not "fix" this with a
  persisted counter.
- §14.2 forbids adding **per-device** alert series to VictoriaMetrics. These are server-exported
  aggregates on `/metrics` — keep them that way, and leave the three existing WS-19 edge series
  untouched.
- Do not report a synthetic soak figure as the fleet rate. That is the exact failure D36 names.

## Out of scope

Any change to the three existing WS-19 per-device series (§14.2). Retention enforcement (§14.3 — a
tech-debt row at EF-Z1, not a sweep). Notifications (§4.2).

## Reviewer checklist

- [ ] Series count invariant to fleet size, proven by enumeration at two fleet sizes.
- [ ] No per-entity labels anywhere in the set; rule-id label bounded by the catalogue.
- [ ] Zero-valued statuses and all three coverage states exported.
- [ ] Gauge refresh bounded; query count asserted.
- [ ] Soak dashboard untouched; new panel is separate.
- [ ] Q12 procedure documented; number (or its explicit absence and the reason) reported to the owner.

## Verification

`cd server && go test ./internal/metrics/... ./internal/alerts/...`, `/precommit`, then the canary
measurement window.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row. The Q12 outcome goes into EF-Z1's evidence section.
