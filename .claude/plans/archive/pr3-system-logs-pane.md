# PR-3 — System Logs pane (source=host) + level/unit filters + repoint drill + icon buttons + remove Recent Activity

Micro-plan of [system-logs-and-central-host-metrics.md](system-logs-and-central-host-metrics.md).
Depends on **PR-1** (deleted the host collectors this PR rebuilds). Self-contained.
This is the large, cohesive feature PR; it also **restores the web mutation floor
to ≥ 85%** and **archives the whole workstream**.

## Objective

Add a **System Logs** pane that shows host logs (journald on Linux, Windows Event
Log on Windows) with level + search + window + **unit** filters and a unit
dropdown auto-populated from the host; repoint the Telemetry "View logs for this
window" drill to it; convert two refresh buttons to icons; and remove the
Recent Activity pane from the Dashboard.

## Decisions carried in (locked)

- **`source=host`** auto-resolves journald (Linux) / Windows Event Log (Windows);
  no OS logic in the browser.
- Drill **repoints to System Logs**; Agent Logs stays (loses the correlation jump).
- **Unit filter now**, agent-side, with **auto-detected units** in a UI dropdown;
  `available_units` **bundled** in the log response.
- **Level** filtering applies to host logs via the **shared severity filter**
  (min-severity: WARN ⊇ ERROR) — the substantive add (host collection returned
  unfiltered entries before).
- System Logs opens **unbounded (most-recent-N)**, mirroring Agent Logs.
- **Two separate cards** (Agent, System). Icon buttons **folded here**. Recent
  Activity removal **folded here**.

## File inventory

### Agent (rebuild collectors + dispatch)
| File | Change |
|---|---|
| `agent/crates/mesh-agent/src/host_logs.rs` | **Rebuild** (PR-1 deleted these): `LogSource{Journald,WindowsEventLog}`, `collect_host_logs`, `collect_journald`, `collect_windows_events`, and the journald/Windows JSON parsers + level/timestamp mappers (recover from git history). New: push **level** (`journalctl -p`, Windows `FilterHashtable Level`), **time** (`--since/--until @epoch`, `FilterHashtable StartTime/EndTime`), and **unit** (`_SYSTEMD_UNIT=<unit>` argv; Windows `FilterHashtable ProviderName`) to the collector; a `list_units(source)` enumerator (`journalctl -F _SYSTEMD_UNIT`; `Get-WinEvent -ListProvider *`), capped+sorted. `redact_entries` already present (kept in PR-1). |
| `agent/crates/mesh-agent/src/logs.rs` | Expose the shared filter (`pub(crate) fn filter_entries(&[LogEntry], &LogFilter)` extracted from `matches_filter`) so host entries get identical level/time/search semantics. |
| `agent/crates/mesh-agent/src/main.rs` | `RequestDeviceLogs` handler: **consume** `source`/`unit` (remove the `..` ignore at l.823-826). `""`/`"self"` → `LogCollector` (unchanged). `"host"` → resolve platform → `collect_host_logs(resolved, filter, unit)` → `filter_entries` → `redact_entries` → enumerate units → `DeviceLogsResponse{entries, total, has_more, available_units}`. |
| `agent/crates/mesh-protocol/src/control.rs` | `DeviceLogsResponse`: add `#[serde(default)] available_units: Vec<String>`. (`RequestDeviceLogs.source/unit` already exist.) |

### Wire protocol / golden
| File | Change |
|---|---|
| `server/internal/protocol/control.go` | `ControlMessage`: add `AvailableUnits []string \`msgpack:"available_units,omitempty"\``. |
| `agent/crates/mesh-protocol/tests/golden_test.rs` + `server/internal/protocol/golden_part*_test.go` + `testdata/golden/*` | Add/extend goldens: `RequestDeviceLogs` carrying `source`+`unit`; `DeviceLogsResponse` carrying `available_units`. Regenerate via `GENERATE_GOLDEN=1` both sides; `make golden`. |

