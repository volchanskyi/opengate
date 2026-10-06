#!/usr/bin/env bash
# Builds benchmark rows from Go -benchmem and Criterion output and gates regressions.
# Allocation metrics are held to the baseline at ±2%; ns/op to a VM window median or a ceiling.
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

GO_BENCH_FILE="${GO_BENCH_FILE:-bench-go.txt}"
CRITERION_ROOT="${CRITERION_ROOT:-agent/target/criterion}"
BASELINE_FILE="${BASELINE_FILE:-benchmarks/baseline.json}"
COMMIT_SHA="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# ns/op window-gate constants, calibrated from measured run-to-run variance.
NS_WINDOW_DAYS=14       # < 30d VM retention; the 14 dates before tonight's
NS_REL_TOL=0.50         # regress if ns/op > window median × (1 + this)
NS_ABS_CEIL_TOL=1.0     # regress if ns/op > committed-baseline ns × (1 + this)
NS_MIN_WINDOW_SAMPLES=3 # fewer dates ⇒ relative rule skipped (cold-start)

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
  local go_tsv criterion_tsv tsv
  go_tsv="$(parse_go_bench "$GO_BENCH_FILE")" || return 2
  criterion_tsv="$(parse_criterion "$CRITERION_ROOT")" || return 2
  tsv="${go_tsv}"$'\n'"${criterion_tsv}"

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
    echo "build_rows: jq failed" >&2
    return 2
  }
}

build_baseline() {
  local rows="$1"
  jq -c \
    --arg generated_at "$TIMESTAMP" \
    '
      {
        version: 1,
        generated_at: $generated_at,
        default_tolerances: {
          allocs_op: 0.02,
          bytes_op: 0.02,
          ns_op: 1.50
        },
        benchmarks: [
          .[] | {
            name,
            lang,
            ns_op,
            bytes_op,
            allocs_op
          }
        ]
      }
    ' <<<"$rows"
}

