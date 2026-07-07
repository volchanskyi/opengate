#!/usr/bin/env bash
# Enforces that a completed phase's micro-plan has been archived.
#
# Recurring failure: a workstream's implementation lands but its plan file is
# left in the active .claude/plans/ dir instead of plans/archive/. This gate ties
# archival to the completion record: every plan LINK in the phases.md "Completed"
# section must point under plans/archive/. Recording a phase as done therefore
# forces its plan into archive/ (and check-doc-links then forces the file to
# exist there). See .claude/rules/plans-and-adrs.md.

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

echo "plans archive consistency:"

section="$(completed_section)"
if [ -z "$section" ]; then
  fail "phases.md has no ## Completed section"
else
  pass "phases.md ## Completed section found"
fi

# Every plan link in the Completed section must resolve under plans/archive/.
# A bare plans/<name>.md link means the plan was recorded as done but never
# archived — the exact miss this gate exists to catch.
mapfile -t plan_links < <(printf '%s\n' "$section" | grep -oE '\]\(plans/[^)]+\.md\)' | sort -u)

if [ "${#plan_links[@]}" -eq 0 ]; then
  pass "no completed-phase plan links to check"
else
  unarchived=()
  for link in "${plan_links[@]}"; do
    case "$link" in
      "](plans/archive/"*) : ;;
      *) unarchived+=("$link") ;;
    esac
  done
  if [ "${#unarchived[@]}" -eq 0 ]; then
    pass "all ${#plan_links[@]} completed-phase plan links point under plans/archive/"
  else
    for link in "${unarchived[@]}"; do
      plan="${link#](plans/}"
      plan="${plan%)}"
      fail "completed phase links a non-archived plan: $plan — archive it (git mv to plans/archive/, bump links one ../ deeper, repoint refs)"
    done
  fi
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
