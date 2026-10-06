#!/usr/bin/env bash
# Classifies files for the hooks that enforce the TDD mandate.
#
# Usage:
#   is-source <path>   exit 0 for a Go, Rust, TS or JS file that is neither a test nor generated
#   is-code <path>     exit 0 for a Go, Rust, TS or JS file that is not generated; tests count
#   has-test-change    exit 0 when the branch has a test-file change, committed or not
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

# Test files count as code; only generated output is excluded.
is_code() {
  local path="$1"
  [[ "$path" =~ $SOURCE_EXT_RE ]] || return 1
  [[ "$path" =~ $GEN_RE ]] && return 1
  return 0
}

# Prefers dev as the merge-base and falls back to the root commit in a repo without remotes.
resolve_base() {
  local ref roots
  for ref in origin/dev dev origin/main main; do
    if git rev-parse --verify --quiet "$ref" >/dev/null 2>&1; then
      if git merge-base HEAD "$ref" 2>/dev/null; then
        return 0
      fi
    fi
  done
  # A variable supplies the root commit because `head` would fail git's write under pipefail.
  roots="$(git rev-list --max-parents=0 HEAD 2>/dev/null || true)"
  printf '%s\n' "${roots%%$'\n'*}"
}

# Succeeds when every changed line of PATH sits inside the file's own `#[cfg(test)] mod` block.
# A file with no such block, or a diff reaching above it, answers no and stays a source change.
rust_change_is_inline_tests_only() {
  local base="$1" path="$2"
  [[ "$path" =~ \.rs$ ]] || return 1
  [ -f "$path" ] || return 1

  # The last `#[cfg(test)]` that opens a module, so a cfg(test) helper higher up is skipped.
  local marker
  marker=$(awk '
    /^[[:space:]]*#\[cfg\(test\)\]/ { attr = NR; next }
    attr && /^[[:space:]]*(pub[[:space:]]+)?mod[[:space:]]/ { line = attr }
    { attr = 0 }
    END { print line + 0 }
  ' "$path")
  [ "${marker:-0}" -gt 0 ] || return 1

  # Diffing the base against the working tree puts committed, staged and unstaged changes together.
  local hunks
  hunks=$(git diff -U0 "$base" -- "$path" 2>/dev/null | grep '^@@' || true)
  [ -n "$hunks" ] || return 1

  # Every hunk's new-side start must fall at or after the attribute; a pure deletion reports the
  # line it followed, so the same comparison holds.
  printf '%s\n' "$hunks" | awk -v m="$marker" '
    {
      plus = $3
      sub(/^\+/, "", plus)
      split(plus, n, ",")
      if (n[1] + 0 < m) { bad = 1 }
    }
    END { exit bad ? 1 : 0 }
  '
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
  if grep -qE "$TEST_RE" <<<"$files"; then
    return 0
  fi

  local rs
  while IFS= read -r rs; do
    [ -n "$rs" ] || continue
    rust_change_is_inline_tests_only "$base" "$rs" && return 0
  done <<<"$(printf '%s\n' "$files" | grep -E '\.rs$' || true)"
  return 1
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

# Runs only when executed directly; sourcing exposes the functions.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  main "$@"
fi
