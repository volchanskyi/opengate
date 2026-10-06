#!/usr/bin/env bash
# Auto-push run by git's native post-commit hook, which fires after every commit, background or not.
# It pushes only a commit made from content /refactor finished on, and always exits 0.
set -uo pipefail

dbg() {
  if [ -n "${OPENGATE_AUTOPUSH_DEBUG:-}" ]; then
    printf 'autopush-dbg: %s\n' "$1" >&2
  fi
}
dbg "enter: pwd=$(pwd) GIT_DIR=${GIT_DIR:-unset} CI=${CI:-unset} GHA=${GITHUB_ACTIONS:-unset}"

# The pull --rebase and push below never re-enter this hook.
if [ -n "${OPENGATE_AUTOPUSH_ACTIVE:-}" ]; then
  dbg "exit: re-entrancy guard (OPENGATE_AUTOPUSH_ACTIVE set)"
  exit 0
fi
export OPENGATE_AUTOPUSH_ACTIVE=1

if [ -n "${CI:-}" ] || [ -n "${GITHUB_ACTIONS:-}" ]; then
  dbg "exit: CI guard (CI=${CI:-} GHA=${GITHUB_ACTIONS:-})"
  exit 0
fi

# The hook env (GIT_DIR, GIT_INDEX_FILE) is dropped so later git commands find the repo by cwd.
root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
dbg "root='$root'"
[ -n "$root" ] || exit 0
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX
cd "$root" || exit 0

branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo)"
dbg "branch='$branch'"
if [ "$branch" != "dev" ]; then
  echo "auto-push skipped: not on dev (on '$branch')"
  exit 0
fi

# Only a commit of the content /refactor finished on, after a gauntlet pass, is marked or pushed.
# shellcheck source=lib/tidy-up.sh
source "$root/.claude/hooks/lib/tidy-up.sh"
if ! tidy_done_matches; then
  dbg "exit: no finished /refactor for this content"
  echo "auto-push skipped: no finished /refactor for the content this commit was made from. Run ./scripts/precommit-gauntlet.sh to a pass, then /refactor, then push."
  exit 0
fi

# Marking before the push lets a later manual push pass the push guard if this push fails.
tidy_write refactor.head "$(git rev-parse HEAD)"
dbg "marker written: $(tidy_read refactor.head)"

# A conflicting rebase is aborted; a replayed commit changes HEAD, so the marker is re-pointed.
if ! git pull --rebase origin dev; then
  git rebase --abort 2>/dev/null || true
  dbg "exit: 'git pull --rebase origin dev' failed"
  echo "auto-push aborted: 'git pull --rebase origin dev' failed — resolve and push manually"
  exit 0
fi
tidy_write refactor.head "$(git rev-parse HEAD)"
dbg "rebased; marker now $(tidy_read refactor.head)"

if git push origin dev; then
  dbg "pushed ok"
  echo "auto-push: pushed $(git rev-parse --short HEAD) to origin/dev"
  # Caches are reclaimed only after a successful push, so a failed push keeps its build cache.
  cleaner="$root/.claude/hooks/post-push-clean-caches.sh"
  if [ -x "$cleaner" ]; then
    dbg "running post-push cache clean"
    "$cleaner" "$root" || true
  fi
else
  dbg "push failed"
  echo "auto-push failed at 'git push origin dev' — push manually"
fi
exit 0
