#!/usr/bin/env bash
# Builds benchmark rows from Go -benchmem and Criterion output and gates allocations, writing
# out each Go timing past its line for scripts/benchmark-confirm.sh to measure again.
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/vm-query.sh
. "$SCRIPT_DIR/lib/vm-query.sh"
# shellcheck source=scripts/lib/summary-table.sh
. "$SCRIPT_DIR/lib/summary-table.sh"

# Where the run's summary page is written: each reading beside what it is held
# to, and what the check made of it.
BENCHMARK_SUMMARY_FILE="${BENCHMARK_SUMMARY_FILE:-benchmark-summary.md}"

# Where each Go timing past its line goes for the re-run: name, line and night count, and the
# nights' readings as Go benchmark lines.
BENCHMARK_FLAGGED_FILE="${BENCHMARK_FLAGGED_FILE:-benchmark-flagged.tsv}"
BENCHMARK_WINDOW_FILE="${BENCHMARK_WINDOW_FILE:-benchmark-window.txt}"

GO_BENCH_FILE="${GO_BENCH_FILE:-bench-go.txt}"
CRITERION_ROOT="${CRITERION_ROOT:-agent/target/criterion}"
BASELINE_FILE="${BASELINE_FILE:-benchmarks/baseline.json}"
COMMIT_SHA="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# ns/op window-gate constants, calibrated from measured run-to-run variance.
NS_WINDOW_DAYS=14   # < 30d VM retention; the 14 dates before tonight's
NS_REL_TOL=0.50     # regress if ns/op > window median × (1 + this)
NS_ABS_CEIL_TOL=1.0 # regress if ns/op > committed-baseline ns × (1 + this)
# Fewer dates skip the relative rule, and hold a row with no committed numbers ungated.
NS_MIN_WINDOW_SAMPLES=3

parse_go_bench() {
  local file="$1"
  [[ -f "$file" ]] || {
    echo "missing: $file" >&2
    return 2
  }

  awk '
    /^Benchmark/ {
      name=$1
      sub(/-[0-9]+$/, "", name)
      ns="null"
      bytes="null"
      allocs="null"
      for (i=1; i<=NF; i++) {
        if ($i == "ns/op") ns=$(i-1)
        if ($i == "B/op") bytes=$(i-1)
        if ($i == "allocs/op") allocs=$(i-1)
      }
      if (ns != "null") {
        printf "%s\tgo\t%s\t%s\t%s\n", name, ns, bytes, allocs
      }
    }
  ' "$file"
}

parse_criterion() {
  local root="$1"
  [[ -d "$root" ]] || {
    echo "missing: $root" >&2
    return 2
  }

  local found=0
  local file name ns
  while IFS= read -r file; do
    found=1
    name="$(basename "$(dirname "$(dirname "$file")")")"
    ns="$(jq -r '.mean.point_estimate // empty' "$file")" || return 2
    [[ -n "$ns" ]] || {
      echo "missing mean.point_estimate in $file" >&2
      return 2
    }
    printf '%s\trust\t%s\tnull\tnull\n' "$name" "$ns"
  done < <(find "$root" -path '*/new/estimates.json' -type f | sort)

  if [[ "$found" -eq 0 ]]; then
    echo "no Criterion estimates found under $root" >&2
    return 2
  fi
}

build_rows() {
  local go_tsv criterion_tsv
  go_tsv="$(parse_go_bench "$GO_BENCH_FILE")" || return 2
  criterion_tsv="$(parse_criterion "$CRITERION_ROOT")" || return 2
  rows_from_tsv "${go_tsv}"$'\n'"${criterion_tsv}"
}

# Turns parsed name<TAB>lang<TAB>ns<TAB>bytes<TAB>allocs lines into the canonical row array.
rows_from_tsv() {
  local tsv="$1"
  jq -Rsc \
    --arg commit "$COMMIT_SHA" \
    --arg timestamp "$TIMESTAMP" \
    '
      split("\n")
      | map(select(length > 0) | split("\t") | {
          name: .[0],
          lang: .[1],
          ns_op: (.[2] | tonumber),
          bytes_op: (if .[3] == "null" then null else (.[3] | tonumber) end),
          allocs_op: (if .[4] == "null" then null else (.[4] | tonumber) end),
          commit: $commit,
          env: "ci",
          timestamp: $timestamp
        })
      | sort_by(.lang, .name)
    ' <<<"$tsv" || {
    echo "rows_from_tsv: jq failed" >&2
    return 2
  }
}

# Writes a baseline from tonight's rows, keeping each row's history_from from BASELINE_FILE.
build_baseline() {
  local rows="$1" kept='{}'
  if [[ -f "$BASELINE_FILE" ]]; then
    kept="$(jq -c '[.benchmarks[]? | select(.history_from != null)
      | {key: (.lang + "/" + .name), value: .history_from}] | from_entries' "$BASELINE_FILE")" || {
      echo "baseline is malformed: $BASELINE_FILE" >&2
      return 2
    }
  fi
  jq -c --arg generated_at "$TIMESTAMP" --argjson kept "$kept" '
    {
      version: 1,
      generated_at: $generated_at,
      default_tolerances: { allocs_op: 0.02, bytes_op: 0.02 },
      benchmarks: [
        .[] | {name, lang, ns_op, bytes_op, allocs_op}
        + (($kept[.lang + "/" + .name]) as $from | if $from then {history_from: $from} else {} end)
      ]
    }
  ' <<<"$rows"
}

