# Developer Workflow Enhancement

Approved: 2026-03-30

## Phase 1: `.claude/` Dedup with Wiki ✅

- Refactor `decisions.md` to compact ADR index table — full text moved to [wiki ADR page](https://github.com/volchanskyi/opengate/wiki/Architecture-Decision-Records)
- Archive ~47 completed plan files to `.claude/plans/archive/`
- Update `CLAUDE.md` with new ADR workflow and archive convention
- Update `phases.md` links to archived paths

**Completed**: 2026-03-30

---

## Phase 2: Playwright MCP Server ~~for Browser Automation~~ — Cancelled

Playwright E2E tests already run in CI and against staging. A browser-automation MCP adds no value over the existing test infrastructure.

---

## Phase 3: `/observe` Skill — Autonomous Diagnostics ✅

Created `.claude/skills/observe/SKILL.md` with 5 sections:

1. **PromQL** — query VictoriaMetrics via SSH + `docker exec` (14 pre-defined queries covering HTTP, app gauges, DB, host metrics)
2. **LogQL** — query Loki via SSH + `docker exec` (server errors, auth failures, Caddy 5xx, agent/relay/enrollment events)
3. **Container health** — `docker ps`, health endpoints, resource usage, logs, image version
4. **Local agent diagnostics (WSL)** — systemctl, journalctl, log files, data directory, cert validation, QUIC connectivity
5. **Investigation playbooks** — "agent offline?", "requests slow?", "deployment health", "post-deploy verification"

**Completed**: 2026-04-01
