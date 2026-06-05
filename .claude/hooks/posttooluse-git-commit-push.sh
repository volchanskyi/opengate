#!/usr/bin/env bash
# posttooluse-git-commit-push.sh
#
# Auto-push after a successful commit — the push half of the commit-enforcing
# hook pair (user rule: "commit ⇒ push, no pause"). The PreToolUse commit-guard
# runs the gauntlet and gates the commit; this PostToolUse partner fires once the
# commit has LANDED and pushes it to dev. Wired in .claude/settings.json on
# PostToolUse/Bash with `if: "Bash(git commit:*)"`.
#
# Mechanics: a PreToolUse hook cannot push (it runs before the commit exists), so
# the push lives in PostToolUse. The push here is a subprocess, not a Claude tool
# call, so the push-guard does not re-run on it; the refactor marker is refreshed
# to HEAD to keep a later manual `git push` consistent with the push-guard.
#
# Fail-safe: only ever acts on branch `dev`; aborts cleanly (no half-rebase) if
# the rebase fails; never blocks (exit 0) — failures are reported for the model.
set -uo pipefail

payload="$(cat 2>/dev/null || true)"
case "$payload" in
  *'git commit'*) : ;;
  *) exit 0 ;;
esac

branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo)"
if [ "$branch" != "dev" ]; then
  echo "auto-push skipped: not on dev (on '$branch')"
  exit 0
fi

# Refresh the refactor marker so a later manual `git push` tool call also passes
# the push-guard (keeps marker state consistent with this auto-push).
mkdir -p .claude/.markers
git rev-parse HEAD > .claude/.markers/refactor.head

if ! git pull --rebase origin dev; then
  git rebase --abort 2>/dev/null || true
  echo "auto-push aborted: 'git pull --rebase origin dev' failed — resolve and push manually"
  exit 0
fi

if git push origin dev; then
  echo "auto-push: pushed $(git rev-parse --short HEAD) to origin/dev"
else
  echo "auto-push failed at 'git push origin dev' — push manually"
fi
exit 0
