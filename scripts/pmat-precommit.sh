#!/usr/bin/env bash
# Fails the commit when a changed code file grades below the PMAT TDG floor, B+ by default.
# Changed code is changed, staged, unstaged and untracked files minus gofmt-only Go test files.
#
# Environment:
#   PMAT_BIN           pmat binary (default: pmat)
#   PMAT_MIN_GRADE     grade floor (default: B+)
#   PMAT_BASELINE_REF  diff baseline ref (default: origin/dev)
#   PMAT_PIN           required pmat version (default: 3.17.0), empty disables the check
#   GOFMT_BIN          gofmt binary (default: gofmt)
#
# Exit codes:
#   0  all changed code meets the floor, or none changed
#   1  at least one changed file is below the floor
#   2  prerequisite missing (wrong or absent pmat)
set -uo pipefail

PMAT_BIN="${PMAT_BIN:-pmat}"
PMAT_MIN_GRADE="${PMAT_MIN_GRADE:-B+}"
PMAT_BASELINE_REF="${PMAT_BASELINE_REF:-origin/dev}"
# An explicitly empty PMAT_PIN disables the check; an unset one takes the default pin.
PMAT_PIN="${PMAT_PIN-3.17.0}"
GOFMT_BIN="${GOFMT_BIN:-gofmt}"

PMAT_PRECOMMIT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TDD_CHECK="$PMAT_PRECOMMIT_DIR/tdd-check.sh"

# pmat_version_ok is true when PMAT_BIN reports exactly the pinned version or the pin is disabled.
pmat_version_ok() {
  [ -z "$PMAT_PIN" ] && return 0
  command -v "$PMAT_BIN" >/dev/null 2>&1 || return 1
  [ "$("$PMAT_BIN" --version 2>/dev/null)" = "pmat $PMAT_PIN" ]
}

# pmat_resolve_base prints the merge-base of HEAD with the first available baseline ref.
# It falls back to the repo root commit when no baseline ref exists.
pmat_resolve_base() {
  local ref roots
  for ref in "$PMAT_BASELINE_REF" origin/dev dev origin/main main; do
    if git rev-parse --verify --quiet "$ref" >/dev/null 2>&1; then
      git merge-base HEAD "$ref" 2>/dev/null && return 0
    fi
  done
  # The root commit comes from a variable because `head` ends the pipe early and pipefail fails it.
  roots="$(git rev-list --max-parents=0 HEAD 2>/dev/null || true)"
  printf '%s\n' "${roots%%$'\n'*}"
}

# pmat_is_gofmt_only_test is true for a Go test file whose gofmt output equals its base's.
# A non-test file, a new file or a missing gofmt returns non-zero, so the file is graded.
pmat_is_gofmt_only_test() {
  local f="$1" base="$2"
  case "$f" in *_test.go) ;; *) return 1 ;; esac
  [ -n "$base" ] || return 1
  command -v "$GOFMT_BIN" >/dev/null 2>&1 || return 1
  git cat-file -e "$base:$f" 2>/dev/null || return 1
  local baseline_fmt current_fmt
  baseline_fmt="$(git show "$base:$f" 2>/dev/null | "$GOFMT_BIN" 2>/dev/null)" || return 1
  current_fmt="$("$GOFMT_BIN" "$f" 2>/dev/null)" || return 1
  [ "$baseline_fmt" = "$current_fmt" ]
}

# pmat_changed_code_files prints the changed code files that still exist, one per line.
pmat_changed_code_files() {
  local base
  base="$(pmat_resolve_base 2>/dev/null || true)"
  {
    [ -n "$base" ] && git diff --name-only "$base"..HEAD 2>/dev/null
    git diff --cached --name-only 2>/dev/null
    git diff --name-only 2>/dev/null
    git ls-files --others --exclude-standard 2>/dev/null
  } | sort -u | while IFS= read -r f; do
    [ -n "$f" ] || continue
    [ -f "$f" ] || continue
    "$TDD_CHECK" is-code "$f" || continue
    pmat_is_gofmt_only_test "$f" "$base" && continue
    printf '%s\n' "$f"
  done
}

# pmat_check_file returns 0 when the file meets the floor and prints the failing grades otherwise.
pmat_check_file() {
  local f="$1" out rc
  out="$("$PMAT_BIN" tdg check-quality -p "$f" \
    --min-grade "$PMAT_MIN_GRADE" --fail-on-violation --format json 2>/dev/null)"
  rc=$?
  [ "$rc" -eq 0 ] && return 0
  # check-quality prints a progress banner before the JSON object, so the slice starts at '{'.
  printf '%s' "$out" | sed -n '/^{/,$p' \
    | jq -r '.violations[]? | "    \(.path): grade \(.new_grade) (\(((.new_score // 0)*10|floor)/10))"' 2>/dev/null \
    || printf '    %s: below %s\n' "$f" "$PMAT_MIN_GRADE"
  return 1
}

pmat_precommit_main() {
  if ! pmat_version_ok; then
    {
      echo "✗ pmat $PMAT_PIN is required for the ADR-019 TDG gate (found: $("$PMAT_BIN" --version 2>/dev/null || echo 'not installed'))."
      echo "  Install the pinned version:  cargo install --locked --version $PMAT_PIN pmat"
    } >&2
    return 2
  fi

  local files
  files="$(pmat_changed_code_files)"
  if [ -z "$files" ]; then
    echo "✓ PMAT TDG gate: no changed code files to grade" >&2
    return 0
  fi

  local fail=0 count=0
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    count=$((count + 1))
    pmat_check_file "$f" || fail=1
  done <<<"$files"

  if [ "$fail" -ne 0 ]; then
    {
      echo "✗ PMAT TDG gate: changed code below $PMAT_MIN_GRADE (ADR-019)."
      echo "  Raise the grade (refactor) or record an ADR exception in the PR description."
      echo "  Inspect a file:  pmat tdg <file> --explain"
    } >&2
    return 1
  fi
  echo "✓ PMAT TDG gate: all $count changed code file(s) meet $PMAT_MIN_GRADE" >&2
  return 0
}

# Sourcing the file exposes the functions without running the gate.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  pmat_precommit_main
fi
