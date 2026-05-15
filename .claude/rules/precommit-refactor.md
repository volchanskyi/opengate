# /precommit and /refactor

**Enforced by:** [`.claude/hooks/pretooluse-git-commit-guard.sh`](../hooks/pretooluse-git-commit-guard.sh) (precommit marker), [`.claude/hooks/pretooluse-git-push-guard.sh`](../hooks/pretooluse-git-push-guard.sh) (refactor marker). **No bypass.**

## /precommit

Run `/precommit` before EVERY commit. Including docs-only commits and CI-only commits.

The marker file `.claude/.markers/precommit.head` (= `git write-tree` at the time `/precommit` last passed) is checked by the commit guard. The hook blocks `git commit` when the marker is missing or stale. Re-staging invalidates the marker — re-run `/precommit`.

## /refactor

Run `/refactor` after `/precommit` passes. Before pushing.

The marker file `.claude/.markers/refactor.head` (= `git rev-parse HEAD` after `/refactor` finishes) is checked by the push guard. The hook blocks `git push` when commits since `origin/dev` touch source files unless this marker equals HEAD. Doc-only pushes are exempt.

## Multi-PR rollouts

Run `/precommit` after EVERY PR in a multi-PR rollout — including docs-only PRs. Security audits (`govulncheck`, `cargo audit`, `npm audit`) evaluate lockfiles, not the diff, so a new advisory published yesterday will fail today's gate even if today's diff only touches Markdown.

Exception: a fast-follow commit that fixes a CI failure already reported by a previous push — re-running `/precommit` adds no safety and wastes a CI cycle.
