# EF-B6 — System-event rule pack, the 24 h per-service error rule, and the edge alert sink

**Master plan:** `edge-first-telemetry-and-investigations.md` §5.3 (D), §6.4, §9.2 (log privacy),
E24, step 9.
**Acceptance criteria owned:** **B6**.
**Dependencies:** none (disjoint files from EF-B2/B4/B5 — may run in parallel with them).
**Blocks:** EF-C1 (which connects this plan's alert sink to the wire).

## Context — verified, and one thing the master plan glosses

The host log reader exists ([host_logs.rs](../../agent/crates/mesh-agent/src/host_logs.rs)):
`journalctl -o json` on Linux, normalized into `LogEntry`, with
[`redact_entries`](../../agent/crates/mesh-agent/src/host_logs.rs) applying edge redaction
(ADR-049).

**It is a bounded on-demand read, not a stream.** `collect_host_logs` shells out per call with
`--since`; nothing tails. So the event pack is a **periodic bounded poll with a watermark**, and
the two consequences must be designed rather than discovered:

1. **Exactly-once firing** (B6) needs a dedup key — `(source, unit/provider, event id, timestamp)` —
   and a cursor that survives a poll cycle, because overlapping `--since` windows will re-present the
   same record.
2. **A burst can exceed `MAX_HOST_LINES` between polls.** Events lost that way must be **counted**,
   never silently missed — the WS-A lesson applied to the log path.

## The pack (fixed)

Four rules, all Linux, all read from `journalctl -o json`:

| Signal | Meaning |
|---|---|
| `hung_task` | task blocked > 120 s |
| OOM kill | the kernel killed a process to reclaim memory |
| ATA reset | disk stopped responding, bus reset |
| thermal throttle | the CPU clocked down under thermal load |

*Extension point:* a Windows agent would add an Event Log reader plus its own rows (4101 TDR, 129
storport, 2004 Resource Exhaustion Detector). The pack is a list of (source, matcher, meaning) rows,
so extending it needs no grammar change — and a new reader inherits the injection discipline the
journald argv path already has.

Plus the second stated signal class: **repeated errors from one service over 24 h** — a rolling
per-service error count evaluated locally.

## File inventory

- **Create:** `agent/crates/mesh-agent-core/src/alerts/event.rs` — event predicate matching, dedup
  key, rolling per-service error counter. **Owned by this plan**; EF-B8 owns the numeric grammar in
  [evaluator.rs](../../agent/crates/mesh-agent-core/src/alerts/evaluator.rs), so the two plans do not
  collide in one file.
- **Create:** `agent/crates/mesh-agent-core/src/alerts/sink.rs` — the in-process `AlertSink`: a
  **bounded** queue with oldest-dropped-and-counted semantics (E24) and the per-device **20 alerts/h**
  ceiling (§6.6, D28), counted locally and reported. Every edge alert producer writes here; EF-C1
  attaches the transport.
- **Modify:** [alerts/mod.rs](../../agent/crates/mesh-agent-core/src/alerts/mod.rs).
- **Modify:** [main.rs](../../agent/crates/mesh-agent/src/main.rs) — schedule the poll task
  (bounded, `spawn_blocking`, watermarked), wire the sink.
- **Create:** fixture log corpora for both platforms, including a non-matching near-miss per rule.
- **Docs:** [Monitoring.md](../../docs/Monitoring.md).

## Steps (TDD-first)

1. **Test first (B6, positive):** each of the seven signals fires **exactly once** for one matching
   record in a fixture corpus.
2. **Test first (B6, negative):** a near-miss per rule fires **nothing** — event 4102 next to 4101, a
   storport informational record next to 129, an `INFO`-level `hung_task`-shaped line, a
   thermal-*recovery* message. The negative half is where an event pack actually earns its keep;
   without it, a rule that matches on substring alone looks green.
3. **Test first — replay:** the same record presented twice across two overlapping polls fires once.
   Then the same record after the cursor advances past it fires **not at all**.
4. **Test first — the rolling counter:** N errors from one service inside 24 h fires once; the
   window slides (an error ageing out lowers the count); a second service is counted separately; the
   counter is bounded in memory per service and the service set itself is capped.
5. **Test first — burst loss is counted:** a poll whose window exceeds `MAX_HOST_LINES` increments a
   counted "events not observed" figure that rides the next summary, rather than passing silently.
6. **Test first — the sink (E24):** an offline agent fills the bounded queue; the **oldest** entries
   drop and the drop count is preserved and reported; the per-device 20/h ceiling suppresses excess
   **with a count**, never silently.
7. **Test first — privacy (feeds C10/E12):** every log-derived field an alert can carry goes through
   `redact_entries`/`redact_log_line` before it reaches the sink. Assert with a corpus containing an
   AWS-key-shaped token, a bearer token, an email address, and a password-looking assignment — none
   may appear in the sink's output.
8. Implement matching, cursor, counter and sink. Rule *instances* are injected (the catalogue
   delivers them in EF-B9); tests inject fixtures, so this plan is independently reviewable.
9. Wire the periodic poll in `mesh-agent`; docs.

## Traps

- **Never shell out per record.** One bounded call per poll; the readers already cap output.
- The journald path pushes every value down as a discrete argv token, with no shell, so
  rule-supplied text is inert by construction. Keep it that way: any new push-down argument is
  another argv token, never a joined command string.
- `mesh-agent` is a binary crate: `anyhow` there, `thiserror` in `mesh-agent-core`, no `unwrap()` in
  either.
- A poll must not run while the device is in **maintenance mode** — the existing `MaintenanceGate`
  already suppresses collectors and alert evaluation; join it rather than adding a second switch
  (E5).
- The sink's ceiling is per **device**; the per-**org** 500/h ceiling is the server's (EF-C2). Do not
  implement half of the organization ceiling at the edge.

## Out of scope

Alert transport and evidence composition (EF-C1). Rule *delivery* and bindings (EF-B9). Numeric
threshold grammar (EF-B8). Any notification (§4.2 forbids it).

## Reviewer checklist

- [ ] Every rule has a positive **and** a negative fixture.
- [ ] Replay across overlapping polls proven idempotent; cursor advance proven.
- [ ] Burst loss counted, not silent.
- [ ] Sink is bounded, oldest-dropped-with-count, 20/h per device counted.
- [ ] Redaction proven with a hostile corpus, before anything reaches the sink.
- [ ] Maintenance mode suppresses the poll.

## Verification

`cd agent && cargo test -p mesh-agent-core -p mesh-agent`, `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row.
