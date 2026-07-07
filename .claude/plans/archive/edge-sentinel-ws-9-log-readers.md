# WS-9 — Endpoint log readers + rate extractor (agent)

**Objective:** Read host logs at the edge and derive cheap **log-rate signals** (per-level +
per-unit top-N by rank + volume) that feed the **WS-2 anomaly ensemble**. Raw lines stay local
(answered only on-demand in WS-11). Agent-only; default-off.

**Dependencies:** WS-2 (ensemble + sampler patterns). **Blocks:** WS-10. **Parallel with:** WS-7.

## Context

Today only the agent's own `tracing-appender` files are read, on-demand, by
[`LogCollector`](../../../agent/crates/mesh-agent/src/logs.rs) (bounded: 7 files, 10k scan lines).
This WS adds **host** sources and a continuous **rate extractor** — the rate series are numeric and
ride the Edge-Sentinel pipeline; **no message content becomes a metric label**.

## Decisions (locked)

- Sources: Linux **journald/syslog**, **Windows Event Log**, **agent self-logs**. Text/JSON files
  out of scope.
- **No GPL/LGPL:** prefer **`journalctl -o json`** + Windows Event Log API (MIT/Apache `windows`
  crate) or `Get-WinEvent` shell-out over libsystemd FFI (ADR in WS-13).

## File inventory

- **Modify:** [`logs.rs`](../../../agent/crates/mesh-agent/src/logs.rs) — add host readers behind a
  source selector; normalize to [`LogEntry`](../../../agent/crates/mesh-protocol/src/types/log_entry.rs);
  keep scan/ring bounds.
- **Create:** `mesh-agent-core/src/ml/` rate extractor — per-level + per-unit **top-N by rank**
  counts + volume per window; feeds the WS-2 ensemble (alloc-free post-load).
- **Modify:** [`main.rs`](../../../agent/crates/mesh-agent/src/main.rs) — spawn readers as a bounded
  background task (default-off; **yields** to control/session traffic; hard RAM/CPU cap).

## Steps (TDD-first)

1. **Test first:** reader tests over **journald/syslog + Windows Event Log fixtures** (deterministic)
   → implement host readers (no-GPL sourcing); bounded scan; normalize to `LogEntry`.
2. **Test first:** rate-extractor tests (per-level/per-unit-rank counts + volume; an injected error
   burst raises the rate; flat input stays low) → implement.
3. **Test first:** feed-into-ensemble test (rate dims produce an anomaly bit on a synthetic burst)
   → wire into the WS-2 ensemble.
4. Spawn the bounded reader task in `main.rs` (default-off).

## Gotchas / constraints

- **No message text in metric labels** — only level / unit-rank / counts. Cardinality bounded by
  unit rank (like process top-N).
- Readers must **yield** and be hard-capped; logs are bursty and must never starve the agent.
- No-GPL sourcing; **no `unwrap()`**; `#[non_exhaustive]` on public enums; `///` docs.

## Reviewer checklist

- [ ] journald/syslog + Windows Event Log readers + self-logs; bounded; no-GPL sourcing.
- [ ] Rate extractor: per-level/per-unit-rank + volume; feeds WS-2; labels carry no message content.
- [ ] Background task default-off; yields; ring/scan hard-capped.
- [ ] Tests precede source (positive + negative); `/precommit` green.

## Verification

`cd agent && cargo test -p mesh-agent -p mesh-agent-core` + clippy `-D warnings`. `/precommit`
green. `/docs`: agent architecture page.
