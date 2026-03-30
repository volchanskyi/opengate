# Developer Workflow Enhancement

Approved: 2026-03-30

## Phase 1: `.claude/` Dedup with Wiki ✅

- Refactor `decisions.md` to compact ADR index table — full text moved to [wiki ADR page](https://github.com/volchanskyi/opengate/wiki/Architecture-Decision-Records)
- Archive ~47 completed plan files to `.claude/plans/archive/`
- Update `CLAUDE.md` with new ADR workflow and archive convention
- Update `phases.md` links to archived paths

**Completed**: 2026-03-30

---

## Phase 2: Playwright MCP Server for Browser Automation

- Install `@anthropic-ai/mcp-playwright` (or equivalent Playwright MCP server)
- Configure in `.claude/mcp.json`
- Create `/browser` skill for browser automation tasks

---

## Phase 3: Observability + Autonomous E2E

- `/observe` skill — SSH tunnel + PromQL/LogQL queries against staging/prod
- `/local-e2e` skill — run Playwright E2E tests locally
- `/agent-test` skill — full agent lifecycle test (QUIC port in `docker-compose.test.yml`)

---

## Motivation

Enable autonomous bug reproduction, UI validation, log/metric querying, and full agent lifecycle testing without manual user intervention. Phases are independent — each is a standalone deliverable.
