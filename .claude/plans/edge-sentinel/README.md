# Edge-Sentinel — micro-plan set (index)

Breakdown of the master plan `../edge-sentinel.md` into self-contained workstreams (WS) for
parallel engineers. Each WS file states its own objective, dependencies, file inventory, TDD
steps, gotchas, reviewer checklist, and verification.

> Note: per the doc-link rule, plan files reference **other plans** (master/siblings) by
> plain path/code span, never markdown links — only repo source/docs are linked.

## Feature in one paragraph

Turn the reactive Rust agent into an **edge sentinel**: a clean-room (no GPL) k=2 k-means
ensemble computes per-metric anomaly bits locally (Netdata-style consensus, 99th-pct
threshold), the agent ships compact summaries + full {min,max,avg,last}@10s windows + a
bounded top-N process report over the existing mTLS QUIC control channel, the server
persists numeric series to a **TimescaleDB hypertable** (extension on the existing
Postgres) and process descriptive data (name + **redacted** cmdline) to an **RLS** table,
an **on-demand** SQL correlation engine ranks "what broke pattern," and the React client
surfaces a health badge + anomaly panel + uPlot timelines. The whole product becomes
**multi-tenant with Postgres RLS** as a prerequisite. Cold data offloads to Parquet on OCI
Object Storage, queried on-demand by server-side DuckDB.

## Workstreams

- `ws-0-tenancy-rls.md` — product-wide org_id + Postgres RLS foundation
- `ws-1-protocol-forward-compat.md` — dispatch tolerates unknown control types
- `ws-2-edge-ml-sampler.md` — clean-room k-means ensemble + sysinfo sampler (agent)
- `ws-3-wire-contract.md` — additive ControlMessage variants + Rust↔Go goldens
- `ws-4-server-ingest-timescale.md` — TimescaleDB hypertable + handlers + process RLS table
- `ws-5-correlation-engine.md` — on-demand SQL anomaly-advisor ranking
- `ws-6-web-ui.md` — badge + anomaly panel + uPlot timelines + drill-down
- `ws-7-cold-tier-duckdb.md` — Parquet/object cold tier + server-side DuckDB
- `ws-8-ops-measurement.md` — Grafana, load-test, p99 budget + mitigation ladder

## Dependency graph / parallelization waves

```
Wave 1 (parallel):   WS-0 RLS foundation   WS-1 protocol fix   WS-2 agent ML+sampler
Wave 2:              WS-3 wire contract        (needs WS-2 shapes; WS-1 for fwd-compat)
Wave 3:              WS-4 server ingest+TSDB   (needs WS-0 + WS-3)
Wave 4:              WS-5 correlation engine   (needs WS-4)
Wave 5 (parallel):   WS-6 web UI (needs WS-5)  WS-7 cold tier (needs WS-4)
Wave 6:              WS-8 ops + load-test      (needs WS-4 + WS-6)
```

WS-0, WS-1, WS-2 start immediately and in parallel. WS-1 (forward-compat dispatch) should
**merge first** so later additive messages are tolerated by older builds.

## Shared conventions (apply to every WS — see [CLAUDE.md](../../../CLAUDE.md))

- **TDD, no bypass.** Failing test before source; positive + negative. No
  `t.Skip`/`.skip`/`#[ignore]` — tests always run deterministically.
- **Per WS:** `/precommit` (`scripts/precommit-gauntlet.sh`) before every commit → commit →
  `/refactor` → push to `dev` only. Author = Ivan Volchanskyi, no co-authors.
- **Protocol:** any `ControlMessage` change is golden-gated (`make golden`).
- **No GPL** vendored/ported (Netdata GPL-3; workspace Apache-2). Clean-room only.
- **New deps** (`arrow`/`parquet`, object-store, **DuckDB = CGO**, the TimescaleDB image)
  each need an **ADR** in [docs/adr/](../../../docs/adr/) + index row in
  [decisions.md](../../decisions.md).
- **Default-off** behind a flag until WS-3/WS-4 land; failure = silent degradation.
- **Tenancy:** after WS-0 every new tenant table carries `org_id` + RLS; every new repo
  method runs in a tenant-scoped tx; add a **cross-tenant-deny test**.
- **Docs:** end each WS with a [/docs](../../../docs/) update (link, don't paraphrase).

## Reviewer checklist template (Claude reviews each implementation)

- [ ] Failing test existed before source (TDD); positive + negative covered.
- [ ] Scope matches this WS only; no unrelated churn; no silent SKIP.
- [ ] Repo rules: no GPL, no Sonar/lint suppressions, golden updated if protocol touched.
- [ ] Tenancy: org_id + RLS + cross-tenant-deny test where applicable.
- [ ] Security/NFR: no secrets/PII leak; bounded resources; mTLS-only transport; no standing creds.
- [ ] `/precommit` green; `/docs` updated.
