#!/usr/bin/env bash
# tdd-check.sh — shared classifier used by the Claude Code hooks that enforce
# the TDD mandate documented in CLAUDE.md.
#
# Subcommands:
#   is-source <path>      exit 0 if <path> is a source file (per project
#                         classifier — Go/Rust/TS/JS, excluding tests and
#                         generated files); exit 1 otherwise.
#   is-code <path>        exit 0 if <path> is a code file (Go/Rust/TS/JS,
#                         excluding only generated files — tests ARE included);
#                         exit 1 otherwise. Used by the PMAT B+ precommit gate
#                         (ADR-019 Amendment 1), which grades all changed code
#                         including tests, but never machine-generated output.
#   has-test-change       exit 0 if the current branch has any test-file
#                         change vs its merge-base with origin/dev
#                         (committed OR staged OR unstaged OR untracked);
#                         exit 1 if no test change exists.
#
# Used by .claude/hooks/pretooluse-tdd-gate.sh (PR 2 of the hooks rollout).
set -euo pipefail

SOURCE_EXT_RE='\.(go|rs|tsx?|jsx?)$'
TEST_RE='(_test\.(go|rs)$|\.test\.(ts|tsx|js|jsx)$|_spec\.(ts|tsx|js|jsx)$|(^|/)tests/|(^|/)test/|(^|/)__tests__/|(^|/)e2e/)'
GEN_RE='(openapi_gen\.go$|_gen\.go$|\.pb\.go$)'

is_source() {
  local path="$1"
  [[ "$path" =~ $SOURCE_EXT_RE ]] || return 1
  [[ "$path" =~ $TEST_RE ]] && return 1
  [[ "$path" =~ $GEN_RE ]] && return 1
  return 0
}

# is_code: like is_source but KEEPS test files. Only generated files are
# excluded. The PMAT precommit gate grades all changed code (tests included,
# per the ADR-019 Amendment 1 scope decision) but never machine-generated output,
# which is regenerated and not hand-maintainable to a grade floor.
is_code() {
  local path="$1"
  [[ "$path" =~ $SOURCE_EXT_RE ]] || return 1
  [[ "$path" =~ $GEN_RE ]] && return 1
  return 0
}

# Resolve the branch merge-base for diffing. Preference order is dev-first
# because all project work happens on dev (CLAUDE.md §Branching Rules).
# Final fallback is the repo's root commit so the function never errors out
# in a fresh repo without remotes.
resolve_base() {
  local ref
  for ref in origin/dev dev origin/main main; do
    if git rev-parse --verify --quiet "$ref" >/dev/null 2>&1; then
      if git merge-base HEAD "$ref" 2>/dev/null; then
        return 0
      fi
    fi
  done
  git rev-list --max-parents=0 HEAD 2>/dev/null | head -1
}

has_test_change() {
  local base
  base=$(resolve_base) || return 1
  [ -n "$base" ] || return 1

  local files
  files=$({
    git diff --name-only "$base"..HEAD 2>/dev/null || true
    git diff --cached --name-only 2>/dev/null || true
    git diff --name-only 2>/dev/null || true
    git ls-files --others --exclude-standard 2>/dev/null || true
  } | sort -u | grep -v '^$' || true)

  [ -n "$files" ] || return 1
  printf '%s\n' "$files" | grep -qE "$TEST_RE"
}

usage() {
  cat >&2 <<'EOF'
usage: tdd-check.sh <subcommand> [args]
  is-source <path>     exit 0 if <path> is a source file (excludes tests), 1 otherwise
  is-code <path>       exit 0 if <path> is a code file (includes tests), 1 otherwise
  has-test-change      exit 0 if the branch has any test-file change, 1 otherwise
EOF
  exit 2
}

main() {
  [ $# -ge 1 ] || usage
  local cmd="$1"
  shift
  case "$cmd" in
    is-source)
      [ $# -eq 1 ] || usage
      is_source "$1"
      ;;
    is-code)
      [ $# -eq 1 ] || usage
      is_code "$1"
      ;;
    has-test-change)
      [ $# -eq 0 ] || usage
      has_test_change
      ;;
    *)
      usage
      ;;
  esac
}

# Only run main if executed directly. Allow `source` for testing internals.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  main "$@"
fi
