# Cross-Agent Compatibility

## Shared instruction entry point

[`AGENTS.md`](../../AGENTS.md) at the repository root is a symlink to
[`CLAUDE.md`](../../CLAUDE.md). `CLAUDE.md` remains the single source of truth
for the project rules index; do not edit the compatibility path separately.

Claude Code reads `CLAUDE.md` and [`.claude/settings.json`](../settings.json).
Codex reads `AGENTS.md`.

## Shared workflows

Repo workflows remain canonical under [`.claude/skills/`](../skills/). When a
request matches a skill's `name` or `description`, Codex must read and follow
that skill's `SKILL.md` directly. At session start, inspect only skill
frontmatter for discovery and load full skill bodies on demand.

Update the canonical rule or skill under `.claude/`; never maintain a separate
Codex copy.

## Client-specific configuration

`.claude/settings.json` activates hooks only in Claude Code. Codex must treat
[`.claude/hooks/`](../hooks/) as executable policy and follow the same rules,
but must not assume those `PreToolUse` hooks intercepted a Codex tool call.

Tool permission allowlists and lifecycle-hook configuration are client-specific.
Do not copy `.claude/settings.json` or `.claude/settings.local.json` into Codex
configuration. Mandatory project behavior belongs in [`CLAUDE.md`](../../CLAUDE.md)
and [`.claude/rules/`](./).
