# Plan: Extract CLAUDE.md Sections into Skills

## Context

The project CLAUDE.md (92 lines) is loaded into every conversation turn. Two large procedural sections — **Pre-Commit Checklist** (23 lines) and **Post-Commit Refactoring** (18 lines) — are only relevant at commit time, yet consume context window space during all coding conversations. Extracting them into skills makes them load on-demand only when invoked.

## Analysis

| Section | Lines | Always needed? | Extract? |
|---------|-------|----------------|----------|
| Branching Rules | 6 | Yes (guardrail) | No |
| Git Identity | 4 | Yes (guardrail) | No |
| TDD Mandate | 3 | Yes (guardrail) | No |
| Rust Conventions | 7 | Yes (coding) | No — too short |
| Go Conventions | 6 | Yes (coding) | No — too short |
| TypeScript Conventions | 5 | Yes (coding) | No — too short |
| Wire Protocol | 4 | Yes (protocol) | No — too short |
| **Pre-Commit Checklist** | **23** | Only at commit time | **Yes** |
| **Post-Commit Refactoring** | **18** | Only after commit | **Yes** |
| Commands | 5 | Yes (reference) | No |

## What to Extract

### 1. `/precommit` skill

**Source:** Lines 44-66 (Pre-Commit Checklist)
**Path:** `.claude/skills/precommit/SKILL.md`

Contains the full procedural checklist: lints (4 steps), tests (3 steps), benchmarks (2 steps), documentation (2 steps), and the gate criteria (coverage thresholds, no failures).

### 2. `/refactor` skill

**Source:** Lines 68-85 (Post-Commit Refactoring)
**Path:** `.claude/skills/refactor/SKILL.md`

Contains the post-commit refactoring workflow: constraints (no new libs, no API changes, no business logic changes), 4-step process (Analyze → Strategize → Divide and conquer → Test), and focus areas.

Note: The built-in `/simplify` skill is generic ("review changed code for reuse, quality, and efficiency"). The project-specific `/refactor` skill adds explicit constraints and a structured methodology, so it's not redundant.

### 3. Update CLAUDE.md

Replace the two extracted sections with brief 1-2 line stubs that reference the skills. This preserves the "MANDATORY" reminders in always-loaded context while moving procedural details to on-demand loading.

**Before** (~92 lines) → **After** (~55 lines), saving ~37 lines of context per turn.

Stub examples:
```
## Pre-Commit Checklist
**MANDATORY** — Run `/precommit` before EVERY commit. No exceptions.

## Post-Commit Refactoring
**MANDATORY** — After all pre-commit checks pass, run `/refactor`. No exceptions.
```

## Files to Create/Modify

1. **Create** `.claude/skills/precommit/SKILL.md` — full checklist with frontmatter
2. **Create** `.claude/skills/refactor/SKILL.md` — full refactoring workflow with frontmatter
3. **Modify** `CLAUDE.md` — replace lines 44-85 with 2-line stubs

## Verification

- Invoke `/precommit` in a conversation and confirm all steps are listed
- Invoke `/refactor` in a conversation and confirm the workflow loads
- Confirm CLAUDE.md still loads the stubs with mandatory reminders
- Confirm no other sections were affected