### Server
| File | Change |
|---|---|
| `server/internal/device/device.go` | `LogFilter`: add `Source`, `Unit string`. |
| `server/internal/agentapi/conn.go` | `SendRequestDeviceLogs`: set `Source`, `Unit` on the control message. |
| `server/internal/agentapi/conn_logs.go` | Thread `available_units` from `handleDeviceLogsResponse` through `RequestLogsSync` back to the caller (extend `logsResult`). |
| `server/internal/api/handlers_device_inventory.go` | `logFilterFromParams`: map new `source`+`unit` query params; `GetDeviceLogs`: include `available_units` in the response; keep admin-gate + group-scope. |
| `server/api/openapi.yaml` | `/devices/{id}/logs`: add `source` (enum `self|host`, default `self`) + `unit` query params; `DeviceLogsResponse`: add `available_units: string[]`. Regen: `cd server && oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml > internal/api/openapi_gen.go`; `cd web && npm run generate:api`. |

### Web
| File | Change |
|---|---|
| `web/src/features/devices/LogExplorer.tsx` (NEW) | Shared explorer extracted from `DeviceLogs.tsx`: table + **level** (dropdown+facets) + search + **window (15m/1h/6h/24h)** + pagination; props: `source`, `title`, `showUnitFilter`, focus window, store selectors. |
| `web/src/features/devices/DeviceLogs.tsx` | Becomes the thin **AgentLogs** wrapper (`source='agent'/self`, no unit filter). |
| `web/src/features/devices/SystemLogs.tsx` (NEW) | `source='system'/host` wrapper: unit **dropdown** from `available_units`, `target` column, click-to-filter facet, unbounded first fetch. |
| `web/src/features/devices/state/device-store.ts` | **Source-key** the logs state: `logs/logsLoading` → keyed by `'agent'|'system'`; `fetchLogs(source, id, params)` sends `source` + `unit`; store `available_units`. |
| `web/src/features/devices/DeviceDetail.tsx` | Add a **separate System Logs card**; **repoint** `onViewLogs` to the System Logs focus window (l.475-502); make **Refresh Hardware** icon-only (l.435). |
| `web/src/features/devices/DeviceInventory.tsx` | Make footprint **Refresh** icon-only (l.174-182), `SpinnerIcon` while loading, `aria-label`+`title`. |
| `web/src/components/icons.tsx` | Add `RefreshIcon`. |
| `web/src/features/dashboard/Dashboard.tsx` | **Remove** the Recent Activity section (l.92) + its now-unused events fetch/state. |
| `web/src/features/dashboard/Dashboard.test.tsx` | Remove the two Recent Activity tests (l.162-180). |
| `web/src/types/api.d.ts` | Regenerated (source/unit params, available_units). |

## Security — unit-filter injection guard

- **journald:** unit/level/time are passed as **discrete argv** to `journalctl`
  (`_SYSTEMD_UNIT=<unit>`, `-p`, `--since/--until`) via `std::process::Command` —
  no shell, inherently injection-safe.
- **Windows:** the `Get-WinEvent -FilterHashtable` values compose into a PowerShell
  `-Command` string → **allowlist** the unit to `[A-Za-z0-9._@:/ -]` (covers
  systemd `user@1000.service` and providers like `Microsoft-Windows-Kernel-Power`
  with spaces); reject otherwise (ignore the unit / empty result). **Fixture test:**
  a hostile unit (`"; Remove-Item C:\ -Recurse"`) is inert on both paths.

## TDD-ordered steps

1. **RED (agent):** test host-log dispatch — `source=host` returns filtered,
   redacted entries + `available_units`; `unit` scopes results; the injection
   fixture proves a hostile unit is inert. Fails — collectors/dispatch absent.
2. Rebuild `host_logs.rs` collectors + `list_units` + `filter_entries`; wire the
   `main.rs` dispatch; add `available_units` to the protocol structs → GREEN.
