#!/usr/bin/env bash
# Fails when a row in the phases.md "Completed" section links a plan.
# See .claude/rules/plans-and-adrs.md.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PHASES="$REPO_ROOT/.claude/phases.md"

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

# The rows between "## Completed" and the next "## " heading.
completed_section() {
  awk '/^## Completed/ { f = 1; next } /^## / { f = 0 } f' "$PHASES"
}

echo "plans retirement:"

section="$(completed_section)"
if [ -z "$section" ]; then
  fail "phases.md has no ## Completed section"
else
  pass "phases.md ## Completed section found"
fi

mapfile -t plan_links < <(grep -oE '\]\([^)]*plans/[^)]+\.md\)' <<<"$section" | sort -u)

if [ "${#plan_links[@]}" -eq 0 ]; then
  pass "no completed phase links a plan"
else
  for link in "${plan_links[@]}"; do
    plan="${link#](}"
    plan="${plan%)}"
    fail "completed phase links a plan: $plan — delete the plan (git rm) in the commit that lands its work"
  done
fi

# In-flight plans are reported and never fail the run.
if [ -d "$REPO_ROOT/.claude/plans" ]; then
  active="$(find "$REPO_ROOT/.claude/plans" -maxdepth 1 -name '*.md' | wc -l)"
  pass "$active plan(s) in flight under .claude/plans/"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
