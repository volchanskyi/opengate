# ADR-028: PMAT 3.17.0 CLI / MCP surface mapping (amends ADR-019 implementation)

Date: 2026-05-31
Status: Accepted

## Context

[ADR-019](ADR-019-pmat-quality-overlay.md) adopted PMAT as an augment-only quality overlay at three integration points and pinned **`pmat@3.17.0`** exactly. ADR-019 wrote its integration points against an *anticipated* CLI/MCP surface drawn from the upstream evaluation. When the overlay was actually implemented against the pinned binary, several of ADR-019's literal prescriptions turned out **not to exist in 3.17.0**:

- The precommit command `pmat tdg --since-commit HEAD~1 --threshold B+` does not run: `--since-commit` is absent, and `--threshold` in 3.17.0 is a *complexity* knob for `--explain` mode, **not** a grade floor.
- The MCP "7-tool allow-list" names (TDG, repo-score, churn, entropy, duplicates, faults, Git-history RAG) mostly **do not exist as MCP tools** in 3.17.0. TDG, repo-score and entropy are CLI-only; the MCP surface is ~21 `analyze_*` / `generate_*` / template tools. `pmat serve` exposes no server-side tool filter.
- `repo-score` reports on a **0–100** scale in 3.17.0 (grade letter alongside), not the 0–289 figure the upstream evaluation referenced.

ADR-019's **decisions are unchanged** — B+ floor on changed code at precommit, a read-only MCP allow-list, a nightly trend → Loki/Grafana with a ≥3-point repo-score and a single-file-TDG-slip Telegram alert, Kaizen `--dry-run` only, exact-pin policy. What changed is only the *mechanics* needed to realise those decisions on the pinned tool. ADRs are immutable ([rules/plans-and-adrs.md](../../.claude/rules/plans-and-adrs.md)), so rather than edit ADR-019 this ADR records the verified mapping; the [decisions.md](../../.claude/decisions.md) index points ADR-019 → ADR-028. This is errata/clarification, not a reversal.

## Decision

Implement ADR-019's three integration points using the verified 3.17.0 surface below. The exact-version pin (ADR-019 §5.5) makes this mapping stable; a future `pmat` bump must re-verify it under a new ADR.

### Integration point 2 — precommit gate

| ADR-019 literal | 3.17.0 reality / implementation |
|---|---|
| `pmat tdg --since-commit HEAD~1 --threshold B+` | `pmat tdg check-quality -p <file> --min-grade B+ --fail-on-violation`, one invocation per changed code file. `--min-grade` is the grade floor; `check-quality` exits non-zero (3) on a violation. |
| "changed files" via `--since-commit` | Resolved in git by [`scripts/pmat-precommit.sh`](../../scripts/pmat-precommit.sh): union of `origin/dev..HEAD` + staged + unstaged + untracked. |
| "any changed file" | **Scope = changed *code* files including tests, excluding generated** (`scripts/tdd-check.sh is-code`). Decision taken 2026-05-31: `check-quality` only grades code (Go/Rust/TS) — it ignores shell/YAML/Markdown even though `pmat tdg <file>` grades those when targeted directly. Generated files (`openapi_gen.go`, `*_gen.go`, `*.pb.go`) are excluded because they are regenerated, not hand-maintainable to a floor. `build.rs` is included (it is hand-written `.rs`). |

Pre-existing files already below B+ (e.g. `server/internal/cert/cert.go` = C, `agent/crates/mesh-agent/build.rs` = F) only block a commit once touched — the Clean-as-You-Code model. No inline suppressions; an exception requires an ADR reference in the PR description.

### Integration point 1 — MCP read-only allow-list

