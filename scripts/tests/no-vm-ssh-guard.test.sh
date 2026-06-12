#!/usr/bin/env bash
# Tests for scripts/no-vm-ssh-guard.sh.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GUARD="$REPO_ROOT/scripts/no-vm-ssh-guard.sh"

PASS=0
FAIL=0
FAILURES=()

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}
fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}

run_positive_case() {
  local name="$1"
  local content="$2"
  local fixture_root output

  fixture_root="$(mktemp -d)"
  mkdir -p "$fixture_root/.github/workflows"
  printf '%s\n' "$content" >"$fixture_root/.github/workflows/legacy.yml"

  if output="$("$GUARD" "$fixture_root" 2>&1)"; then
    fail "$name is rejected"
  elif grep -qF '.github/workflows/legacy.yml:1:' <<<"$output"; then
    pass "$name is rejected with its path"
  else
    fail "$name reports its offending path"
  fi

  rm -rf "$fixture_root"
}

echo "no-vm-ssh-guard:"

if [ ! -x "$GUARD" ]; then
  fail "guard exists and is executable"
else
  pass "guard exists and is executable"

  run_positive_case "SSH composite action" "uses: ./.github/actions/oci-ssh-setup"
  run_positive_case "SSH host alias" "run: ssh deploy-target uptime"
  run_positive_case "legacy Loki transport" "LOKI_PUSH_MODE: ssh-docker"
  run_positive_case "legacy deploy root" "DEPLOY_DIR: /opt/opengate"
  run_positive_case "cutover gate" 'if: vars.K8S_CUTOVER == "true"'
  # shellcheck disable=SC2016
  run_positive_case "SSH-only secret" 'key: ${{ secrets.DEPLOY_SSH_PRIVATE_KEY }}'

  scope_root="$(mktemp -d)"
  mkdir -p "$scope_root/docs"
  printf '%s\n' "Historical note: ssh deploy-target used the old VM." >"$scope_root/docs/history.md"
  if "$GUARD" "$scope_root" >/dev/null 2>&1; then
    pass "documentation history is outside guard scope"
  else
    fail "documentation history is outside guard scope"
  fi
  rm -rf "$scope_root"

  if "$GUARD" "$REPO_ROOT" >/dev/null 2>&1; then
    pass "repository contains no retired VM SSH path"
  else
    fail "repository contains no retired VM SSH path"
  fi
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
