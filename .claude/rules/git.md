# Git Workflow

**Enforced by:** [`.claude/hooks/pretooluse-git-commit-guard.sh`](../hooks/pretooluse-git-commit-guard.sh), [`.claude/hooks/pretooluse-git-push-guard.sh`](../hooks/pretooluse-git-push-guard.sh). **No bypass.**

## Branching

All work happens on `dev`. No exceptions.

- Before starting any work: `git checkout dev && git pull origin dev`
- Before every push: `git pull --rebase origin dev` then push
- Commit and push to `dev` only: `git push origin dev`
- Never commit or push directly to `main` — `main` receives code exclusively via the automated `merge-to-main` CI job after all checks pass on `dev`

## Commit / Push Atomicity

Never leave committed changes un-pushed after the implementation is complete. Commit and push are a single handoff: once a commit succeeds, push it immediately before yielding back to the user. Do not allow a time gap where a freshly tested local commit remains only local; dependency/security gates can change underneath that commit and make the eventual push fail for reasons that were not present at commit time.

## Identity

Every commit must be authored by Ivan Volchanskyi. No co-authors, no `Co-Authored-By` trailers.

- `git config user.name "Ivan Volchanskyi"`
- `git config user.email "ivan.volchanskyi@gmail.com"`
