# Git Workflow

**Enforced by:** [`.claude/hooks/pretooluse-git-commit-guard.sh`](../hooks/pretooluse-git-commit-guard.sh), [`.claude/hooks/pretooluse-git-push-guard.sh`](../hooks/pretooluse-git-push-guard.sh). **No bypass.**

## Branching

All work happens on `dev`. No exceptions.

- Before starting any work: `git checkout dev && git pull origin dev && git pull origin main`
- Before every push: `git pull --rebase origin dev`, then push
- Commit and push to `dev` only
- Never commit or push directly to `main`. `main` receives code exclusively via
  the automated `merge-to-main` CI job after all checks pass on `dev`.

`dev` pulls `main` because a Dependabot **security** update ignores
`target-branch` and opens against the default branch. Every ecosystem in
[`dependabot.yml`](../../.github/dependabot.yml) sets `target-branch: dev`, and
routine version bumps honour it. The lockfile audits read the current advisory
database rather than the diff, so `dev` fails every commit — including a
docs-only one — until the fix is carried across. Pull `main` before the first
commit.

## Commit / push atomicity

- Commit and push are a single handoff. Once a commit succeeds, push it
  immediately before yielding.
- Never leave committed changes un-pushed after the implementation is complete.

## Identity

Every commit is authored by Ivan Volchanskyi. No co-authors, no co-author
trailers.

- `git config user.name "Ivan Volchanskyi"`
- `git config user.email "ivan.volchanskyi@gmail.com"`
