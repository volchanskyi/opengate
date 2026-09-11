#!/usr/bin/env bash
# Enforces that a completed phase's plan has been deleted.
#
# A plan is a working document. Once its implementation has landed, what it
# described lives in the code, in docs/ and in the ADRs, and the plan is a
# second, stale account of the same thing. Recurring failure: the
# implementation lands and the plan is left behind "for later".
#
# This gate ties retirement to the completion record: no row in the phases.md
# "Completed" section may link a plan at all. Recording a phase as done
# therefore forces its plan out of the tree. See .claude/rules/plans-and-adrs.md.

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

# A plan left in the working area with no phase recording it is the other half
# of the same miss, but an in-flight plan is the ordinary case, so this only
# reports what is there rather than failing on it.
if [ -d "$REPO_ROOT/.claude/plans" ]; then
  active="$(find "$REPO_ROOT/.claude/plans" -maxdepth 1 -name '*.md' | wc -l)"
  pass "$active plan(s) in flight under .claude/plans/"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
