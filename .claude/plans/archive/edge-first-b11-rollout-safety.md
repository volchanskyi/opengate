# EF-B11 — Rollout safety: staged gates, per-agent throttle, kill switch

**Master plan:** `edge-first-telemetry-and-investigations.md` §6.5 (rollout safety), Q6, E10, D24,
step 14.
**Acceptance criteria owned:** **B13**.
**Dependencies:** EF-B9 (rollout state lives in `rule_rollout`). **Soft:** EF-C2 supplies the live
per-organization ceiling signal — build against an injected signals port and wire it when that lands.
**Blocks:** the fleet rollout of the curated pack past canary, which additionally waits on **Q12**
(EF-C4). This plan builds the machinery, not the rollout.

## Context — the one High-impact risk in the register

"A bad curated rule degrades 5 000 machines" is the program's only High-impact row. Its mitigations
are a declarative grammar (EF-B8), static cost analysis (EF-B9), and this plan: canary, throttle,
kill switch.

## Design (fixed)

| Stage | Population | Hold | Advance gate |
|---|---|---|---|
| Canary | **max(5 devices, 1 %)** | **1 h** | No per-organization ceiling breach, no agent budget throttle trip, no rule-evaluation error |
| Staged | **10 %** | **6 h** | Same gates |
| Full | **100 %** | — | — |

**Any gate failing halts and reverts to the previous stage automatically.** It never advances on a
timer alone — the hold is a *minimum*, not a trigger.

`kill` is a **row flip**, effective on the agent's next reconnect and on the next rule push,
whichever is sooner. No server deploy is on the critical path for stopping a rule that is degrading
customer machines.

## File inventory

- **Modify:** `server/internal/rules/` — stage machine, population selection, gate evaluation,
  revert; a `GateSignals` port so the machine is testable without live counters.
- **Modify:** [alert_rules.go](../../../server/internal/agentapi/alert_rules.go) — the pushed ruleset
  respects stage membership and `kill`.
- **Modify:** [conn.go](../../../server/internal/agentapi/conn.go) — the reconnect path re-resolves
  rules, which is what makes `kill` effective without a deploy.
- **Modify:** the agent's evaluator budget — per-rule CPU/IO accounting with a **hard** throttle
  (Q6), reported so the server's gate can see a trip.
- **Docs:** [Monitoring.md](../../../docs/infrastructure/Monitoring.md).

## Steps (TDD-first)

1. **Test first — population selection is stable:** a device's stage membership is a deterministic
   function of `(device_id, rule_id, rollout_percent)`, so raising the percent only **adds** devices
   and a device never oscillates in and out between evaluations. Assert both properties; an unstable
   hash makes every other test in this plan flaky and would flap rules across a customer estate.
2. **Test first — canary floor:** an org with 200 devices gets `max(5, 1 %) = 5`; an org with 2 000
   gets 20; an org with 3 devices gets 3, not 5 (the floor cannot exceed the fleet).
3. **Test first (B13, first clause):** a canary stage whose org **ceiling trips** does not advance
   when the 1 h hold expires, and **reverts** to the previous stage. Same for an **agent budget
   throttle trip** and for a **rule-evaluation error**. Three separate tests — a single combined one
   hides which gate is actually wired.
4. **Test first — the hold is a minimum:** all gates green but the hold not yet elapsed → no advance.
   Gates green and hold elapsed → advance exactly one stage, never two.
5. **Test first — revert is idempotent and bounded:** a rule already at canary that trips a gate does
   not revert below canary (it stops), and a flapping signal cannot walk the stage up and down
   repeatedly within one hold window.
6. **Test first (B13, second clause):** `kill = true` stops evaluation on the agent's **next
   reconnect** with no server deploy — assert through the real reconnect path, and separately that a
   connected agent stops at the next rule push. Both, because "whichever is sooner" is the contract.
7. **Test first — the agent budget:** a rule exceeding its per-agent CPU/IO budget is **throttled
   hard** (evaluation stops for that rule, not for the whole evaluator), the trip is counted, and the
   count reaches the server so the gate can act on it.
8. Implement; docs.

## Traps

- **Stage state is per `(organization_id, rule_id)`** — per **customer**, not per tenant. A
  tenant-wide stage would push one customer's bad canary
  onto every tenant. The primary key in `013_rules` already says so — keep every read scoped.
- The advance gate needs a signal that a ceiling was hit; until EF-C2 exists, that port is faked in
  tests. Do **not** approximate it with a local guess — an advance gate reading a made-up signal is
  worse than no gate, because it looks like protection.
- `kill` must not require the rule to be *pushable* — a rule already resident on an offline agent is
  stopped when that agent reconnects, which is the point. Test the offline→reconnect path, not just
  the connected one.
- The throttle is **per rule**, not per agent-process; one expensive rule must not silence a cheap
  one, or a bad rollout degrades detection everywhere while looking contained.
- Do not add a notification when a stage reverts (§4.2). The aggregate counters (EF-C4) are how a
  bad rollout becomes visible.

## Out of scope

The organization ceiling's enforcement (EF-C2) and its counter (EF-C4). Deciding when to roll the pack out —
that waits on Q12 and is the owner's call.

## Reviewer checklist

- [ ] Membership deterministic, monotone in `rollout_percent`, no oscillation.
- [ ] Canary floor `max(5, 1 %)` bounded by fleet size.
- [ ] Three separate gate-trip tests; each reverts and none advances on the timer.
- [ ] Advance is exactly one stage; revert is bounded and idempotent.
- [ ] `kill` proven on both the reconnect path and the next-push path, with no deploy.
- [ ] Per-rule hard throttle, counted, and the count reaches the server.

## Verification

`cd server && go test ./internal/rules/... ./internal/agentapi/...`,
`cd agent && cargo test -p mesh-agent-core`, `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row.
