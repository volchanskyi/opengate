#!/usr/bin/env bash
# Measures again each Go benchmark the nightly flagged as slow, and fails only on a slowdown
# benchstat finds significant whose median is past the benchmark's line.
#
# Usage:
#   benchmark-confirm.sh rerun <flagged.tsv> <rerun.txt>
#   benchmark-confirm.sh judge <flagged.tsv> <window.txt> <rerun.txt>
#
# Environment:
#   BENCHMARK_CONFIRM_SUMMARY  where the verdict table is written (default benchmark-confirm.md)
#
# Exit codes:
#   0  no slowdown confirmed
#   1  a slowdown confirmed
#   2  an input, a measurement, or benchstat at its pin is missing
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/summary-table.sh
. "$HERE/lib/summary-table.sh"

# Ten samples a benchmark, the count Go's benchstat documentation asks for.
RERUN_COUNT=10
# The nights a comparison needs before it can fail the run.
MIN_NIGHTS=3
SUMMARY="${BENCHMARK_CONFIRM_SUMMARY:-benchmark-confirm.md}"
# Fences benchstat's own table on the page.
FENCE='~~~'

usage() {
  sed -n '5,7p' "${BASH_SOURCE[0]}" | sed 's/^# *//' >&2
  exit 2
}

rerun() {
  local flagged="$1" out="$2" names
  [ -f "$flagged" ] || {
    echo "missing: $flagged" >&2
    return 2
  }
  names="$(cut -f1 "$flagged" | paste -sd '|' -)"
  [ -n "$names" ] || {
    echo "nothing is flagged in $flagged" >&2
    return 2
  }
  # The nightly's own processor count, so the readings compare with the nights'.
  (cd "$HERE/../server" && go test -run '^$' -bench "^(${names})\$" -benchmem \
    -count="$RERUN_COUNT" -cpu 4 ./internal/...) >"$out"
}

# sec_op_rows prints name<TAB>new seconds<TAB>change<TAB>p for each row of benchstat's time table.
sec_op_rows() {
  awk -F, '
    NR == 2 && $2 != "sec/op" { exit }
    NR > 2 && $0 == "" { exit }
    NR > 2 && $1 != "geomean" { print $1 "\t" $4 "\t" $6 "\t" $7 }
  ' <<<"$1"
}

judge() {
  local flagged="$1" window="$2" rerun_file="$3" f
  for f in "$flagged" "$window" "$rerun_file"; do
    [ -f "$f" ] || {
      echo "missing: $f" >&2
      return 2
    }
  done
  "$HERE/require-tool.sh" benchstat >&2 || return 2

  local work
  work="$(mktemp -d)"
  # The re-run names carry the processor count, which the nights' lines leave out.
  sed -E 's/^(Benchmark[^[:space:]]*)-[0-9]+([[:space:]])/\1\2/' "$rerun_file" >"$work/re-run"
  cp "$window" "$work/nights"
  local csv text
  if ! csv="$(cd "$work" && benchstat -format csv nights re-run 2>/dev/null)"; then
    rm -rf "$work"
    echo "benchstat could not compare the nights with the re-run" >&2
    return 2
  fi
  text="$(cd "$work" && benchstat nights re-run 2>&1 || true)"
  rm -rf "$work"
  local rows
  rows="$(sec_op_rows "$csv")"

  local name line nights row seconds change p median verdict result confirmed=() table=()
  while IFS=$'\t' read -r name line nights; do
    [ -n "$name" ] || continue
    row="$(awk -F'\t' -v n="${name#Benchmark}" '$1 == n' <<<"$rows")"
    IFS=$'\t' read -r _ seconds change p <<<"$row"
    if [ -z "${seconds:-}" ]; then
      echo "the re-run did not measure $name" >&2
      return 2
    fi
    median="$(awk -v s="$seconds" 'BEGIN { printf "%.4g", s * 1e9 }')"
    if [ "$nights" -lt "$MIN_NIGHTS" ]; then
      verdict="reported only: $nights of $MIN_NIGHTS nights to compare with"
      result="—"
    elif [ -z "$change" ] || [ "$change" = "~" ] || [[ "$change" == -* ]]; then
      verdict="benchstat ${change:-~}, ${p}"
      result="pass"
    elif awk -v m="$median" -v l="$line" 'BEGIN { exit !(m > l) }'; then
      verdict="benchstat ${change}, ${p}"
      result="FAIL"
      confirmed+=("  • go/${name} ns_op: ${line} → ${median} (${verdict})")
    else
      verdict="benchstat ${change}, ${p}"
      result="pass"
    fi
    table+=("$(summary_table_row "go/${name} $(summary_term ns/op), measured $RERUN_COUNT times" \
      "≤ ${line} ns, where benchstat finds a change" "${median} ns (${verdict})" "$result")")
  done <"$flagged"

  {
    printf '### Benchmarks measured again\n\n'
    summary_table_header
    printf '%s\n' "${table[@]}"
    printf '\n%s\n%s\n%s\n' "$FENCE" "$text" "$FENCE"
  } >"$SUMMARY"

  if ((${#confirmed[@]})); then
    echo "REGRESSION_ALERT:⚠️ Benchmark regression on ${GITHUB_REF_NAME:-dev}, measured again"
    echo "REGRESSION_ALERT:"
    printf 'REGRESSION_ALERT:%s\n' "${confirmed[@]}"
    return 1
  fi
  return 0
}

main() {
  case "${1:-}" in
    rerun)
      [ "$#" -eq 3 ] || usage
      rerun "$2" "$3"
      ;;
    judge)
      [ "$#" -eq 4 ] || usage
      judge "$2" "$3" "$4"
      ;;
    *) usage ;;
  esac
}

main "$@"
