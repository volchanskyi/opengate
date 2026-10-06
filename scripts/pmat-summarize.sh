#!/usr/bin/env bash
# Emits a one-line JSON row from the pmat repo-score and TDG check outputs and flags a regression.
# A regression is a repo score drop of REPO_SCORE_DROP_THRESHOLD or a rise in files below B+.
#
# Environment:
#   REPO_SCORE_JSON   the `pmat repo-score` JSON (default: repo-score.json)
#   TDG_CHECK_JSON    the `pmat tdg check-quality` JSON (default: tdg-check.json)
#   PREV_REPO_SCORE   previous repo_score; empty or "null" skips the score rule
#   PREV_BELOW_BPLUS  previous count of files below B+; empty or "null" skips the count rule
#   GITHUB_SHA        the commit tagged into the row
#
# Exit codes:
#   0  no regression
#   1  regression detected
#   2  input missing
set -uo pipefail

REPO_SCORE_JSON="${REPO_SCORE_JSON:-repo-score.json}"
TDG_CHECK_JSON="${TDG_CHECK_JSON:-tdg-check.json}"
REPO_SCORE_DROP_THRESHOLD="${REPO_SCORE_DROP_THRESHOLD:-3.0}"

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

build_row() {
  [[ -f "$REPO_SCORE_JSON" ]] || {
    echo "missing: $REPO_SCORE_JSON" >&2
    return 2
  }
  [[ -f "$TDG_CHECK_JSON" ]] || {
    echo "missing: $TDG_CHECK_JSON" >&2
    return 2
  }
  local rs tg
  rs="$(slice_json "$REPO_SCORE_JSON")"
  tg="$(slice_json "$TDG_CHECK_JSON")"
  jq -nc \
    --arg ts "$TIMESTAMP" \
    --arg sha "$COMMIT_SHA" \
    --argjson rs "$rs" \
    --argjson tg "$tg" \
    '{
      timestamp: $ts,
      commit: $sha,
      repo_score: ($rs.total_score // 0),
      repo_grade: ($rs.grade // "?"),
      below_bplus: (($tg.violations // []) | length),
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
    echo "REGRESSION_ALERT:⚠️ PMAT quality regression on dev"
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
