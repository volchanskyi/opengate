# Plan: Project Handover & Knowledge Management System

## Context

Plans currently live in `~/.claude/plans/` (user-level, outside the project, random names, not
version-controlled). There is no canonical phase tracker or tech debt register. The auto-memory
system (`~/.claude/projects/.../memory/MEMORY.md`) has overlapping project state info, but it's
Claude-specific and not visible to human engineers or fresh agent sessions without memory loaded.

**Goal**: A lightweight, version-controlled handover system so any agent or engineer can quickly
orient themselves to where the project is and what debt exists, without reading the entire codebase.

---

## Current State

```
/home/ivan/opengate/.claude/
  settings.json
  settings.local.json     ← likely should be gitignored
  skills/
    backend-audit/
    frontend-audit/
    infra-audit/
    precommit/
    refactor/

~/.claude/plans/            ← 20+ plans, random names, outside the project, NOT version-controlled
~/.claude/projects/.../memory/  ← auto-memory files, also outside the project
```

---

## Options

### Option A — Flat (Recommended: simplest, least friction)

```
.claude/
  phases.md       ← new
  techdebt.md     ← new
  plans/          ← new; all agent plans moved here, descriptive names
  settings.json
  settings.local.json
  skills/
```

**Pros**: Minimal new structure; `.claude/` already exists; plans are right next to settings
**Cons**: `.claude/` is tool-specific — engineers unfamiliar with Claude Code might not look there

---

### Option B — Dedicated `handover/` subdirectory

```
.claude/
  handover/
    phases.md
    techdebt.md
    decisions.md   ← optional ADR log
  plans/
  settings.json
  skills/
```

**Pros**: Clearly separates "docs for humans/agents" from "Claude tooling config"
**Cons**: One extra directory level; `handover/` is slightly unusual naming

---

### Option C — Project-level `docs/` folder (most engineer-friendly)

```
docs/
  phases.md
  techdebt.md
.claude/
  plans/
  settings.json
  skills/
```

**Pros**: Docs visible without knowing about `.claude/`; standard convention for OSS projects
**Cons**: Splits handover knowledge across two locations (`docs/` vs `.claude/plans/`)

---

## Recommendation: Option A

Keep everything in `.claude/`. It's already version-controlled (not in `.gitignore`), agents already
know to look there, and it avoids a `docs/` folder that might grow into something else later.
Add a `plans/` subdirectory for all plan files going forward.

---

## File Formats

### `.claude/phases.md`

```markdown
# Implementation Phases

<!-- Last updated: YYYY-MM-DD by <agent/engineer> -->

## ✅ Completed
| Phase | Summary | Key Files | Plan |
|-------|---------|-----------|------|
| Phase 0: Scaffolding | Rust workspace, Go module, React/Vite, CI | / | — |
| Phase 1: Shared Protocol | mesh-protocol crate + Go, golden tests | crates/mesh-protocol, server/internal/protocol | — |
...

## 🔄 In Progress
| Phase | Summary | Owner | Plan |
|-------|---------|-------|------|
| Linux Headless + Tray Removal | Remove tray/IPC on Linux | — | [plans/linux-headless-tray-removal.md] |
...

## ⏳ Planned
| Phase | Summary | Priority | Notes |
|-------|---------|----------|-------|
| Phase 13: Multiserver | PostgreSQL, cross-server routing, k8s | High | — |
...
```

### `.claude/techdebt.md`

```markdown
# Tech Debt Register

<!-- Last updated: YYYY-MM-DD by <agent/engineer> -->

## 🔴 Critical

## 🟠 High

## 🟡 Medium
### QUIC Stream Ownership Workaround
- **File**: `server/internal/agentapi/` + `crates/agent-core/`
- **Issue**: Server opens control stream instead of agent due to quic-go AcceptStream bug with mTLS
- **Impact**: Breaks at >20k concurrent agents
- **Fix**: Revert once quic-go fixes mTLS AcceptStream
- **Ref**: `.claude/plans/quic-stream-ownership-fix.md`

## 🟢 Low
```

### `.claude/plans/` naming convention

Descriptive, kebab-case: `phase13-multiserver.md`, `fix-quic-stream-ownership.md`,
`linux-headless-tray-removal.md`. No random names. Date prefix optional: `2026-03-29-refactor.md`.

---

## CLAUDE.md Changes

Add a **Project State** section near the top (after Branching Rules):

```markdown
## Project State — Read Before Starting Work
Before beginning any session, read:
- `.claude/phases.md` — implementation phases (completed / in-progress / planned)
- `.claude/techdebt.md` — known tech debt by severity

After completing any significant work item, update both files to reflect new state.
New agent plans must be created in `.claude/plans/` with a descriptive kebab-case name.
```

---

## Relationship to Auto-Memory

The `~/.claude/.../memory/MEMORY.md` system is Claude-specific (not committed) and serves a
different purpose: agent preferences, feedback, short-term context, references to external systems.
After this system is in place, MEMORY.md should slim down to only things NOT in the new files
(feedback, preferences, references) and point to `.claude/phases.md` + `.claude/techdebt.md` as
the canonical source for project state.

---

## .gitignore Note

`settings.local.json` should be added to `.gitignore` (local machine settings, likely has personal
paths). `phases.md`, `techdebt.md`, and `plans/` should be committed.

---

## Migration Steps (if approved)

1. Add `.claude/settings.local.json` to `.gitignore`
2. Create `.claude/phases.md` — populate from existing MEMORY.md "Completed Phases" section
3. Create `.claude/techdebt.md` — populate from existing MEMORY.md "Known Tech Debt" section
4. Create `.claude/plans/` directory (add `.gitkeep`)
5. Rename/move in-progress plan files from `~/.claude/plans/` → `.claude/plans/` with descriptive names
6. Update `CLAUDE.md` with "Project State" section
7. Update `MEMORY.md` to remove duplicated phase/debt content and point to the new files

---

## Decisions Made

- **Structure**: Option A — flat `.claude/` (phases.md, techdebt.md, decisions.md, plans/)
- **Plan migration**: Full — move all ~20 plans from `~/.claude/plans/` into `.claude/plans/` with descriptive names
- **ADR log**: Yes — add `decisions.md` capturing existing key decisions

---

## Final File Layout

```
.claude/
  phases.md         ← implementation phase tracker
  techdebt.md       ← tech debt register
  decisions.md      ← ADR log (key architectural decisions)
  plans/            ← all agent plans, descriptive kebab-case names
    .gitkeep
    phase13-multiserver.md
    linux-headless-tray-removal.md
    quic-stream-ownership-fix.md
    broad-refactor.md
    auto-tag-changelog.md
    screen-capture-chat.md
    test-coverage-gaps.md
    ... (migrated from ~/.claude/plans/)
  settings.json
  settings.local.json   ← add to .gitignore
  skills/
```
