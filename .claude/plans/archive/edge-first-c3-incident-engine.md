# EF-C3 — The incident engine: two-axis grouping, lifecycle, auto-resolve

**Master plan:** `edge-first-telemetry-and-investigations.md` §6.6 (incident engine), §7.3,
D18, D19, D27, D30, E5, E8, E15, E16, E17, step 17.
**Acceptance criteria owned:** **C2**, **C3**, **C4**, **C5**, **C6**.
**Dependencies:** EF-C2 (store + partial unique index). **Co-verified with EF-B10:** the "one
incident per `(rule, scope)`" half of B9 is asserted here over EF-B10's `backfilled` fixture.
**Blocks:** EF-C4, EF-C5.

## Design (fixed)

An `Alert` joins an open `Incident` when **all** hold: same `organization_id`; same `rule_id` (**not**
`rule_version` — a rule upgrade must not fork a live incident); scope-compatible per the rule's
`group_by`; and `now − incident.last_seen <= rule.group_window`.

| Rule shape | `group_by` | `group_window` | RMM effect |
|---|---|---|---|
| Fleet event | `organization` / `site` | 30 min | Contoso's driver rollout: 312 alerts → **1** incident, 40 devices |
| Slow burn | `device` | 24 h | FS01 disk filling: re-fires fold into one incident |
| **Recurrence** | `device` | 7 d | WS-4471: 30 daily freezes → **1** incident, "30 occurrences" |

The recurrence row is load-bearing: for WS-4471 **the pattern is the diagnosis**, and only grouping
across *time* makes it visible.

**`organization` is the broadest scope a rule may declare.** Grouping never crosses a customer:
at the tenant, Contoso's driver rollout and Fabrikam's unrelated outage would land in one room
with no correct assignee. Add a test that a rule attempting to group above the organization is
rejected — an unreachable-by-convention rule is how this comes back.

**Lifecycle:** `new → acknowledged → investigating → resolved`. An incident in `new` **is** the
triage queue — which is why no separate promotion entity exists.

**`reopen_window` defaults to the rule's own `group_window`** (D27), overridable per rule. This is
definitional: an incident must stay open exactly as long as a new alert could still fold into it,
otherwise auto-resolve and grouping disagree and WS-4471's 30 freezes fragment into 30 incidents.

**Cause code — closed set, required on manual resolution:** `resolved_self | fixed_by_tech |
hardware_fault | expected_load | false_positive | duplicate | wont_fix`. `false_positive` is
load-bearing — it is the feedback channel that says which curated rule needs its threshold moved.

## File inventory

- **Modify:** `server/internal/alerts/` — the incident engine (fold, transitions, auto-resolve
  sweep), or a sibling package if arch-lint prefers it; keep `mayDependOn: [dbtx]`.
- **Modify:** `server/internal/agentapi/conn_alerts.go` — ingest calls the fold.
- **Create:** fixtures for Contoso (312/40/29 min), WS-4471 (30 alerts over 30 d), FS01 (6 days).
- **Docs:** [Monitoring.md](../../../docs/infrastructure/Monitoring.md).

## Steps (TDD-first)

1. **Test first (C2):** the Contoso rollout fixture — 312 alerts across 40 devices in 29 minutes
   under one `group_by: organization`, `group_window: 30 min` rule → **exactly 1** incident,
   `device_count = 40`, `occurrences = 312`.
2. **Test first (C3):** WS-4471 — 30 daily alerts over 30 days under a `group_by: device`,
   `group_window: 7 d` recurrence rule → **1** incident, `occurrences = 30`. Then the control case:
   the **same** alerts under a 30 min window produce 30 incidents, proving the window is what does
   the work.
3. **Test first (C4):** FS01 re-fires over 6 days under a 24 h slow-burn window fold into one
   incident; a 25 h gap opens a second — the boundary is the assertion.
4. **Test first — fold is race-safe:** two alerts arriving concurrently for the same key produce one
   incident, driven through the real partial unique index (`WHERE status <> 'resolved'`) with a
   concurrent write, not a mutex in Go.
5. **Test first (E16):** a rule upgraded from v1 to v2 while an incident is open does **not** fork
   it — grouping keys on `rule_id`.
6. **Test first (E15):** two different rules firing on one underlying condition produce **distinct**
   incidents unless `group_by` says otherwise.
7. **Test first (E8):** `backfilled` alerts fold by **real event time** into **one** incident per
   `(rule, scope)` — a retro scan of 30 historical freezes must not produce 30 live incidents. Use
   EF-B10's fixture.
8. **Test first (C5):** every legal transition succeeds; every illegal one (`resolved → investigating`
   without reopen, skipping straight to `resolved` without a cause code, an unknown status) is
   **rejected** with a typed error; each transition appends an `incident_events` row.
9. **Test first (C6):** auto-resolve fires only after `reopen_window` elapses with no alert; one
   alert inside the window resets it; and a **device in maintenance keeps its open incident** (E5) —
   an incident open *before* maintenance does not auto-resolve.
10. **Test first (E17):** low-severity **observations** fold into an incident only on cross-device
    co-occurrence — a single host's sub-threshold observation opens nothing.
11. Implement fold, transitions, the auto-resolve sweep, and `occurrences`/`device_count`/`first_seen`
    /`last_seen` maintenance; docs.

## Traps

- **`device_count` is distinct devices, `occurrences` is alerts.** Contoso's fixture (40 vs 312) is
  exactly the case that catches conflating them — keep both assertions in the same test.
- The auto-resolve sweep is time-driven; drive it with an **injected clock**, never `time.Sleep` (the
  test-determinism rule, and a 7 d recurrence window is otherwise untestable).
- `reopen_window` **defaults from** `group_window`. Do not copy the value into the incident row at
  open time unless the rule's window changing mid-incident should not apply — decide, state it in the
  code comment, and test the chosen behaviour.
- Scope key must be **derived server-side** from the rule's `group_by` and the alert's own fields.
  Accepting an agent-supplied grouping key is the crafted-key attack EF-C2's C7 test covers — keep
  the derivation on this side.
- An incident spans devices; erasure (EF-C2, C8) mutates `device_count`. The engine must tolerate a
  device disappearing under an open incident without recomputing from deleted rows.
- Do not add notifications on any transition (§4.2).

## Out of scope

Assignment/comment **API surface** (EF-C5 — this plan owns the state machine and the event rows).
Aggregate counters (EF-C4). Any remediation action from the room (§4.2).

## Reviewer checklist

- [ ] C2/C3/C4 fixtures assert incident count, `device_count` **and** `occurrences`.
- [ ] C3 has the control case proving the window drives the grouping.
- [ ] Concurrent fold proven race-safe through the real index.
- [ ] Rule upgrade does not fork; two rules do not merge.
- [ ] Backfilled alerts fold by event time into one incident.
- [ ] Illegal transitions rejected; every transition writes an `incident_events` row.
- [ ] Auto-resolve is clock-injected; maintenance suppresses it.

## Verification

`cd server && go test ./internal/alerts/... ./internal/agentapi/...` (testpg-backed), `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row.
