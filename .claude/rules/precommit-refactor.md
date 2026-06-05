# /precommit and /refactor

**Enforced by:** [`.claude/hooks/pretooluse-git-commit-guard.sh`](../hooks/pretooluse-git-commit-guard.sh) (runs the gauntlet directly), [`.claude/hooks/pretooluse-git-push-guard.sh`](../hooks/pretooluse-git-push-guard.sh) (refactor marker). **No bypass.**

## /precommit

Run `/precommit` before EVERY commit. Including docs-only commits and CI-only commits.

`scripts/precommit-gauntlet.sh` is the single source of truth — both the `/precommit` skill and the commit-guard hook execute it. There is no marker file shortcut: the hook re-runs every check (lints, tests, coverage, audits, benchmarks, e2e, sonar) on each commit attempt. Refreshing a marker hash does NOT let a commit through — the hook ignores the legacy marker.

## /refactor

Run `/refactor` after `/precommit` passes. Before pushing.

The marker file `.claude/.markers/refactor.head` (= `git rev-parse HEAD` after `/refactor` finishes) is checked by the push guard. The hook blocks `git push` whenever there are **any** commits since `origin/dev` unless this marker equals HEAD — **regardless of what files they touch**. There is no doc-only / CI-only exemption: a push is a push. (The post-commit auto-push hook refreshes the marker to HEAD, so the normal commit→push flow satisfies this automatically; a manual push of a non-code change still needs `/refactor` — a near-no-op that writes the marker.)

## Multi-PR rollouts

Run `/precommit` after EVERY PR in a multi-PR rollout — including docs-only PRs. Security audits (`govulncheck`, `cargo audit`, `npm audit`) evaluate lockfiles, not the diff, so a new advisory published yesterday will fail today's gate even if today's diff only touches Markdown.

Exception: a fast-follow commit that fixes a CI failure already reported by a previous push — re-running `/precommit` adds no safety and wastes a CI cycle.
