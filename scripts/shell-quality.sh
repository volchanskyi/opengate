#!/usr/bin/env bash
# Runs the shell syntax, static-analysis, formatting and test checks.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="${SHELL_QUALITY_ROOT:-$(cd "$HERE/.." && pwd)}"
POLICY_CHECKER="$HERE/check-shell-policy.sh"

die() {
  printf 'shell-quality: ERROR: %s\n' "$1" >&2
  exit 1
}

# require_tools checks ShellCheck and shfmt against their pins through require-tool.sh.
require_tools() {
  local tool
  for tool in shellcheck shfmt; do
    "$HERE/require-tool.sh" "$tool" || die "$tool is not the pinned version; see above"
  done
}

tracked_scripts() {
  git -C "$ROOT" ls-files -z -- '*.sh'
}

changed_scripts() {
  local base="$1"
  {
    git -C "$ROOT" diff --name-only --diff-filter=ACMR -z "$base" -- '*.sh'
    git -C "$ROOT" ls-files --others --exclude-standard -z -- '*.sh'
  }
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

# The files a CI step hands every command it runs; a test writing to one writes into the job.
STEP_FILES=(GITHUB_STEP_SUMMARY GITHUB_OUTPUT GITHUB_ENV GITHUB_PATH)

# run_tests runs every shell test, untracked ones included, each with fresh empty step files.
run_tests() {
  local test_file rel var step_dir
  local tests=() failed=()

  while IFS= read -r -d '' rel; do
    [ -f "$ROOT/$rel" ] && tests+=("$ROOT/$rel")
  done < <(
    git -C "$ROOT" ls-files -z --cached --others --exclude-standard -- \
      'scripts/tests/*.test.sh' 'deploy/tests/*.test.sh'
  )

  [ "${#tests[@]}" -gt 0 ] || die "no shell tests found"
  step_dir="$(mktemp -d)"
  for test_file in "${tests[@]}"; do
    rel="${test_file#"$ROOT/"}"
    # A test runs as an executable, so a file missing its executable bit fails here.
    if [ ! -x "$test_file" ]; then
      printf 'shell-quality: not executable: %s — chmod +x it\n' "$rel" >&2
      failed+=("$rel")
      continue
    fi
    for var in "${STEP_FILES[@]}"; do : >"$step_dir/$var"; done
    printf '▶ %s\n' "$rel"
    if ! GITHUB_STEP_SUMMARY="$step_dir/GITHUB_STEP_SUMMARY" \
      GITHUB_OUTPUT="$step_dir/GITHUB_OUTPUT" \
      GITHUB_ENV="$step_dir/GITHUB_ENV" \
      GITHUB_PATH="$step_dir/GITHUB_PATH" \
      "$test_file"; then
      failed+=("$rel")
    fi
    for var in "${STEP_FILES[@]}"; do
      if [ -s "$step_dir/$var" ]; then
        printf 'shell-quality: %s wrote into %s, which in CI is the job running it: %s\n' \
          "$rel" "$var" "$(sed -n 1p "$step_dir/$var")" >&2
        failed+=("$rel ($var)")
      fi
    done
  done
  rm -rf "$step_dir"

  if [ "${#failed[@]}" -gt 0 ]; then
    printf 'shell-quality: %d failed:\n' "${#failed[@]}" >&2
    printf '  %s\n' "${failed[@]}" >&2
    exit 1
  fi
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
