#!/usr/bin/env bash
# Reads mutation-test outputs from all three languages, emits a canonical
# JSON-Lines row, runs the regression check, and prints a Telegram-ready
# alert payload.
#
# Invoked by .github/workflows/mutation.yml after the matrix runs complete.
# Per the PR 9 plan (mutation testing as observability):
# .claude/plans/pr9-mutation-testing-as-observability.md
#
# Inputs (file paths can be overridden via env vars for testing):
#   RUST_OUTCOMES   default: agent/mutants.out/outcomes.json
#   GO_REPORT       default: server/mutation-report.json
#   WEB_REPORT      default: web/reports/mutation/mutation.json
#   HISTORY_FILE    default: docs/mutation-history.jsonl
#
# Behavior controlled by env vars:
#   GITHUB_SHA      tagged into the canonical row
#   APPEND=1        append the canonical row to HISTORY_FILE (and rotate to 90d)
#
# Outputs to stdout:
#   - the canonical row as a single JSON object
#   - on regression: a separate line starting with "REGRESSION_ALERT:" containing
#     the Telegram-ready text (one alert payload per run, never multiple)
#
# Exit codes:
#   0  no regression detected
#   1  regression detected (per-language: drop >2pp from previous OR score <85%)
#   2  input file missing or unparseable
#
# Score definition (matches PR 6/7/8 conventions):
#   Rust  = (caught + timeout) / (caught + missed + timeout)         [unviable excluded]
#   Go    = mutants_killed / (mutants_killed + mutants_lived + mutants_not_covered)
#   Web   = (killed + timeout) / (killed + survived + timeout + no_coverage)

set -euo pipefail

# --- Configuration ------------------------------------------------------------

RUST_OUTCOMES="${RUST_OUTCOMES:-agent/mutants.out/outcomes.json}"
GO_REPORT="${GO_REPORT:-server/mutation-report.json}"
WEB_REPORT="${WEB_REPORT:-web/reports/mutation/mutation.json}"
HISTORY_FILE="${HISTORY_FILE:-docs/mutation-history.jsonl}"

REGRESSION_DROP_PP=2.0       # alert when score drops by more than this from prev
REGRESSION_FLOOR_PCT=85.0    # alert when absolute score crosses below this floor
RETENTION_DAYS=90            # rolling window for HISTORY_FILE rotation

COMMIT_SHA="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# --- Parsers ------------------------------------------------------------------

