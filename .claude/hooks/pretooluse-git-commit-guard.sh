#!/usr/bin/env bash
# pretooluse-git-commit-guard.sh — block bad commits.
#
# Triggers on PreToolUse Bash; noop unless the command is `git commit`.
# Enforces, in order:
#   1. No Co-Authored-By trailer in -m message.
#   2. No --no-verify flag.
#   3. Identity must equal "Ivan Volchanskyi <ivan.volchanskyi@gmail.com>".
#   4. Branch must not be main.
#   5. Branch must not be behind upstream (best-effort; offline → skip).
#   6. scripts/precommit-gauntlet.sh must exit 0 — runs EVERY check from
#      the /precommit skill (lints, tests, coverage thresholds, security
#      audits, benchmarks, e2e, sonar). This is the actual enforcement;
#      there is no forgeable marker. Refreshing a hash file does NOT let
#      a commit through.
#   7. TDD backup check via scripts/tdd-check.sh.
#
# NO BYPASS.
set -euo pipefail
# shellcheck source=lib/common.sh
# shellcheck source=lib/common.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

parse_input_fields tool_name tool_input.command

[ "${HOOK_TOOL_NAME:-}" = "Bash" ] || exit 0
cmd="${HOOK_TOOL_INPUT_COMMAND:-}"
[ -n "$cmd" ] || exit 0

# Filter: command must include a `git commit` verb.
if ! printf '%s' "$cmd" | grep -qE '\bgit[[:space:]]+(-[^[:space:]]+[[:space:]]+)*commit\b'; then
  exit 0
fi

# 1. Co-Authored-By.
if printf '%s' "$cmd" | grep -qiF 'Co-Authored-By'; then
  block git-no-co-authored-by "git commit refused: message contains Co-Authored-By. .claude/rules/git.md requires no trailers. Remove it and re-issue."
fi

# 2. --no-verify.
if printf '%s' "$cmd" | grep -qE -- '--no-verify\b'; then
  block git-no-verify "git commit refused: --no-verify disabled. Fix the underlying hook failure or remove the flag."
fi

# 3. Identity — read overrides from the command, else use repo config.
override_email="$(printf '%s' "$cmd" | grep -oE -- '-c[[:space:]]+user\.email=[^[:space:]]+' | tail -1 | sed -E 's/^-c[[:space:]]+user\.email=//' || true)"
override_name="$(printf '%s' "$cmd" | grep -oE -- '-c[[:space:]]+user\.name=[^[:space:]]+' | tail -1 | sed -E 's/^-c[[:space:]]+user\.name=//' || true)"
eff_email="${override_email:-$(git config user.email 2>/dev/null || echo "")}"
eff_name="${override_name:-$(git config user.name 2>/dev/null || echo "")}"

# Trim surrounding quotes from override values.
eff_email="${eff_email%\"}"; eff_email="${eff_email#\"}"
eff_email="${eff_email%\'}"; eff_email="${eff_email#\'}"
eff_name="${eff_name%\"}"; eff_name="${eff_name#\"}"
eff_name="${eff_name%\'}"; eff_name="${eff_name#\'}"

if [ "$eff_email" != "ivan.volchanskyi@gmail.com" ] || [ "$eff_name" != "Ivan Volchanskyi" ]; then
  block git-identity "git commit refused: identity is '$eff_name <$eff_email>'. .claude/rules/git.md requires Ivan Volchanskyi <ivan.volchanskyi@gmail.com>. Fix with: git config user.name \"Ivan Volchanskyi\" && git config user.email \"ivan.volchanskyi@gmail.com\"."
fi

# 4. Branch ≠ main.
current_branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
if [ "$current_branch" = "main" ]; then
  block git-branch-not-main "git commit refused: HEAD is on main. .claude/rules/git.md: main receives code only via the auto-merge CI job. Switch to dev."
fi

# 5. Behind upstream (best-effort; skip on offline).
if git fetch --quiet origin dev 2>/dev/null; then
  if git rev-parse --verify --quiet origin/dev >/dev/null 2>&1; then
    behind="$(git rev-list --count HEAD..origin/dev 2>/dev/null || echo 0)"
    if [ "${behind:-0}" -gt 0 ] 2>/dev/null; then
      summary="$(git log --format='%h %s' HEAD..origin/dev 2>/dev/null | head -5 || true)"
      block git-behind-upstream "git commit refused: local HEAD is $behind commit(s) behind origin/dev. Run: git pull --rebase origin dev. Upstream commits:
$summary"
    fi
  fi
fi

# 6. Run the full precommit gauntlet. No marker shortcut — the script IS
#    the gate. Output streams to stderr; on first failed check the script
#    keeps going so the user sees ALL failures in one pass. Exit code 0 =
#    clean, 1 = check(s) failed, 2 = prerequisite missing (Postgres,
#    SONAR_TOKEN, $HOME/go shadow install).
gauntlet="$(project_root)/scripts/precommit-gauntlet.sh"
if [ ! -x "$gauntlet" ]; then
  block git-precommit-gauntlet "git commit refused: scripts/precommit-gauntlet.sh is missing or not executable. The hook needs it to enforce the precommit checks."
fi
rc=0
"$gauntlet" >&2 || rc=$?
if [ "$rc" -ne 0 ]; then
  case "$rc" in
    2) block git-precommit-prereq "git commit refused: precommit gauntlet reported a missing prerequisite (see output above). Fix the prerequisite and re-attempt — do not refresh a marker, the hook re-runs every check." ;;
    *) block git-precommit-failed "git commit refused: precommit gauntlet failed (exit $rc). Fix the failing checks listed above and re-attempt — every commit re-runs the full gauntlet." ;;
  esac
fi

# 7. TDD backup check.
if ! "$TDD_CHECK" has-test-change; then
  # Only blocks if the branch has source-language changes vs base.
  base="$(git merge-base HEAD origin/dev 2>/dev/null \
         || git merge-base HEAD dev 2>/dev/null \
         || git merge-base HEAD origin/main 2>/dev/null \
         || git merge-base HEAD main 2>/dev/null \
         || git rev-list --max-parents=0 HEAD 2>/dev/null | head -1 \
         || echo HEAD)"
  branch_files="$( {
    git diff --name-only "$base"..HEAD 2>/dev/null || true
    git diff --cached --name-only 2>/dev/null || true
    git diff --name-only 2>/dev/null || true
  } | sort -u | grep -v '^$' || true )"
  has_source=false
  if [ -n "$branch_files" ]; then
    while IFS= read -r f; do
      [ -n "$f" ] || continue
      if "$TDD_CHECK" is-source "$f"; then
        has_source=true
        break
      fi
    done <<<"$branch_files"
  fi
  if [ "$has_source" = "true" ]; then
    block tdd-backup-check "git commit refused (TDD backup): branch has source changes but no test changes. Stage at least one test file before committing. There is NO bypass."
  fi
fi

exit 0