`pmat serve --mode mcp` (registered in [`.mcp.json`](../../.mcp.json)) exposes its full tool surface with **no server-side filter**, so the read-only allow-list is enforced in [`.claude/settings.json`](../../.claude/settings.json): `enabledMcpjsonServers: ["pmat"]` approves the project server; `permissions.allow` blesses the 7 read-only tools; `permissions.deny` hard-blocks the file-writing tools (the analogue of ADR-019's "tools that write outside an ephemeral report directory are not exposed", since 3.17.0 has no `kaizen` MCP tool).

| ADR-019 capability | 3.17.0 MCP tool (allowed) |
|---|---|
| TDG grade | `mcp__pmat__analyze_complexity` (TDG is CLI-only; complexity is the closest read-only MCP proxy) |
| Churn | `mcp__pmat__analyze_code_churn` |
| Faults | `mcp__pmat__analyze_satd` (self-admitted technical debt) |
| Duplicates | `mcp__pmat__analyze_duplicates_vectorized` |
| (dead code — added) | `mcp__pmat__analyze_dead_code` |
| Git-history RAG | `mcp__pmat__analyze_deep_context` |
| (health/handshake — added) | `mcp__pmat__get_server_info` |
| Repo score, Entropy | **No MCP tool in 3.17.0 (CLI-only).** Surfaced via the nightly workflow instead. |

Denied (file-writers): `scaffold_project`, `generate_template`, `generate_enhanced_report`.

### Integration point 3 — nightly workflow

[`.github/workflows/pmat-trend.yml`](../../.github/workflows/pmat-trend.yml) runs `pmat repo-score --format json` + `pmat tdg check-quality -p . --min-grade B+ --format json`, reshapes them via [`scripts/pmat-summarize.sh`](../../scripts/pmat-summarize.sh), and pushes to Loki via [`scripts/pmat-loki-push.sh`](../../scripts/pmat-loki-push.sh) (Grafana dashboard `opengate-pmat-trend`).

- **Repo-score scale:** 0–100 (with letter grade), not 0–289.
- **Day-over-day:** the previous repo-score and below-B+ count are read from Loki by [`scripts/pmat-loki-query.sh`](../../scripts/pmat-loki-query.sh) before the current push (Loki is the trend store per [ADR-017](ADR-017-ci-gates-consolidation.md)); the push uses only low-cardinality labels `{job,env}` so the query resolves a single series.
- **Single-file-TDG-slip alert** (ADR-019 §"Resolved thresholds" #7): implemented as the below-B+ **count rising** day-over-day — a faithful, Loki-storable proxy for "a file slipped below B+ since the previous run." Exact per-file enforcement is the precommit gate (IP2).

## Pre-decomposition baseline (ADR-019 §"Resolved thresholds" #6)

First baseline, on `dev` HEAD `d3f0373` (2026-05-31), before the first modular-monolith decomposition phase:

- **Repo score: 64.5 / 100 (grade C).** Low categories are measurement artifacts of pmat's own repo-checklist: Pre-commit Hooks 0/20 (pmat looks for a `.pre-commit-config.yaml`, not OpenGate's `.claude/hooks/`), PMAT Compliance 2.5/5 (no `.pmat-gates.toml`).
- **Files below B+: 8 production** (`handlers_devices.go` C-, `cert.go` C, `apf.go`/`mps.go` C+, `instrumented.go` B-, `postgres.go`/`handlers_security_groups.go` B, `build.rs` F) plus ~36 server test files; `web/src` clean.

## Consequences

- ADR-019's resolved thresholds and retirement criteria are preserved verbatim; only the invocations differ.
- The exact-version pin is now load-bearing for this mapping (not just reproducibility): a `pmat` bump can move flags, tool names, the grade scale, or the score scale. Any bump re-verifies this ADR's tables under a new ADR.
- `pmat`'s self-assessed repo-score categories reward conventions OpenGate deliberately does not use (pre-commit framework, `.pmat-gates.toml`); the absolute number is therefore a weak signal. The trend (day-over-day delta) and the TDG below-B+ count are the actionable parts.

## References

- Amends: [ADR-019](ADR-019-pmat-quality-overlay.md) (decisions unchanged; this records the 3.17.0 mechanics)
- Trend store: [ADR-017](ADR-017-ci-gates-consolidation.md) · Template workflow: [`.github/workflows/mutation.yml`](../../.github/workflows/mutation.yml)
- Implementation: [`scripts/pmat-precommit.sh`](../../scripts/pmat-precommit.sh), [`scripts/pmat-summarize.sh`](../../scripts/pmat-summarize.sh), [`scripts/pmat-loki-push.sh`](../../scripts/pmat-loki-push.sh), [`scripts/pmat-loki-query.sh`](../../scripts/pmat-loki-query.sh), [`scripts/tdd-check.sh`](../../scripts/tdd-check.sh) (`is-code`)
- Plan: [`.claude/plans/adr-020-024-pmat-phase-13b-rollout.md`](../../.claude/plans/adr-020-024-pmat-phase-13b-rollout.md)