# parse_rust FILE → JSON object {killed, survived, no_coverage, total, score_pct}
# Uses cargo-mutants outcomes.json; "unviable" mutants are excluded from the
# denominator (they couldn't even compile and aren't real test signal).
#
# Note: cargo-mutants does NOT distinguish "no coverage" from "missed" — both
# end up in `.missed`. The JSON keeps the field for canonical-row shape
# consistency across languages, but it is encoded as null (not 0) so downstream
# consumers (Grafana, summary tables) can render it as "—" / "n/a" instead of
# misreading it as "0 mutants without coverage".
parse_rust() {
  local file="$1"
  [[ -f "$file" ]] || { echo "missing: $file" >&2; return 2; }
  jq -e '
    {
      killed:      (.caught   // 0),
      survived:    (.missed   // 0),
      timeout:     (.timeout  // 0),
      no_coverage: null,
      unviable:    (.unviable // 0),
      total:       ((.caught // 0) + (.missed // 0) + (.timeout // 0))
    }
    | .score_pct = (
        if .total == 0 then 0
        else (((.killed + .timeout) * 1000 / .total | floor) / 10)
        end)
    | { killed, survived, timeout, no_coverage, unviable, total, score_pct }
  ' "$file" || { echo "parse_rust failed on $file" >&2; return 2; }
}

# parse_go FILE → JSON object {killed, survived, no_coverage, total, score_pct}
# Uses gremlins --output JSON. Per PR 7 convention NOT_COVERED counts toward
# the denominator (gremlins' own "% caught" includes NOT COVERED).
parse_go() {
  local file="$1"
  [[ -f "$file" ]] || { echo "missing: $file" >&2; return 2; }
  jq -e '
    {
      killed:      (.mutants_killed     // 0),
      survived:    (.mutants_lived      // 0),
      timeout:     0,
      no_coverage: (.mutants_not_covered // 0),
      unviable:    (.mutants_not_viable // 0),
      total:       ((.mutants_killed // 0) + (.mutants_lived // 0) + (.mutants_not_covered // 0))
    }
    | .score_pct = (
        if .total == 0 then 0
        else ((.killed * 1000 / .total | floor) / 10)
        end)
    | { killed, survived, timeout, no_coverage, unviable, total, score_pct }
  ' "$file" || { echo "parse_go failed on $file" >&2; return 2; }
}

# parse_web FILE → JSON object {killed, survived, no_coverage, total, score_pct}
# Uses Stryker JSON reporter. The reporter writes a per-mutant array; we
# aggregate status counts. CompileError mutants are excluded (free kill by the
# TS checker; not real test signal). Matches PR 8 score convention.
parse_web() {
  local file="$1"
  [[ -f "$file" ]] || { echo "missing: $file" >&2; return 2; }
  jq -e '
    [ .files | to_entries[] | .value.mutants[] | .status ] as $statuses
    | {
        killed:      ($statuses | map(select(. == "Killed"))      | length),
        survived:    ($statuses | map(select(. == "Survived"))    | length),
        timeout:     ($statuses | map(select(. == "Timeout"))     | length),
        no_coverage: ($statuses | map(select(. == "NoCoverage")) | length),
        unviable:    ($statuses | map(select(. == "CompileError")) | length)
      }
    | .total = (.killed + .survived + .timeout + .no_coverage)
    | .score_pct = (
        if .total == 0 then 0
        else (((.killed + .timeout) * 1000 / .total | floor) / 10)
        end)
    | { killed, survived, timeout, no_coverage, unviable, total, score_pct }
  ' "$file" || { echo "parse_web failed on $file" >&2; return 2; }
}

# --- Aggregator ---------------------------------------------------------------

# build_row → canonical JSON object for the current run
build_row() {
  local rust go web
  rust="$(parse_rust "$RUST_OUTCOMES")"
  go="$(parse_go "$GO_REPORT")"
  web="$(parse_web "$WEB_REPORT")"

  jq -nc \
    --arg ts "$TIMESTAMP" \
    --arg sha "$COMMIT_SHA" \
    --argjson rust "$rust" \
    --argjson go "$go" \
    --argjson web "$web" \
    '{
      timestamp: $ts,
      commit: $sha,
      scores: { rust: $rust, go: $go, web: $web }
    }'
}

# --- Regression check ---------------------------------------------------------

# previous_row → last row from HISTORY_FILE, or null if empty/missing
previous_row() {
  [[ -f "$HISTORY_FILE" ]] || { echo "null"; return 0; }
  tail -n 1 "$HISTORY_FILE" 2>/dev/null || echo "null"
}

# regression_check CURR_ROW PREV_ROW → exit 1 if any language regressed,
# also prints "REGRESSION_ALERT:" followed by Telegram-ready text on regression.
# When PREV_ROW is null (first run), only the absolute-floor rule applies.
regression_check() {
  local curr="$1" prev="$2"

  local result
  result="$(jq -nc \
    --argjson curr "$curr" \
    --argjson prev "$prev" \
    --argjson drop "$REGRESSION_DROP_PP" \
    --argjson floor "$REGRESSION_FLOOR_PCT" \
    '
    def regressed(c; p):
      if c == null then false
      else
        (c < $floor)
        or (p != null and (p - c) > $drop)
      end;

    {
      rust: { curr: $curr.scores.rust.score_pct, prev: ($prev.scores.rust.score_pct // null) },
      go:   { curr: $curr.scores.go.score_pct,   prev: ($prev.scores.go.score_pct   // null) },
      web:  { curr: $curr.scores.web.score_pct,  prev: ($prev.scores.web.score_pct  // null) }
    }
    | .rust.regressed = regressed(.rust.curr; .rust.prev)
    | .go.regressed   = regressed(.go.curr;   .go.prev)
    | .web.regressed  = regressed(.web.curr;  .web.prev)
    | .any = (.rust.regressed or .go.regressed or .web.regressed)
    ')"

  local any
  any="$(jq -r '.any' <<< "$result")"

  if [[ "$any" == "true" ]]; then
    # Build Telegram-friendly text
    local lines
    lines="$(jq -r '
      def fmt(lang; row):
        if row.regressed
          then "  \(lang | ascii_upcase): \(row.prev // "n/a") → \(row.curr)" +
               (if row.prev == null then " (below floor)"
                elif (row.curr < 85.0) then " (below 85% floor)"
                else " (drop > 2pp)" end)
          else "  \(lang | ascii_upcase): \(row.prev // "n/a") → \(row.curr)"
          end;

      [fmt("rust"; .rust), fmt("go"; .go), fmt("web"; .web)] | join("\n")
    ' <<< "$result")"
    echo "REGRESSION_ALERT:⚠️ Mutation score regression on dev"
    echo "REGRESSION_ALERT:"
    while IFS= read -r line; do
      echo "REGRESSION_ALERT:$line"
    done <<< "$lines"
    return 1
  fi
  return 0
}

# --- Rotation -----------------------------------------------------------------

# rotate_history → drop rows older than RETENTION_DAYS from HISTORY_FILE.
# Called only when APPEND=1. Stable: in-place via temp file.
rotate_history() {
  [[ -f "$HISTORY_FILE" ]] || return 0
  local cutoff_epoch
  cutoff_epoch="$(date -u -d "${RETENTION_DAYS} days ago" +%s 2>/dev/null \
    || date -u -v-"${RETENTION_DAYS}d" +%s)"
  local tmp
  tmp="$(mktemp)"
  while IFS= read -r line; do
    local ts row_epoch
    ts="$(jq -r '.timestamp // empty' <<< "$line" 2>/dev/null || true)"
    if [[ -z "$ts" ]]; then continue; fi
    row_epoch="$(date -u -d "$ts" +%s 2>/dev/null || date -u -jf "%Y-%m-%dT%H:%M:%SZ" "$ts" +%s 2>/dev/null || echo 0)"
    if (( row_epoch >= cutoff_epoch )); then
      echo "$line" >> "$tmp"
    fi
  done < "$HISTORY_FILE"
  mv "$tmp" "$HISTORY_FILE"
}

# --- Main ---------------------------------------------------------------------

main() {
  local row prev
  row="$(build_row)" || exit 2
  echo "$row"

  prev="$(previous_row)"

  if [[ "${APPEND:-0}" == "1" ]]; then
    rotate_history
    echo "$row" >> "$HISTORY_FILE"
  fi

  if regression_check "$row" "$prev"; then
    return 0
  else
    return 1
  fi
}

main "$@"