# nightly_json METRIC prints the window's nightly readings of METRIC as [{lang,name,date,value}].
# A store that cannot be read gives [], which leaves only the committed limits.
nightly_json() {
  local lines
  lines="$(vm_query_nightly "$1" 'env="ci"' "$NS_WINDOW_DAYS")" || lines=""
  jq -Rnc '
    [ inputs | split("\t") | select(length >= 3)
      | (.[0] | split(",") | map(split("=") | {key: .[0], value: (.[1:] | join("="))})
         | from_entries) as $labels
      | select($labels.benchmark != null and $labels.lang != null)
      | {lang: $labels.lang, name: $labels.benchmark, date: .[1], value: (.[2] | tonumber)} ]
  ' <<<"$lines" 2>/dev/null || printf '[]'
}

# benchmark_limits prints one object a row: each metric's reading beside the limit it is held to
# and where that limit came from. Every gate and the summary page read these objects.
benchmark_limits() {
  local rows="$1" baseline="$2" ns="$3" allocs="$4" bytes="$5"
  jq -c \
    --argjson rows "$rows" \
    --argjson ns "$ns" --argjson allocs "$allocs" --argjson bytes "$bytes" \
    --argjson reltol "$NS_REL_TOL" --argjson ceiltol "$NS_ABS_CEIL_TOL" \
    --argjson minn "$NS_MIN_WINDOW_SAMPLES" '
    def median:
      sort
      | if length == 0 then null
        elif length % 2 == 1 then .[(length - 1) / 2]
        else (.[length / 2 - 1] + .[length / 2]) / 2 end;
    def nights($all; $row; $from):
      [$all[] | select(.lang == $row.lang and .name == $row.name and ($from == null or .date >= $from))]
      | sort_by(.date) | map(.value);
    def held($reading; $committed; $values; $tol; $from):
      if $reading == null then null
      elif $committed != null then
        {reading: $reading, limit: ($committed * (1 + $tol)), basis: "baseline", base: $committed, tol: $tol}
      elif ($values | length) >= $minn then
        ($values | median) as $m
        | {reading: $reading, limit: ($m * (1 + $tol)), basis: "nights", base: $m, tol: $tol}
      elif $from != null then
        {reading: $reading, limit: null, basis: "waiting", count: ($values | length), from: $from}
      else {reading: $reading, limit: null, basis: "none"} end;
    . as $baseline
    | ($baseline.default_tolerances // {}) as $defaults
    | [ $rows[] as $row
        | ([$baseline.benchmarks[]? | select(.name == $row.name and .lang == $row.lang)] | first) as $base
        | ($base.history_from // null) as $from
        | nights($ns; $row; $from) as $nsv
        | ($nsv | length) as $count
        | (if $count >= $minn then ($nsv | median) * (1 + $reltol) else null end) as $band
        | (if $base.ns_op != null then $base.ns_op * (1 + $ceiltol) else null end) as $ceiling
        | ([$band, $ceiling] | map(select(. != null)) | min) as $line
        | {
            lang: $row.lang,
            name: $row.name,
            from: $from,
            ns: {
              reading: $row.ns_op, nights: $nsv, count: $count, median: ($nsv | median),
              band: $band, ceiling: $ceiling, base: $base.ns_op, line: $line,
              over: ($line != null and $row.ns_op != null and $row.ns_op > $line)
            },
            allocs_op: held($row.allocs_op; $base.allocs_op; nights($allocs; $row; $from);
              ($base.tolerances.allocs_op // $defaults.allocs_op // 0.02); $from),
            bytes_op: held($row.bytes_op; $base.bytes_op; nights($bytes; $row; $from);
              ($base.tolerances.bytes_op // $defaults.bytes_op // 0.02); $from)
          } ]
  ' <<<"$baseline"
}

# Prints one alert line a reading that fails the run: an allocation count past its limit, or a
# Rust timing past its line.
hard_regressions() {
  local limits="$1"
  jq -r --argjson reltol "$NS_REL_TOL" --argjson ceiltol "$NS_ABS_CEIL_TOL" '
    .[] as $r
    | ( (["allocs_op", "bytes_op"][] as $m
          | $r[$m] | select(. != null and .limit != null and .reading > .limit)
          | "  • \($r.lang)/\($r.name) \($m): \(.base) → \(.reading) (>\(.tol * 100)% "
            + (if .basis == "baseline" then "tolerance" else "over the nights\u0027 median" end) + ")"),
        ($r.ns | select($r.lang != "go" and .over)
          | if .band != null and .reading > .band
              then "  • \($r.lang)/\($r.name) ns_op: \(.median) → \(.reading) (>\($reltol * 100)% window median)"
              else "  • \($r.lang)/\($r.name) ns_op: \(.base) → \(.reading) (>\($ceiltol * 100)% baseline ceiling)"
            end) )
  ' <<<"$limits"
}

# Writes each Go timing past its line, and its nights as Go benchmark lines, for the re-run.
write_flagged() {
  local limits="$1"
  jq -r '.[] | select(.lang == "go" and .ns.over) | "\(.name)\t\(.ns.line)\t\(.ns.count)"' \
    <<<"$limits" >"$BENCHMARK_FLAGGED_FILE" || return 2
  jq -r '.[] | select(.lang == "go" and .ns.over) | .name as $n | .ns.nights[] | "\($n) 1 \(.) ns/op"' \
    <<<"$limits" >"$BENCHMARK_WINDOW_FILE" || return 2
}

# Writes the summary page from the limits the gates apply.
summary_page() {
  local limits="$1"
  {
    printf '### Benchmarks\n\n'
    summary_table_header
    jq -r --argjson reltol "$NS_REL_TOL" --argjson ceiltol "$NS_ABS_CEIL_TOL" --argjson minn "$NS_MIN_WINDOW_SAMPLES" \
      --argjson terms "$(summary_terms_json ns/op allocs/op B/op)" '
      def term($w): "<span title=\"\($terms[$w])\">\($w)</span>";
      def num: (. * 100 | round) / 100 | tostring;
      def waiting($count; $from): "no limit yet (\($count) of \($minn) nights since \($from))";
      .[] as $r
      | ("\($r.lang)/\($r.name)") as $name
      | ((["allocs_op", "bytes_op"][] as $m
          | $r[$m] | select(. != null)
          | ($m | sub("_op$"; "/op") | sub("^bytes"; "B") | term(.)) as $unit
          | if .limit == null then
              [$name + " " + $unit, (if .basis == "waiting" then waiting(.count; .from) else "no limit" end),
               (.reading | num), "—"]
            else
              [$name + " " + $unit,
               "≤ \(.limit | num) (" + (if .basis == "baseline" then "baseline" else "nights\u0027 median" end)
                 + " \(.base | num) + \(.tol * 100 | num) %)",
               (.reading | num), (if .reading > .limit then "FAIL" else "pass" end)]
            end),
         ($r.ns | select(.reading != null)
          | ([ (if .band != null then "≤ \(.band | num) ns (nights\u0027 median × \(1 + $reltol | num))" else empty end),
               (if .ceiling != null then "≤ \(.ceiling | num) ns (baseline × \(1 + $ceiltol | num))" else empty end) ]
             | if length > 0 then join("; ")
               elif $r.from != null then waiting($r.ns.count; $r.from)
               else "no limit" end) as $expected
          | [$name + " " + term("ns/op"), $expected, "\(.reading | num) ns",
             (if .line == null then "—" elif .over then (if $r.lang == "go" then "re-run" else "FAIL" end)
              else "pass" end)]))
      | "| \(.[0]) | \(.[1]) | \(.[2]) | \(.[3]) |"
    ' <<<"$limits"
  } >"$BENCHMARK_SUMMARY_FILE"
}

regression_check() {
  local rows="$1"
  [[ -f "$BASELINE_FILE" ]] || {
    echo "missing: $BASELINE_FILE" >&2
    return 2
  }

  local baseline
  baseline="$(jq -c '.' "$BASELINE_FILE")" || {
    echo "baseline is malformed: $BASELINE_FILE" >&2
    return 2
  }

  # Without tonight's date the window would read tonight into itself.
  vm_tonight >/dev/null || return 2
  local ns allocs='[]' bytes='[]'
  ns="$(nightly_json benchmark_ns_op)"
  # Allocation nights are read only for a row with no committed numbers to hold them to.
  if jq -e '[.benchmarks[]? | select(.history_from != null and (.allocs_op == null or .bytes_op == null))]
    | length > 0' <<<"$baseline" >/dev/null; then
    allocs="$(nightly_json benchmark_allocs_op)"
    bytes="$(nightly_json benchmark_bytes_op)"
  fi

  local limits
  limits="$(benchmark_limits "$rows" "$baseline" "$ns" "$allocs" "$bytes")" || {
    echo "could not work out the limits" >&2
    return 2
  }
  summary_page "$limits"
  write_flagged "$limits" || return 2

  local branch="${GITHUB_REF_NAME:-dev}" lines
  lines="$(hard_regressions "$limits")" || return 2
  if [[ -n "$lines" ]]; then
    echo "REGRESSION_ALERT:⚠️ Benchmark regression on ${branch}"
    echo "REGRESSION_ALERT:"
    local l
    while IFS= read -r l; do
      echo "REGRESSION_ALERT:${l}"
    done <<<"$lines"
    return 1
  fi
  return 0
}

main() {
  local update_baseline=0
  if [[ "${1:-}" == "--update-baseline" ]]; then
    update_baseline=1
    shift
  fi
  if [[ "$#" -gt 0 ]]; then
    echo "usage: $0 [--update-baseline]" >&2
    return 2
  fi

  local rows
  rows="$(build_rows)" || return 2

  if ((update_baseline)); then
    build_baseline "$rows"
    return
  fi

  echo "$rows"
  regression_check "$rows"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
