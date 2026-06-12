#!/usr/bin/env bash
# Decision-table tests for strict-mode and sourced-library policy.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECKER="$REPO_ROOT/scripts/check-shell-policy.sh"
MANIFEST="$REPO_ROOT/.claude/shell-policy.exceptions"

PASS=0
FAIL=0
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}
fail() {
  FAIL=$((FAIL + 1))
  printf '  FAIL %s\n' "$1" >&2
}

new_repo() {
  local name="$1"
  local repo="$TMP_DIR/$name"
  mkdir -p "$repo/.claude"
  git -C "$repo" init -q
  git -C "$repo" config user.name test
  git -C "$repo" config user.email test@example.com
  printf '%s\n' "$repo"
}

write_script() {
  local repo="$1"
  local path="$2"
  local options="$3"
  mkdir -p "$(dirname "$repo/$path")"
  cat >"$repo/$path" <<EOF
#!/usr/bin/env bash
$options
printf '%s\n' ok
EOF
  chmod +x "$repo/$path"
  git -C "$repo" add "$path"
}

run_case() {
  local name="$1"
  local repo="$2"
  local expected_status="$3"
  local expected_output="${4:-}"
  local output=""
  local status=0

  if output="$(
    SHELL_POLICY_ROOT="$repo" \
      SHELL_POLICY_MANIFEST="$repo/.claude/shell-policy.exceptions" \
      "$CHECKER" 2>&1
  )"; then
    status=0
  else
    status=$?
  fi

  if [ "$status" -eq "$expected_status" ]; then
    pass "$name exits $expected_status"
  else
    fail "$name exits $expected_status (got $status: $output)"
  fi

  if [ -n "$expected_output" ]; then
    if grep -qF "$expected_output" <<<"$output"; then
      pass "$name reports $expected_output"
    else
      fail "$name reports $expected_output (got: $output)"
    fi
  fi
}

echo "check-shell-policy:"

if [ -x "$CHECKER" ]; then
  pass "checker exists and is executable"
else
  fail "checker exists and is executable"
fi

if [ -x "$CHECKER" ]; then
  repo="$(new_repo clean)"
  write_script "$repo" clean.sh 'set -euo pipefail'
  : >"$repo/.claude/shell-policy.exceptions"
  run_case "clean standalone" "$repo" 0

  repo="$(new_repo bad-shebang)"
  printf '%s\n' '#!/bin/sh' 'set -euo pipefail' >"$repo/bad.sh"
  git -C "$repo" add bad.sh
  : >"$repo/.claude/shell-policy.exceptions"
  run_case "non-Bash shebang" "$repo" 1 "Bash shebang"

  repo="$(new_repo leaking-library)"
  write_script "$repo" lib/common.sh 'set -euo pipefail'
  printf 'library\tlib/common.sh\t-\tshared functions\n' >"$repo/.claude/shell-policy.exceptions"
  run_case "library option leak" "$repo" 1 "mutates caller options"

  repo="$(new_repo unclassified-aggregate)"
  write_script "$repo" aggregate.sh 'set -uo pipefail'
  : >"$repo/.claude/shell-policy.exceptions"
  run_case "unclassified aggregator" "$repo" 1 "standalone strict mode"

  repo="$(new_repo classified-aggregate)"
  write_script "$repo" aggregate.sh 'set -uo pipefail'
  printf 'aggregate\taggregate.sh\t-\tcollects failures\n' >"$repo/.claude/shell-policy.exceptions"
  run_case "classified aggregator" "$repo" 0

  repo="$(new_repo stale-manifest)"
  write_script "$repo" clean.sh 'set -euo pipefail'
  printf 'aggregate\tmissing.sh\t-\tstale row\n' >"$repo/.claude/shell-policy.exceptions"
  run_case "stale manifest" "$repo" 1 "stale manifest entry"

  repo="$(new_repo set-plus)"
  write_script "$repo" unsafe.sh $'set -euo pipefail\nset +e'
  : >"$repo/.claude/shell-policy.exceptions"
  run_case "unapproved option disable" "$repo" 1 "unapproved option disable"

  printf 'standalone\tunsafe.sh\te\tfixture exercises errexit disable\n' >"$repo/.claude/shell-policy.exceptions"
  run_case "approved option disable" "$repo" 0

  run_case "repository policy" "$REPO_ROOT" 0

  if bash -c '
    set +e +u
    set +o pipefail
    before_options="$(set +o)"
    before_trap="$(trap -p ERR)"
    source "$1"
    [[ "$(set +o)" == "$before_options" && "$(trap -p ERR)" == "$before_trap" ]]
  ' _ "$REPO_ROOT/.claude/hooks/lib/common.sh" \
    && bash -c '
      set +e +u
      set +o pipefail
      before_options="$(set +o)"
      before_trap="$(trap -p ERR)"
      source "$1"
      [[ "$(set +o)" == "$before_options" && "$(trap -p ERR)" == "$before_trap" ]]
    ' _ "$REPO_ROOT/deploy/scripts/common.sh"; then
    pass "sourced libraries preserve caller options and traps"
  else
    fail "sourced libraries preserve caller options and traps"
  fi

  fail_closed="$TMP_DIR/fail-closed.sh"
  cat >"$fail_closed" <<EOF
#!/usr/bin/env bash
set -euo pipefail
source "$REPO_ROOT/.claude/hooks/lib/common.sh"
enable_fail_closed_hook
false
EOF
  chmod +x "$fail_closed"
  if output="$("$fail_closed" 2>&1)"; then
    fail "opt-in hook failure exits 2"
  else
    status=$?
    if [ "$status" -eq 2 ] && grep -qF "failing closed" <<<"$output"; then
      pass "opt-in hook failure exits 2"
    else
      fail "opt-in hook failure exits 2 (got $status: $output)"
    fi
  fi
fi

if [ -f "$MANIFEST" ]; then
  pass "repository manifest exists"
else
  fail "repository manifest exists"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
