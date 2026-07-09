# WS-13 — Log privacy, sourcing ADR, benchmark + soak

**Objective:** Close the cross-cutting concerns for endpoint logs: a raw-log redaction corpus, the
no-GPL reader-sourcing ADR, edge reader benchmarks (Linux + Windows) before default-on, and a soak
that folds logs into the WS-15b default-on decision.

**Dependencies:** WS-9..WS-12. **Parallel with:** WS-12. **Feeds:** WS-15b default-on gate.

## Context

Raw logs are secret-dense (worse than process cmdlines); redaction is defense-in-depth on top of
the WS-11 audit + elevated-permission + no-central-storage controls. The journald/Windows reader
sourcing (WS-9) is a license decision that must be recorded. Edge reader overhead on real hosts is
unproven and gates default-on.

## File inventory

- **Modify/Create:** redaction corpus (extend the WS-2 redaction tests) covering log-line secret
  shapes (tokens, connection strings, bearer/JWT, cloud keys, kv pairs) — agent + server guards.
- **Create:** edge reader benchmark (Linux journald/syslog + Windows Event Log) — CPU, RSS,
  allocations, enumeration cost; recorded artifact.
- **Modify:** [`loadtest`](../../../server/tests/loadtest/main.go) — add log-rate ingest +
  on-demand raw pulls to the multi-tenant soak.
- **Modify:** [`deploy/grafana/provisioning/dashboards/`](../../../deploy/grafana/provisioning/dashboards/)
  — log-rate ingest, raw-pull rate + latency, audit-event count panels (VM datasource).
- **Create:** ADRs (housekeeping below).

## Steps (TDD-first)

1. **Test first:** redaction corpus — each secret shape is stripped from a raw log line by both the
   agent and server guards → extend the redactor.
2. **Benchmark:** run the edge readers on a Linux host and a Windows host; record CPU/RSS/enumeration
   cost; assert within budget before default-on.
3. **Soak:** extend the load harness to drive log-rate ingest + periodic raw pulls under multi-tenant
   load; record control-plane p99 (must stay within the WS-15b budget), VM cardinality/disk, raw-pull
   latency, audit-event volume.
4. Add the Grafana panels; write the ADRs; flip default-on **only if every budget passes** (with WS-15b).

## Gotchas / constraints

- Redaction is **defense-in-depth**, not the only control (audit + permission + no-central-storage
  from WS-11 are primary). Corpus must be extensible — secret formats are app-specific.
- Benchmark on **both** Linux and Windows — `sysinfo`/journald/WinEvt costs differ by platform.
- Logs ride the WS-3 priority/drop path; the soak must prove log bursts never starve control.

## Reviewer checklist

- [ ] Redaction corpus covers the common secret shapes; agent + server guards tested.
- [ ] Edge reader benchmark (Linux + Windows) recorded; within budget; default-off until passed.
- [ ] Soak adds log-rate ingest + raw pulls; control-plane p99 within the WS-15b budget; dashboards added.
- [ ] ADRs written (model, raw privacy, reader sourcing); `/precommit` green.

## Verification

`cd agent && cargo test -p mesh-agent-core` (redaction); `cd server && go test ./tests/loadtest/...`;
run the soak via `make e2e`/the Docker lifecycle; capture Grafana panels. `/precommit` green.
`/docs`: Monitoring + Multiscale-Readiness + the endpoint-logs page.

## Housekeeping (ADRs)

- (a) endpoint-log model (edge-stored, server-proxied; why not Loki/central). (b) raw-log privacy
  (no central storage; audited on-demand; elevated permission; redaction defense-in-depth).
  (c) journald/Windows reader sourcing (no-GPL: `journalctl -o json` / Windows Event Log API).
  Index rows in [.claude/decisions.md](../../decisions.md); update [.claude/phases.md](../../phases.md).
