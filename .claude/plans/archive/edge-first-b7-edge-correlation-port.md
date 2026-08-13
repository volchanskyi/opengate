# EF-B7 — Move KS correlation to the edge; remove `/correlate` and the drag UX

**Master plan:** `edge-first-telemetry-and-investigations.md` §5.5, §6.5 (edge correlation), D14,
D33, step 10.
**Acceptance criteria owned:** **B7**.
**Dependencies:** none in WS-B (the port is self-contained; it consumes the local store, not the new
vitals).
**Blocks:** EF-C1 — evidence's `ranked[]` comes from this engine.

## Context

Correlation is an **on-demand central engine** today: it KS-ranks which dimensions broke pattern by
pulling VictoriaMetrics ([correlate.go](../../../server/internal/correlate/correlate.go), scoring
`0.4·KS + anomaly + magnitude` at [:35-39](../../../server/internal/correlate/correlate.go#L35-L39)),
reached through [`CorrelateDevice`](../../../server/internal/api/handlers_device_correlate.go#L16) and
driven by drag-to-select on the device chart
([DeviceMetrics.tsx:184](../../../web/src/features/devices/DeviceMetrics.tsx#L184)).

Both halves of that are being removed by decision: it **is** an on-demand telemetry pull (D2), and
after EF-B2 it would rank 60 s vitals instead of the detail that matters.

**On the edge it ranks what central never had.** When a rule fires on FS01, the agent ranks its own
full-resolution dimensions over the event window and ships the ranking inside the alert. The tech
opens the incident and it already says `disk.io_wait 0.91, backup-svc cpu 0.84` — no operator
action, no VM query.

## File inventory

- **Create:** `agent/crates/mesh-agent-core/src/correlate/` — KS statistic, the three-signal blend,
  ranking, bounded window fetch from the local store.
- **Modify:** [ml/store_sink.rs](../../../agent/crates/mesh-agent-core/src/ml/store_sink.rs) /
  [edge-tsdb](../../../agent/crates/edge-tsdb/) read path — a bounded range read for the focus and
  baseline windows (read-only; `edge-tsdb` itself is unchanged by this program).
- **Delete:** [server/internal/correlate/](../../../server/internal/correlate/) (10 files incl. tests),
  [handlers_device_correlate.go](../../../server/internal/api/handlers_device_correlate.go) and its test.
- **Modify:** [api/openapi.yaml](../../../api/openapi.yaml) — remove
  `/api/v1/devices/{id}/correlate` ([:1724](../../../api/openapi.yaml#L1724)) **outright**, not
  deprecated (D33: no external consumers, and its only caller retires in the same step); regen Go +
  TS.
- **Modify:** [api.go](../../../server/internal/api/api.go) — drop the engine from `Server`
  construction; [main.go](../../../server/cmd/meshserver/main.go) — drop the wiring.
- **Modify:** [DeviceMetrics.tsx](../../../web/src/features/devices/DeviceMetrics.tsx),
  [device-store.ts](../../../web/src/features/devices/state/device-store.ts) — remove the drag
  selection, the `correlate` action, the freeze-poll behaviour and the caption at
  [:260](../../../web/src/features/devices/DeviceMetrics.tsx#L260).
- **Create:** a shared fixture used by **both** the Go reference test and the Rust port.
- **Docs:** [API-Reference.md](../../../docs/API-Reference.md),
  [Architecture.md](../../../docs/Architecture.md), [Monitoring.md](../../../docs/Monitoring.md).

## Steps (TDD-first)

1. **Test first (B7) — port-equivalence:** freeze a fixture (baseline window, focus window, several
   dimensions, one deliberately broken pattern) and capture the **current Go engine's** ranking and
   scores as committed expected values, *before* deleting anything. The Rust port must reproduce the
   same order and the same scores within a stated float tolerance. Without this capture the Go
   engine's behaviour is unrecoverable after deletion.
2. **Test first:** the synthetic broken-pattern dimension ranks **first** — the property that
   actually matters, asserted independently of the score-for-score comparison.
3. **Test first — KS statistic:** identical samples → 0; disjoint supports → 1; single-point
   windows; empty focus or baseline → no ranking rather than a divide-by-zero or a NaN (§9.2: never
   propagate NaN).
4. Port the KS statistic and the blend. Weights are **behaviour**, not tuning knobs to revisit here —
   any change to them invalidates step 1.
5. **Test first:** ranking is bounded — a cap on dimensions considered, a cap on points read per
   window, and a wall-clock/CPU bound so a fire during a storm cannot pin the agent (Q5/Q6 apply).
6. Delete the Go package, the handler, the route and the client code **in the same commit** as the
   port lands, so no dead central path survives a release.
7. Regenerate OpenAPI → Go + TS; assert the generated client no longer exposes the operation.
8. Docs: describe what the system does now (ranking arrives with the alert). Per
   [docs-live-state.md](../../rules/docs-live-state.md), **do not** narrate the removal.

## Traps

- Deleting `internal/correlate` also removes its mutation-test files; check the mutation floors still
  hold for the packages that remain (the deleted files were carrying score in `make mutate-go`).
- The edge store read must be **bounded and snapshot-based** (the local TSDB offers MVCC snapshot
  reads) — a correlation running while the sampler writes must not block ingestion.
- `f64` ordering: the Go engine sorts by `KSStatistic` then score
  ([:193](../../../server/internal/correlate/correlate.go#L193)); reproduce the **tie-break**, not just
  the primary key, or equivalence passes on the fixture and diverges in production.
- Removing an OpenAPI path changes the generated server interface — the compile error surface is
  wide; do it in one pass and regenerate both languages together.

## Out of scope

Evidence composition and the alert payload (EF-C1). Any replacement for the drag UX — none is
planned; ranking arrives with the alert (accepted loss #5).

## Reviewer checklist

- [ ] Go reference ranking captured as a committed fixture **before** deletion.
- [ ] Rust port matches order, scores (stated tolerance) **and** tie-break.
- [ ] Degenerate windows never produce NaN; ranking is bounded in time and points.
- [ ] `/correlate` gone from OpenAPI, Go, TS, and the web client in one commit; no dead route.
- [ ] Mutation floors still met after the Go deletions.
- [ ] Docs describe current behaviour only.

## Verification

`cd agent && cargo test -p mesh-agent-core`, `cd server && go test ./...`, `cd web && npm test`,
`make golden`, `make mutate-go`, `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row.
