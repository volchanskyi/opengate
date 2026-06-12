#!/usr/bin/env bash
# pmat-summarize.sh — ADR-019 integration point 3 (nightly analytics).
#
# Reads `pmat repo-score` + `pmat tdg check-quality` JSON outputs, emits a
# canonical single-line JSON row for Loki, runs the day-over-day regression
# check, and prints a Telegram-ready alert payload on regression.
#
# Mirrors scripts/mutation-summarize.sh. Invoked by .github/workflows/
# pmat-trend.yml after the pmat runs complete.
#
# Inputs (paths overridable via env for testing):
#   REPO_SCORE_JSON   default: repo-score.json   (`pmat repo-score --format json`)
#   TDG_CHECK_JSON    default: tdg-check.json     (`pmat tdg check-quality
#                                                   -p . --min-grade B+ --format json`)
# Previous-run values (fetched from Loki by the workflow; empty/"null" on the
# first run, which suppresses the day-over-day rules):
#   PREV_REPO_SCORE   previous repo_score
#   PREV_BELOW_BPLUS  previous count of files below B+
# Other env:
#   GITHUB_SHA        tagged into the canonical row
#
# Alert conditions (ADR-019 §"Integration point 3"):
#   - repo_score drop ≥ REPO_SCORE_DROP_THRESHOLD points day-over-day, OR
#   - a file newly below B+ since the previous run. We track this via the
#     below-B+ COUNT rising (a faithful, Loki-storable proxy for "any single
#     file slipped below B+"; the per-file enforcement lives in the C5
#     precommit gate). Recorded in ADR-019.
#
# Exit codes: 0 no regression · 1 regression detected · 2 input missing.
set -uo pipefail

REPO_SCORE_JSON="${REPO_SCORE_JSON:-repo-score.json}"
TDG_CHECK_JSON="${TDG_CHECK_JSON:-tdg-check.json}"
REPO_SCORE_DROP_THRESHOLD="${REPO_SCORE_DROP_THRESHOLD:-3.0}"

COMMIT_SHA="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# slice_json FILE — print exactly ONE clean JSON object from FILE.
#
# pmat's `check-quality` output is hostile to naive slicing: it prints a
# progress banner first, colours it with ANSI escapes that `--color never`
# does NOT suppress, AND (on `-p .`) emits the result object *twice*. A
# `sed '/^{/,$p'` grab therefore yields banner-free but DOUBLE JSON, which
# `jq --argjson` rejects as "invalid JSON text" (it wants a single value).
# This was the CI failure in pmat-trend run 26730207721.
#
# On `-p .` check-quality emits TWO result objects: an F-grade-cap gate first
# (violations = F-grade files) and the MIN-GRADE gate last (violations = files
# below B+ — what `below_bplus` must count). So: strip ANSI, then raw_decode
# every top-level object and keep the LAST one (the min-grade gate in pinned
# 3.17.0). repo-score's clean single object passes through unchanged.
slice_json() {
  python3 - "$1" <<'PY'
import json, re, sys
try:
    raw = open(sys.argv[1]).read()
except OSError:
    sys.exit(1)
raw = re.sub(r'\x1b\[[0-9;]*m', '', raw)   # strip ANSI colour escapes
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

# build_row → canonical JSON row for the current run.
build_row() {
  [[ -f "$REPO_SCORE_JSON" ]] || { echo "missing: $REPO_SCORE_JSON" >&2; return 2; }
  [[ -f "$TDG_CHECK_JSON"  ]] || { echo "missing: $TDG_CHECK_JSON"  >&2; return 2; }
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
    || { echo "build_row: jq failed (malformed pmat JSON?)" >&2; return 2; }
}

# regression_check CURR_ROW → exit 1 + REGRESSION_ALERT lines if regressed.
regression_check() {
  local curr="$1"
  local curr_score curr_grade curr_below
  curr_score="$(jq -r '.repo_score'  <<<"$curr")"
  curr_grade="$(jq -r '.repo_grade'  <<<"$curr")"
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
    if (( curr_below > PREV_BELOW_BPLUS )); then
      alerts+=("Files below B+ rose ${PREV_BELOW_BPLUS} → ${curr_below} (a file slipped below B+)")
      regressed=1
    fi
  fi

  if (( regressed )); then
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
