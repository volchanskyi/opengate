#!/usr/bin/env bash
# Enforces that the state files stay pointers, not a second copy of the ADRs.
#
# The ADR is the only home of a decision and its why. decisions.md is an index,
# phases.md is a ledger, techdebt.md is a register — each carries just enough
# text to let a reader choose a link. Left ungated these files grew to 322 KB of
# paraphrase: 85% of a decisions row's distinctive terms already appeared in the
# ADR it pointed at, but verbatim overlap was 5%, so the two copies drifted
# independently and no diff ever showed it.
#
# Caps are on prose. Links, ADR numbers, phase names, dates and table scaffolding
# do not count — a row's job is to say which decision this is and whether it
# still stands. See .claude/rules/plans-and-adrs.md.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DECISIONS="$REPO_ROOT/.claude/decisions.md"
PHASES="$REPO_ROOT/.claude/phases.md"
ADR_DIR="$REPO_ROOT/docs/adr"

DECISION_CAP=200
PHASE_CAP=300

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

# Prose length of a table cell: link targets stripped to their text, so a long
# path never counts against a row.
prose_length() {
  printf '%s' "$1" | sed -E 's/\[([^]]*)\]\([^)]*\)/\1/g' | LC_ALL=C.UTF-8 awk '{ print length($0) }'
}

echo "state index density:"

# --- Rule 1: decisions.md rows are capped, and cell 2 is the prose.
over=0
rows=0
while IFS= read -r line; do
  rows=$((rows + 1))
  decision="$(printf '%s' "$line" | cut -d'|' -f3)"
  length="$(prose_length "$decision")"
  if [ "$length" -gt "$DECISION_CAP" ]; then
    over=$((over + 1))
    number="$(printf '%s' "$line" | cut -d'|' -f2 | tr -d ' ')"
    fail "decisions.md row $number is $length characters of prose (cap $DECISION_CAP) — the why belongs in the ADR"
  fi
done < <(grep -E '^\| [0-9]{3} \|' "$DECISIONS" || true)

if [ "$rows" -eq 0 ]; then
  fail "decisions.md has no ADR rows — the index parser found nothing"
elif [ "$over" -eq 0 ]; then
  pass "all $rows decisions.md rows are within $DECISION_CAP characters of prose"
fi

# --- Rule 2: phases.md rows are capped on the same measure.
over=0
phase_rows=0
while IFS= read -r line; do
  case "$line" in
    '|---'* | '| Phase '*) continue ;;
  esac
  phase_rows=$((phase_rows + 1))
  summary="$(printf '%s' "$line" | cut -d'|' -f3)"
  length="$(prose_length "$summary")"
  if [ "$length" -gt "$PHASE_CAP" ]; then
    over=$((over + 1))
    name="$(printf '%s' "$line" | cut -d'|' -f2 | cut -c1-60)"
    fail "phases.md row '$name' is $length characters of prose (cap $PHASE_CAP) — link the plan and the ADR instead"
  fi
done < <(grep -E '^\| ' "$PHASES" || true)

if [ "$phase_rows" -eq 0 ]; then
  fail "phases.md has no rows — the ledger parser found nothing"
elif [ "$over" -eq 0 ]; then
  pass "all $phase_rows phases.md rows are within $PHASE_CAP characters of prose"
fi

# --- Rule 3: the index is complete in both directions.
# Nothing checked this before, so an ADR could ship with no row and a row could
# outlive its file, and neither was visible until somebody went looking.
missing_row=0
for adr in "$ADR_DIR"/ADR-*.md; do
  number="$(basename "$adr" | cut -c5-7)"
  count="$(grep -cE "^\| $number \|" "$DECISIONS" || true)"
  if [ "$count" -eq 0 ]; then
    missing_row=$((missing_row + 1))
    fail "$(basename "$adr") has no decisions.md row"
  elif [ "$count" -gt 1 ]; then
    missing_row=$((missing_row + 1))
    fail "$(basename "$adr") has $count decisions.md rows; an ADR gets exactly one"
  fi
done
[ "$missing_row" -eq 0 ] && pass "every ADR file has exactly one decisions.md row"

dangling=0
while IFS= read -r number; do
  # ADR-001 … ADR-012 live in the combined log rather than as per-file records.
  if [ "$number" -le 12 ] 2>/dev/null; then
    continue
  fi
  if ! ls "$ADR_DIR/ADR-$number-"*.md >/dev/null 2>&1; then
    dangling=$((dangling + 1))
    fail "decisions.md row $number resolves to no ADR file"
  fi
done < <(grep -oE '^\| [0-9]{3} \|' "$DECISIONS" | tr -d '| ' || true)

[ "$dangling" -eq 0 ] && pass "every decisions.md row resolves to an ADR"

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