hard_regressions() {
  local rows="$1" baseline="$2"
  jq -r --argjson rows "$rows" '
    . as $baseline
    | ($baseline.default_tolerances // {}) as $defaults
    | $rows[] as $row
    | ($baseline.benchmarks[]? | select(.name == $row.name and .lang == $row.lang)) as $base
    | ["allocs_op", "bytes_op"][] as $metric
    | select($base[$metric] != null and $row[$metric] != null)
    | ($base.tolerances[$metric] // $defaults[$metric] // 0.02) as $tolerance
    | select(($row[$metric] | tonumber) > (($base[$metric] | tonumber) * (1 + $tolerance)))
    | [
        $row.lang,
        $row.name,
        $metric,
        ($base[$metric] | tostring),
        ($row[$metric] | tostring),
        (($tolerance * 100) | tostring)
      ]
      | @tsv
  ' <<<"$baseline"
}

# Prints "lang<TAB>name<TAB>median<TAB>count" per series: the median of each prior date's latest
# reading. A VM failure yields no lines, leaving only the absolute rule.
ns_window_stats() {
  vm_nightly_window benchmark_ns_op 'env="ci"' "$NS_WINDOW_DAYS" | awk -F'\t' '
    {
      sig = $1; median = $2; count = $3
      bench = ""; lang = ""
      n = split(sig, parts, ",")
      for (i = 1; i <= n; i++) {
        split(parts[i], kv, "=")
        if (kv[1] == "benchmark") bench = kv[2]
        if (kv[1] == "lang") lang = kv[2]
      }
      if (bench == "" || lang == "") next
      print lang "\t" bench "\t" median "\t" count
    }
  '
}

# Build the window map "{\"lang/name\":{median,count}}" from ns_window_stats, or an
# empty object on any failure (fail-open ⇒ relative rule uniformly skipped).
ns_window_map() {
  local map
  map="$(ns_window_stats | jq -Rn '
    [ inputs
      | split("\t")
      | select(length >= 4)
      | { key: (.[0] + "/" + .[1]), value: { median: .[2], count: (.[3] | tonumber) } }
    ] | from_entries
  ' 2>/dev/null || true)"
  [[ -n "$map" ]] && printf '%s' "$map" || printf '{}'
}

# Emits one TSV row per series whose ns/op exceeds the window band or the baseline ceiling.
# The window rule needs NS_MIN_WINDOW_SAMPLES dates; the ceiling always applies.
ns_window_regressions() {
  local rows="$1" baseline="$2" window="$3"
  jq -r \
    --argjson rows "$rows" \
    --argjson window "$window" \
    --argjson reltol "$NS_REL_TOL" \
    --argjson ceiltol "$NS_ABS_CEIL_TOL" \
    --argjson minn "$NS_MIN_WINDOW_SAMPLES" '
    . as $baseline
    | $rows[] as $row
    | ($baseline.benchmarks[]? | select(.name == $row.name and .lang == $row.lang)) as $base
    | select($base.ns_op != null and $row.ns_op != null)
    | ($window[$row.lang + "/" + $row.name]) as $win
    | ($base.ns_op | tonumber) as $base_ns
    | ($row.ns_op | tonumber) as $cur_ns
    | ($base_ns * (1 + $ceiltol)) as $ceiling
    | (if ($win != null and ($win.count // 0) >= $minn and $win.median != null)
         then (($win.median | tonumber) * (1 + $reltol)) else null end) as $band
    | (if $band != null and $cur_ns > $band then "window"
       elif $cur_ns > $ceiling then "ceiling"
       else null end) as $rule
    | select($rule != null)
    | if $rule == "window"
        then [$row.lang, $row.name, "ns_op", ($win.median | tostring), ($cur_ns | tostring), (($reltol * 100) | tostring), "window median"]
        else [$row.lang, $row.name, "ns_op", ($base_ns | tostring), ($cur_ns | tostring), (($ceiltol * 100) | tostring), "baseline ceiling"]
      end
    | @tsv
  ' <<<"$baseline"
}

# Writes the summary page, stating the same limits the two gates apply.
summary_page() {
  local rows="$1" baseline="$2" window="$3"
  {
    printf '### Benchmarks\n\n'
    summary_table_header
    jq -r --argjson rows "$rows" --argjson window "$window" \
      --argjson reltol "$NS_REL_TOL" --argjson ceiltol "$NS_ABS_CEIL_TOL" --argjson minn "$NS_MIN_WINDOW_SAMPLES" '
      def num: (. * 100 | round) / 100 | tostring;
      . as $baseline
      | ($baseline.default_tolerances // {}) as $defaults
      | $rows[] as $row
      | ([$baseline.benchmarks[]? | select(.name == $row.name and .lang == $row.lang)] | first) as $base
      | ("\($row.lang)/\($row.name)") as $name
      | ((["allocs_op", "bytes_op"][] as $m
          | select($row[$m] != null)
          | ($m | sub("_op$"; "/op")) as $unit
          | if $base == null or $base[$m] == null then [$name + " " + $unit, "no limit", ($row[$m] | num), "—"]
            else ($base.tolerances[$m] // $defaults[$m] // 0.02) as $tol
              | ($base[$m] * (1 + $tol)) as $limit
              | [$name + " " + $unit, "≤ \($limit | num) (baseline \($base[$m] | num) + \($tol * 100 | num) %)",
                 ($row[$m] | num), (if $row[$m] > $limit then "FAIL" else "pass" end)]
            end),
         (select($row.ns_op != null)
          | ($window[$row.lang + "/" + $row.name]) as $win
          | (if $win != null and ($win.count // 0) >= $minn and $win.median != null
               then ($win.median | tonumber) * (1 + $reltol) else null end) as $band
          | (if $base != null and $base.ns_op != null then $base.ns_op * (1 + $ceiltol) else null end) as $ceiling
          | ([ (if $band != null then "≤ \($band | num) ns (nights\u0027 median × \(1 + $reltol | num))" else empty end),
               (if $ceiling != null then "≤ \($ceiling | num) ns (baseline × \(1 + $ceiltol | num))" else empty end) ]
             | if length == 0 then "no limit" else join("; ") end) as $expected
          | (($band != null and $row.ns_op > $band) or ($ceiling != null and $row.ns_op > $ceiling)) as $crossed
          | [$name + " ns/op", $expected, "\($row.ns_op | num) ns",
             (if $band == null and $ceiling == null then "—" elif $crossed then "FAIL" else "pass" end)]))
      | "| \(.[0]) | \(.[1]) | \(.[2]) | \(.[3]) |"
    ' <<<"$baseline"
    summary_legend
    printf -- '- **ns/op, allocs/op, B/op** — the time, the allocations and the bytes one call of the benchmark took. Allocations are held to the committed baseline; time is held to the median of the nights before and to a ceiling over the baseline, because a runner'"'"'s speed varies from night to night.\n'
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

  # Read-back the ns/op window baseline once (fail-open to {} on any VM failure).
  # Without tonight's date the window would read tonight into itself.
  vm_tonight >/dev/null || return 2
  local window
  window="$(ns_window_map)"
  summary_page "$rows" "$baseline" "$window"

  local branch="${GITHUB_REF_NAME:-dev}"
  local lines=()

  # Deterministic allocs/bytes gate — committed baseline ±2% (unchanged).
  while IFS=$'\t' read -r lang name metric old new tolerance; do
    [[ -n "${lang:-}" ]] || continue
    lines+=("  • ${lang}/${name} ${metric}: ${old} → ${new} (>${tolerance}% tolerance)")
  done < <(hard_regressions "$rows" "$baseline")

  # Noise-robust ns/op gate — VM window median band OR committed-baseline ceiling.
  while IFS=$'\t' read -r lang name metric old new tolerance rule; do
    [[ -n "${lang:-}" ]] || continue
    lines+=("  • ${lang}/${name} ${metric}: ${old} → ${new} (>${tolerance}% ${rule})")
  done < <(ns_window_regressions "$rows" "$baseline" "$window")

  if ((${#lines[@]})); then
    echo "REGRESSION_ALERT:⚠️ Benchmark regression on ${branch}"
    echo "REGRESSION_ALERT:"
    local l
    for l in "${lines[@]}"; do
      echo "REGRESSION_ALERT:${l}"
    done
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
