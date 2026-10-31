#!/usr/bin/env bash
# Emits a JSON row from pmat's outputs and flags a regression: a file below B+ but generated code,
# a repo score under its floor, or a score drop or a rise in files below B+ since the night before.
#
# Environment:
#   REPO_SCORE_JSON   the `pmat repo-score` JSON (default: repo-score.json)
#   TDG_CHECK_JSON    the `pmat tdg check-quality` JSON (default: tdg-check.json)
#   PYTHON_SCORES     each Python file and its own score, a line each (default: python-scores.txt)
#   REPO_SCORE_FLOOR  the committed floor under the repo score (default 64.0)
#   PREV_REPO_SCORE   previous repo_score; empty or "null" skips the score rule
#   PREV_BELOW_BPLUS  previous count of files below B+; empty or "null" skips the count rule
#   GITHUB_SHA        the commit tagged into the row
#   GITHUB_REF_NAME   the branch the alert names (default: main)
#
# Exit codes:
#   0  no regression
#   1  regression detected
#   2  input missing
set -uo pipefail

REPO_SCORE_JSON="${REPO_SCORE_JSON:-repo-score.json}"
TDG_CHECK_JSON="${TDG_CHECK_JSON:-tdg-check.json}"
PYTHON_SCORES="${PYTHON_SCORES:-python-scores.txt}"
REPO_SCORE_DROP_THRESHOLD="${REPO_SCORE_DROP_THRESHOLD:-3.0}"
REPO_SCORE_FLOOR="${REPO_SCORE_FLOOR:-64.0}"
# pmat's check holds Python to B whatever floor it is asked for, so Python is held to B+ by score.
PYTHON_MIN=80
TDD_CHECK="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/tdd-check.sh"

COMMIT_SHA="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# slice_json prints the last JSON object in FILE after stripping ANSI escapes and the banner.
# check-quality emits the F-grade gate first and the min-grade gate last, which below_bplus counts.
slice_json() {
  python3 - "$1" <<'PY'
import json, re, sys
try:
    raw = open(sys.argv[1]).read()
except OSError:
    sys.exit(1)
raw = re.sub(r'\x1b\[[0-9;]*m', '', raw)   # ANSI colour escapes are stripped.
dec = json.JSONDecoder()
i, last = 0, None
while i < len(raw):
    j = raw.find('{', i)
    if j < 0:
        break
    try:
        obj, end = dec.raw_decode(raw[j:])
        last, i = obj, j + end
    except ValueError:
        i = j + 1
if last is None:
    sys.exit(1)
json.dump(last, sys.stdout)
PY
}

# below_bplus_files prints, one per line, each graded file below B+: check-quality's violations in
# code the commit check grades, generated code left out, then each Python file scoring under 80.
below_bplus_files() {
  local tg="$1" path score
  while IFS= read -r path; do
    [ -n "$path" ] || continue
    [[ "$path" == *.py ]] && continue
    "$TDD_CHECK" is-code "$path" && printf '%s\n' "$path"
  done <<<"$(jq -r '.violations[]?.path' <<<"$tg")"
  while read -r path score; do
    [ -n "$path" ] || continue
    awk -v s="$score" -v m="$PYTHON_MIN" 'BEGIN { exit !(s < m) }' && printf '%s\n' "$path"
  done <"$PYTHON_SCORES"
  return 0
}

build_row() {
  local input
  for input in "$REPO_SCORE_JSON" "$TDG_CHECK_JSON" "$PYTHON_SCORES"; do
    [[ -f "$input" ]] || {
      echo "missing: $input" >&2
      return 2
    }
  done
  local rs tg below
  rs="$(slice_json "$REPO_SCORE_JSON")"
  tg="$(slice_json "$TDG_CHECK_JSON")"
  below="$(below_bplus_files "$tg" | jq -Rsc 'split("\n") | map(select(length > 0))')"
  jq -nc \
    --arg ts "$TIMESTAMP" \
    --arg sha "$COMMIT_SHA" \
    --argjson rs "$rs" \
    --argjson below "$below" \
    '{
      timestamp: $ts,
      commit: $sha,
      repo_score: ($rs.total_score // 0),
      repo_grade: ($rs.grade // "?"),
      below_bplus: ($below | length),
      below_bplus_files: $below,
      categories: (($rs.categories // {}) | with_entries({ key: .key, value: (.value.percentage // 0) }))
    }' \
    || {
      echo "build_row: jq failed (malformed pmat JSON?)" >&2
      return 2
    }
}

regression_check() {
  local curr="$1"
  local curr_score curr_grade curr_below
  curr_score="$(jq -r '.repo_score' <<<"$curr")"
  curr_grade="$(jq -r '.repo_grade' <<<"$curr")"
  curr_below="$(jq -r '.below_bplus' <<<"$curr")"

  local regressed=0
  local alerts=()

  if ((curr_below > 0)); then
    alerts+=("Files below B+: $(jq -r '.below_bplus_files | join(", ")' <<<"$curr")")
    regressed=1
  fi

  if awk -v c="$curr_score" -v f="$REPO_SCORE_FLOOR" 'BEGIN { exit !(c < f) }'; then
    alerts+=("Repo-score ${curr_score} is under its committed floor ${REPO_SCORE_FLOOR}")
    regressed=1
  fi

  if [[ -n "${PREV_REPO_SCORE:-}" && "$PREV_REPO_SCORE" != "null" ]]; then
    if awk -v p="$PREV_REPO_SCORE" -v c="$curr_score" -v t="$REPO_SCORE_DROP_THRESHOLD" \
      'BEGIN { exit !((p - c) >= t) }'; then
      alerts+=("Repo-score dropped ${PREV_REPO_SCORE} → ${curr_score} (≥${REPO_SCORE_DROP_THRESHOLD}-pt drop)")
      regressed=1
    fi
  fi

  if [[ -n "${PREV_BELOW_BPLUS:-}" && "$PREV_BELOW_BPLUS" != "null" ]]; then
    if ((curr_below > PREV_BELOW_BPLUS)); then
      alerts+=("Files below B+ rose ${PREV_BELOW_BPLUS} → ${curr_below} (a file slipped below B+)")
      regressed=1
    fi
  fi

  if ((regressed)); then
    echo "REGRESSION_ALERT:⚠️ PMAT quality regression on ${GITHUB_REF_NAME:-main}"
    echo "REGRESSION_ALERT:"
    echo "REGRESSION_ALERT:  Repo score: ${PREV_REPO_SCORE:-n/a} → ${curr_score} (grade ${curr_grade})"
    local a
    for a in "${alerts[@]}"; do echo "REGRESSION_ALERT:  • ${a}"; done
    return 1
  fi
  return 0
}

main() {
  local row
  row="$(build_row)" || exit 2
  echo "$row"
  if regression_check "$row"; then return 0; else return 1; fi
}

main "$@"
