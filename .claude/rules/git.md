# Git Workflow

**Enforced by:** [`.claude/hooks/pretooluse-git-commit-guard.sh`](../hooks/pretooluse-git-commit-guard.sh), [`.claude/hooks/pretooluse-git-push-guard.sh`](../hooks/pretooluse-git-push-guard.sh). **No bypass.**

## Branching

All work happens on `dev`. No exceptions.

- Before starting any work: `git checkout dev && git pull origin dev && git pull origin main`
- Before every push: `git pull --rebase origin dev` then push
- Commit and push to `dev` only: `git push origin dev`
- Never commit or push directly to `main` — `main` receives code exclusively via the automated `merge-to-main` CI job after all checks pass on `dev`

## Why `dev` also pulls `main`

`dev` is where the work happens and `main` is downstream of it, so the second
pull looks redundant. It is not: a few things reach `main` without passing
through `dev`, and nothing carries them back.

Dependabot **security** updates are the recurring case. Every ecosystem in
[`dependabot.yml`](../../.github/dependabot.yml) sets `target-branch: dev`, and
routine version bumps honour it — but a security update ignores `target-branch`
and always opens against the repository's default branch, which is `main`. So
the one class of dependency change that matters most is the one class `dev`
never sees.

What that costs is a gate failing on `dev` for something already fixed on
`main`: the lockfile audits read the current advisory database rather than the
diff, so `dev` keeps failing every commit — including a docs-only one — until
the fix is carried across. It stranded a patched `browserslist` for `web` and a
patched `dompurify` and `mermaid` for `tools/mermaid-validate`, the latter for
months, because no gate on `dev` was even looking at that second lockfile.

Pulling `main` at the start of the work is what closes it. Do it before the
first commit, not after a gate has already gone red.

## Commit / Push Atomicity

Never leave committed changes un-pushed after the implementation is complete. Commit and push are a single handoff: once a commit succeeds, push it immediately before yielding back to the user. Do not allow a time gap where a freshly tested local commit remains only local; dependency/security gates can change underneath that commit and make the eventual push fail for reasons that were not present at commit time.

## Identity

Every commit must be authored by Ivan Volchanskyi. No co-authors, no `Co-Authored-By` trailers.

- `git config user.name "Ivan Volchanskyi"`
- `git config user.email "ivan.volchanskyi@gmail.com"`
