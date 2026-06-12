#!/usr/bin/env bash
# Canonical syntax, static-analysis, formatting, and test runner for Shell.

set -euo pipefail

SHELLCHECK_VERSION="0.11.0"
SHFMT_VERSION="3.13.1"
ROOT="${SHELL_QUALITY_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
POLICY_CHECKER="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/check-shell-policy.sh"

die() {
  printf 'shell-quality: ERROR: %s\n' "$1" >&2
  exit 1
}

tool_version() {
  local tool="$1"
  case "$tool" in
    shellcheck) shellcheck --version 2>/dev/null | awk '/^version:/ { print $2; exit }' ;;
    shfmt) shfmt --version 2>/dev/null | sed -n '1{s/^v//;p;}' ;;
  esac
}

require_tools() {
  command -v shellcheck >/dev/null 2>&1 \
    || die "ShellCheck ${SHELLCHECK_VERSION} is required; run scripts/install-shell-tools.sh"
  command -v shfmt >/dev/null 2>&1 \
    || die "shfmt ${SHFMT_VERSION} is required; run scripts/install-shell-tools.sh"
  [ "$(tool_version shellcheck)" = "$SHELLCHECK_VERSION" ] \
    || die "ShellCheck ${SHELLCHECK_VERSION} is required"
  [ "$(tool_version shfmt)" = "$SHFMT_VERSION" ] \
    || die "shfmt ${SHFMT_VERSION} is required"
}

tracked_scripts() {
  git -C "$ROOT" ls-files -z -- '*.sh'
}

changed_scripts() {
  local base="$1"
  git -C "$ROOT" diff --name-only --diff-filter=ACMR -z "$base" -- '*.sh'
}

read_files() {
  local mode="$1"
  local base="${2:-}"
  local relative
  FILES=()

  if [ "$mode" = "changed" ]; then
    while IFS= read -r -d '' relative; do
      [ -f "$ROOT/$relative" ] && FILES+=("$ROOT/$relative")
    done < <(changed_scripts "$base")
  else
    while IFS= read -r -d '' relative; do
      FILES+=("$ROOT/$relative")
    done < <(tracked_scripts)
  fi
}

check_files() {
  [ "${#FILES[@]}" -gt 0 ] || exit 0
  require_tools

  for file in "${FILES[@]}"; do
    bash -n "$file"
  done
  (
    cd "$ROOT"
    shellcheck --severity=style -x "${FILES[@]}"
    shfmt -d "${FILES[@]}"
  )
  SHELL_POLICY_ROOT="$ROOT" \
    SHELL_POLICY_MANIFEST="$ROOT/.claude/shell-policy.exceptions" \
    "$POLICY_CHECKER"
}

format_files() {
  require_tools
  read_files all
  [ "${#FILES[@]}" -gt 0 ] || exit 0
  (cd "$ROOT" && shfmt -w "${FILES[@]}")
}

run_tests() {
  local test_file
  local tests=()

  while IFS= read -r -d '' test_file; do
    tests+=("$ROOT/$test_file")
  done < <(
    git -C "$ROOT" ls-files -z -- 'scripts/tests/*.test.sh' 'deploy/tests/*.test.sh'
  )

  [ "${#tests[@]}" -gt 0 ] || die "no shell tests found"
  for test_file in "${tests[@]}"; do
    bash "$test_file"
  done
}

case "${1:-}" in
  check)
    read_files all
    check_files
    ;;
  changed)
    [ "$#" -eq 2 ] || die "usage: scripts/shell-quality.sh changed <base>"
    read_files changed "$2"
    check_files
    ;;
  format)
    format_files
    ;;
  test)
    run_tests
    ;;
  *)
    die "usage: scripts/shell-quality.sh {check|changed <base>|format|test}"
    ;;
esac
