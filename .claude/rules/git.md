# Git Workflow

**Enforced by:** [`.claude/hooks/pretooluse-git-commit-guard.sh`](../hooks/pretooluse-git-commit-guard.sh), [`.claude/hooks/pretooluse-git-push-guard.sh`](../hooks/pretooluse-git-push-guard.sh). **No bypass.**

## Branching

All work happens on `dev`. No exceptions.

- Before starting any work: `git checkout dev && git pull origin dev && git pull origin main`
- Before every push: `git pull --rebase origin dev` then push
- Commit and push to `dev` only: `git push origin dev`
- Never commit or push directly to `main` — `main` receives code exclusively via the automated `merge-to-main` CI job after all checks pass on `dev`

## Why `dev` also pulls `main`

Some things reach `main` without passing through `dev`, and nothing carries them
back.

Dependabot **security** updates are the recurring case. Every ecosystem in
[`dependabot.yml`](../../.github/dependabot.yml) sets `target-branch: dev` and
routine version bumps honour it, but a security update ignores `target-branch`
and opens against the default branch, which is `main`.

The cost is a gate failing on `dev` for something already fixed on `main`: the
lockfile audits read the current advisory database rather than the diff, so `dev`
keeps failing every commit — including a docs-only one — until the fix is carried
across.

Pull `main` before the first commit, not after a gate has gone red.

## Commit / Push Atomicity

Never leave committed changes un-pushed after the implementation is complete. Commit and push are a single handoff: once a commit succeeds, push it immediately before yielding back to the user. Do not allow a time gap where a freshly tested local commit remains only local; dependency/security gates can change underneath that commit and make the eventual push fail for reasons that were not present at commit time.

## Identity

Every commit must be authored by Ivan Volchanskyi. No co-authors, no `Co-Authored-By` trailers.

- `git config user.name "Ivan Volchanskyi"`
- `git config user.email "ivan.volchanskyi@gmail.com"`
