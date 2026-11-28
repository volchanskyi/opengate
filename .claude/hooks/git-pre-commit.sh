#!/usr/bin/env bash
# Git's own pre-commit check, run inside the commit command itself: in this project's clones and
# work trees it refuses a commit unless the finished tidy-up and a gauntlet pass name the content.
set -euo pipefail

# CI's own commits carry no proofs; CI runs every check itself.
if [ -n "${CI:-}" ] || [ -n "${GITHUB_ACTIONS:-}" ]; then
  exit 0
fi

# Git hands a hook its own GIT_DIR and index, so they are dropped and the repo is found by its
# folder.
root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
[ -n "$root" ] || exit 0
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX
cd "$root"

# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
# shellcheck source=lib/tidy-up.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/tidy-up.sh"

holds_project_history || exit 0

if commit_proofs_match; then
  exit 0
fi

printf '%s\n' \
  "commit refused by git's own check: the content on disk lacks a finished /refactor and a gauntlet" \
  "pass from the last $((GAUNTLET_PASS_SECONDS / 60)) minutes. The order is /refactor (scripts/refactor-gate.sh" \
  "start … finish), then ./scripts/precommit-gauntlet.sh, then the commit. .claude/rules/refactor.md." >&2
exit 1
