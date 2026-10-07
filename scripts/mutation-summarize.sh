#!/usr/bin/env bash
# Reads mutation-test outputs from the three languages, emits a canonical JSON-Lines row, runs the
# regression check and prints one Telegram-ready REGRESSION_ALERT payload on regression.
#
# Environment:
#   RUST_OUTCOMES       Rust outcomes file (default: agent/mutants.out/outcomes.json)
#   GO_REPORT           Go gremlins report (default: server/mutation-report.json)
#   WEB_REPORT          Stryker report (default: web/reports/mutation/mutation.json)
#   HISTORY_FILE        history rows file (default: docs/mutation-history.jsonl)
#   GITHUB_SHA          tagged into the canonical row
#   APPEND              1 appends the canonical row to HISTORY_FILE and rotates it to 90 days
#   MUTATION_LANGUAGES  legs to carry, space-separated (default: all three)
#
# Exit codes:
#   0  no regression
#   1  a language dropped more than 2pp from the previous row or scored under 85%
#   2  an input file is missing or unparseable

set -euo pipefail

RUST_OUTCOMES="${RUST_OUTCOMES:-agent/mutants.out/outcomes.json}"
GO_REPORT="${GO_REPORT:-server/mutation-report.json}"
WEB_REPORT="${WEB_REPORT:-web/reports/mutation/mutation.json}"
HISTORY_FILE="${HISTORY_FILE:-docs/mutation-history.jsonl}"

# A leg nobody asked for is absent from the row; a leg asked for and broken is exit 2.
MUTATION_LANGUAGES="${MUTATION_LANGUAGES-rust go web}"

REGRESSION_DROP_PP=2.0
REGRESSION_FLOOR_PCT=85.0
RETENTION_DAYS=90

COMMIT_SHA="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# parse_rust reads cargo-mutants outcomes.json; unviable mutants leave the denominator, and
# no_coverage is null because cargo-mutants counts no-coverage mutants as missed.
parse_rust() {
  local file="$1"
  [[ -f "$file" ]] || {
    echo "missing: $file" >&2
    return 2
  }
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
  ' "$file" || {
    echo "parse_rust failed on $file" >&2
    return 2
  }
}

# parse_go reads the gremlins JSON; not-covered mutants count toward the denominator, as in
# gremlins' own caught percentage.
parse_go() {
  local file="$1"
  [[ -f "$file" ]] || {
    echo "missing: $file" >&2
    return 2
  }
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
  ' "$file" || {
    echo "parse_go failed on $file" >&2
    return 2
  }
}

# parse_web aggregates the Stryker per-mutant statuses; CompileError mutants are excluded as a
# free kill by the TypeScript checker.
parse_web() {
  local file="$1"
  [[ -f "$file" ]] || {
    echo "missing: $file" >&2
    return 2
  }
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
  ' "$file" || {
    echo "parse_web failed on $file" >&2
    return 2
  }
}

# build_row emits the canonical row, with one entry per language in MUTATION_LANGUAGES.
build_row() {
  local language parsed scores='{}'

  if [[ -z "${MUTATION_LANGUAGES// /}" ]]; then
    echo "no language was named as complete, so there is no score to publish" >&2
    return 2
  fi

  # `|| return 2` is needed because build_row runs where set -e is suspended, so a parse failure
  # would otherwise fall through to the aggregating jq.
  for language in $MUTATION_LANGUAGES; do
    case "$language" in
      rust) parsed="$(parse_rust "$RUST_OUTCOMES")" || return 2 ;;
      go) parsed="$(parse_go "$GO_REPORT")" || return 2 ;;
      web) parsed="$(parse_web "$WEB_REPORT")" || return 2 ;;
      *)
        # An unknown language is an error; a silently narrower row would hide it.
        echo "unknown mutation language: $language" >&2
        return 2
        ;;
    esac
    scores="$(jq -nc --argjson scores "$scores" --arg l "$language" --argjson s "$parsed" \
      '$scores + {($l): $s}')" || return 2
  done

  jq -nc \
    --arg ts "$TIMESTAMP" \
    --arg sha "$COMMIT_SHA" \
    --argjson scores "$scores" \
    '{
      timestamp: $ts,
      commit: $sha,
      scores: $scores
    }'
}

# previous_row prints the last HISTORY_FILE row, or null when it is empty or missing.
previous_row() {
  [[ -f "$HISTORY_FILE" ]] || {
    echo "null"
    return 0
  }
  tail -n 1 "$HISTORY_FILE" 2>/dev/null || echo "null"
}

# regression_check returns 1 and prints REGRESSION_ALERT lines when a language regressed; with a
# null PREV_ROW only the absolute floor applies.
regression_check() {
  local curr="$1" prev="$2"
  local branch="${GITHUB_REF_NAME:-dev}"

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

    # Only the legs the row carries are judged, so a leg that did not run stays out of the alert.
    [ $curr.scores | keys[] ] as $languages
    | reduce $languages[] as $l ({};
        .[$l] = { curr: $curr.scores[$l].score_pct, prev: ($prev.scores[$l].score_pct // null) }
        | .[$l].regressed = regressed(.[$l].curr; .[$l].prev))
    | .any = ([ .[$languages[]].regressed ] | any)
    ')"

  local any
  any="$(jq -r '.any' <<<"$result")"

  if [[ "$any" == "true" ]]; then
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

      [ to_entries[] | select(.key != "any") | fmt(.key; .value) ] | join("\n")
    ' <<<"$result")"
    echo "REGRESSION_ALERT:⚠️ Mutation score regression on $branch"
    echo "REGRESSION_ALERT:"
    while IFS= read -r line; do
      echo "REGRESSION_ALERT:$line"
    done <<<"$lines"
    return 1
  fi
  return 0
}

# rotate_history drops rows older than RETENTION_DAYS from HISTORY_FILE through a temp file.
rotate_history() {
  [[ -f "$HISTORY_FILE" ]] || return 0
  local cutoff_epoch
  cutoff_epoch="$(date -u -d "${RETENTION_DAYS} days ago" +%s 2>/dev/null \
    || date -u -v-"${RETENTION_DAYS}d" +%s)"
  local tmp
  tmp="$(mktemp)"
  while IFS= read -r line; do
    local ts row_epoch
    ts="$(jq -r '.timestamp // empty' <<<"$line" 2>/dev/null || true)"
    if [[ -z "$ts" ]]; then continue; fi
    row_epoch="$(date -u -d "$ts" +%s 2>/dev/null || date -u -jf "%Y-%m-%dT%H:%M:%SZ" "$ts" +%s 2>/dev/null || echo 0)"
    if ((row_epoch >= cutoff_epoch)); then
      echo "$line" >>"$tmp"
    fi
  done <"$HISTORY_FILE"
  mv "$tmp" "$HISTORY_FILE"
}

main() {
  local row prev
  row="$(build_row)" || exit 2
  echo "$row"

  prev="$(previous_row)"

  if [[ "${APPEND:-0}" == "1" ]]; then
    rotate_history
    echo "$row" >>"$HISTORY_FILE"
  fi

  if regression_check "$row" "$prev"; then
    return 0
  else
    return 1
  fi
}

main "$@"
