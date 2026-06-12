#!/usr/bin/env bash
# Tests for deterministic Markdown link enforcement.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECKER_PACKAGE="$REPO_ROOT/scripts/check-doc-links"
FIXTURES="$SCRIPT_DIR/fixtures/check-doc-links"

PASS=0
FAIL=0
FAILURES=()
TMP_DIR="$(mktemp -d)"
CHECKER="$TMP_DIR/check-doc-links"
trap 'rm -rf "$TMP_DIR"' EXIT

pass() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAILURES+=("$1"); printf '  FAIL %s\n' "$1" >&2; }

run_fixture() {
  local name="$1"
  local expected_status="$2"
  local expected_output="${3:-}"
  local output=""

  if output="$("$CHECKER" --root "$FIXTURES/$name" 2>&1)"; then
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

run_hook_case() {
  local name="$1"
  local fixture="$2"
  local envelope="$3"
  local expected_status="$4"
  local expected_output="${5:-}"
  local output=""

  if output="$(printf '%s' "$envelope" | "$CHECKER" --root "$FIXTURES/$fixture" --hook 2>&1)"; then
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

echo "check-doc-links:"

if [ ! -d "$CHECKER_PACKAGE" ]; then
  fail "checker package exists"
else
  pass "checker package exists"
fi

if [ -d "$CHECKER_PACKAGE" ] &&
  (cd "$REPO_ROOT" && GO111MODULE=off go build -o "$CHECKER" ./scripts/check-doc-links); then
  pass "checker builds without modules or network"
else
  fail "checker builds without modules or network"
  printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
  exit 1
fi

run_fixture ok 0
run_fixture broken-file 1 'docs/index.md:3: target does not exist'
run_fixture broken-anchor 1 'docs/index.md:3: heading anchor "#missing-heading" not found'
run_fixture broken-line 1 'docs/index.md:3: line anchor "#L4" exceeds 3 lines'
run_fixture active-plan-link 1 'links to active plan'
run_fixture active-plan-self-anchor 0
run_fixture archived-plan-link 0

if "$CHECKER" --root "$FIXTURES/broken-file" --write-baseline "$TMP_DIR/baseline.txt" >/dev/null 2>&1 &&
  "$CHECKER" --root "$FIXTURES/broken-file" --baseline "$TMP_DIR/baseline.txt" >/dev/null 2>&1; then
  pass "baseline suppresses an existing problem"
else
  fail "baseline suppresses an existing problem"
fi

run_hook_case \
  "hook rejects broken Write" \
  hook-overlay \
  '{"tool_name":"Write","tool_input":{"file_path":"docs/new.md","content":"# New\n\n[missing](missing.md)\n"}}' \
  1 \
  'docs/new.md:3: target does not exist'

run_hook_case \
  "hook accepts valid Write" \
  hook-overlay \
  '{"tool_name":"Write","tool_input":{"file_path":"docs/new.md","content":"# New\n\n[target](target.md#target)\n"}}' \
  0

run_hook_case \
  "hook catches inbound anchor break" \
  hook-overlay \
  '{"tool_name":"Edit","tool_input":{"file_path":"docs/target.md","old_string":"# Target","new_string":"# Renamed"}}' \
  1 \
  'docs/index.md:3: heading anchor "#target" not found'

run_hook_case \
  "hook allows pre-existing debt" \
  hook-existing \
  '{"tool_name":"Edit","tool_input":{"file_path":"docs/index.md","old_string":"# Index","new_string":"# Revised Index"}}' \
  0

run_hook_case \
  "hook rejects additional debt" \
  hook-existing \
  '{"tool_name":"Edit","tool_input":{"file_path":"docs/index.md","old_string":"[missing](missing.md)","new_string":"[missing](missing.md)\n[duplicate](missing.md)"}}' \
  1 \
  'docs/index.md:4: target does not exist'

if grep -qF 'run_check "doc links"' "$REPO_ROOT/scripts/precommit-gauntlet.sh" &&
  grep -qF -- '--baseline .claude/doc-link-baseline.txt' "$REPO_ROOT/scripts/precommit-gauntlet.sh"; then
  pass "gauntlet registers doc-link checker"
else
  fail "gauntlet registers doc-link checker"
fi

if python3 - "$REPO_ROOT/.claude/settings.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    settings = json.load(handle)

hooks = settings["hooks"]["PreToolUse"]
commands = [
    hook["command"]
    for group in hooks
    if group.get("matcher") == "Write|Edit|MultiEdit"
    for hook in group.get("hooks", [])
]
raise SystemExit(
    0 if ".claude/hooks/pretooluse-doc-link-check.sh" in commands else 1
)
PY
then
  pass "settings register doc-link hook"
else
  fail "settings register doc-link hook"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