3. **Golden:** `RequestDeviceLogs{source,unit}` + `DeviceLogsResponse{available_units}`
   (Rust generator + Go decode); regenerate; `make golden`.
4. Server: `LogFilter` + `SendRequestDeviceLogs` + broker threading + handler
   param mapping + OpenAPI + regen (Go/TS). Server tests for the new params +
   `available_units` passthrough.
5. **Web (test-first per unit):** `LogExplorer` extraction (existing DeviceLogs
   tests move onto it), `SystemLogs` (unit dropdown, target column, unbounded),
   store source-keying, `DeviceDetail` two cards + repoint drill (+ update
   `DeviceMetrics.test.tsx` "View logs" wiring), icon buttons (query by accessible
   name), remove Recent Activity (+ delete its tests).
6. **Restore the web mutation floor:** `make mutate-web` (Stryker) → write killer
   tests for surviving mutants until **web ≥ 85%** (currently 84.6% on `main`).
   Rust/Go floors held on changed code.
7. `make lint` / `make test` / `make golden` / `make e2e`; `/precommit` → commit →
   `/refactor` → `/precommit` → commit → push.
8. **Workstream close (this PR):** add an ADR in `docs/adr/` (live host-metric
   streaming + log-rate removal + host system-logs) + a `decisions.md` row;
   `/docs` update (Testing/Home as needed); `phases.md` **Completed** row linking
   `plans/archive/system-logs-and-central-host-metrics.md`; **archive all four
   plan files** (master + pr1/pr2/pr3) with the one-`../`-deeper link bump;
   validate `GO111MODULE=off go run ./scripts/check-doc-links`.

## Edge / error cases

- **Neither host source present** (container/minimal): empty entries → "No logs
  available"; empty `available_units` → dropdown shows "All units" only. No error.
- **Windows level scale is inverse** (1 Critical..5 Verbose): the shared filter
  normalizes both journald and Windows to one severity scale before min-severity
  compares.
- **Huge journald/provider set:** cap `available_units` (≈200, sorted); the unit
  filter still accepts an exact value outside the capped list.
- **Provider names with spaces:** allowed by the charset; bound as a single
  `FilterHashtable` value, never interpolated raw.
- **Older agent** (no `available_units`): `#[serde(default)]` → empty; dropdown
  degrades to "All units".
- **Recent Activity events fetch:** if the Dashboard's events query has no other
  consumer after removal, delete it too (else `ts-prune`/dead-code flags it).

## Reviewer checklist

- [ ] `source=host` resolves per-platform; `self` unchanged; Agent Logs pane intact (15m/1h/6h/24h + level preserved).
- [ ] **Level** filtering on host logs matches Agent Logs semantics (WARN⊇ERROR) — tested against a host fixture.
- [ ] Unit dropdown auto-populates from `available_units`; unit filter scopes results; `target` column + click-to-filter work.
- [ ] **Injection fixture green** on both collector paths.
- [ ] Drill "View logs for this window" focuses **System Logs**; two separate cards render.
- [ ] Icon buttons have accessible names + spinner; text-query tests updated.
- [ ] Recent Activity fully removed (section + fetch/state + tests); no dead code (`ts-prune`, `staticcheck`, `clippy -D warnings`).
- [ ] `make golden` green; OpenAPI + generated Go/TS in sync; no hand-edited generated files.
- [ ] Raw host lines redacted (edge + server); logs never persisted centrally; admin-gate + group-scope intact.
- [ ] **Web mutation ≥ 85%** in this PR; Rust/Go floors held.
- [ ] ADR + `decisions.md` + `phases.md` Completed row added; **all four plans archived**; doc-links green.

## DoD

`/precommit` green (lint, tests, coverage, golden, sonar, dead-code, shell, e2e),
`/refactor`, pushed to `dev`. Workstream complete: ADR recorded, phases.md updated,
master + three micro-plans archived. Web mutation ≥ 85% restored.
