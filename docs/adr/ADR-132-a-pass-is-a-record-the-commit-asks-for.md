---
number: 132
title: A pass is a record the commit asks for
---

# ADR-132 — A pass is a record the commit asks for

## Context

The commit guard ran the whole gauntlet inside a Claude Code PreToolUse hook with a 1,800 s
limit. On `13506f53` (2026-11-20) the run took 1,770 s — a stuck store-probe test spent 600 of
them — and Claude Code cancelled the hook at its limit (`hook_cancelled`, `timedOut: true` in the
session log) and ran the commit 0.26 s later, with three checks already failed. Claude Code's own
documentation says it: on PreToolUse, a hook that times out does not block the tool call, so a
stalled hook is no gate.

A cancelled hook says nothing, and the guard's "yes" was also saying nothing, so the two could not
be told apart. Any time limit fails open, and a passing gauntlet takes 35-43 minutes. The session
logs held 28 timed-out commit guards between 2026-10-24 and 2026-11-22, 1,760-2,000 s each: most
commits in that stretch went in without the gauntlet's answer, and the end-to-end tests and the
SonarCloud scan were never judged locally. The guard also caught `--no-verify` but not its short
form `-n`.

## Decision

**The gauntlet is a step of its own, run with no time limit, and a full pass writes a record.**
[`precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh) clears the last
`.claude/.markers/gauntlet.pass` before any check, and on exit 0 writes the content fingerprint
and the second its run began — only when the fingerprint at the end equals the one at the start.
A run that fails, stops at a missing prerequisite, is killed, or finds its content edited during
the run writes nothing. The fingerprint is the tidy-up's own
([`tidy-up.sh`](../../.claude/hooks/lib/tidy-up.sh)): the working tree as it stands on disk,
untracked files included.

**The commit asks for that record, twice.** The Claude Code commit guard
([`pretooluse-git-commit-guard.sh`](../../.claude/hooks/pretooluse-git-commit-guard.sh)) refuses a
commit unless the finished tidy-up and the gauntlet pass both name the content on disk; it reads
two files and finishes in seconds, under a 60 s limit. Git's own `pre-commit` hook
([`git-pre-commit.sh`](../../.claude/hooks/git-pre-commit.sh)) checks the same two records inside
the commit command itself, which no tool can abandon, so a guard that does time out still leaves
the commit refused. The session-start installer writes its shim beside the post-commit one, and
it judges every commit in a repository holding this project's first commit — its clones and its
linked work trees, a hand-made terminal commit included — and leaves CI's own commits to CI.

**A pass is good for one hour from its run's start.** The advisory lookups happen at the start of
the run, so the hour is counted from there; it leaves the 17-25 minutes after a passing run to
write the message and commit, and refuses a pass that sat while a new advisory could be published.

**A commit or push guard that runs out of time refuses.** Both carry Claude Code's
`onFailure: "block"` (from Claude Code 2.1.295), so a stalled fetch inside either refuses the call
rather than letting it through; an older client ignores the setting, and git's own check still
stands behind the commit.

**The post-commit hook pushes only a commit both records name.** A terminal commit run with `-n`
skips git's own check, and stays local.

**The hooks folder stays where it is.** The commit guard refuses `-n` anywhere in a commit's
option cluster (git reads `-nm x` as no-verify plus a message), and a `core.hooksPath` override by
`-c` or through `GIT_CONFIG_PARAMETERS` or `GIT_CONFIG_COUNT`, since each skips git's own check.

**The background run is watched, not waited on.** The gauntlet nudge fires on the gauntlet
command itself, and wakes the session every ten minutes while the run is still going.

## Consequences

A commit after a pass costs seconds; a commit with no pass costs a gauntlet run first, as before,
but outside any hook. An edit made after the run — a fixed typo, a new file in the work tree —
voids the pass, so the message file and the run's log live outside the work tree.

Git's own check makes every commit in the project's clones need the two records: a merge concluded
by hand, an amend, a terminal commit. A rebase and a cherry-pick commit without running
`pre-commit` (git 2.34), so their commits reach the remote only through the push guard, which
reads the refactor marker.

A guard that times out no longer lets a commit through, because git's own check runs inside the
commit. What it cannot stop is a commit made with `-n` in a terminal, which the post-commit hook
then declines to push.
