#!/usr/bin/env bash
# Build canonical benchmark rows from Go -benchmem output and Criterion JSON,
# then compare deterministic allocation metrics against the committed baseline.
set -uo pipefail

GO_BENCH_FILE="${GO_BENCH_FILE:-bench-go.txt}"
CRITERION_ROOT="${CRITERION_ROOT:-agent/target/criterion}"
BASELINE_FILE="${BASELINE_FILE:-benchmarks/baseline.json}"
COMMIT_SHA="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

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

ns_advisories() {
  local rows="$1" baseline="$2"
  jq -r --argjson rows "$rows" '
    . as $baseline
    | ($baseline.default_tolerances // {}) as $defaults
    | $rows[] as $row
    | ($baseline.benchmarks[]? | select(.name == $row.name and .lang == $row.lang)) as $base
    | select($base.ns_op != null and $row.ns_op != null)
    | ($base.tolerances.ns_op // $defaults.ns_op // 1.5) as $tolerance
    | select(($row.ns_op | tonumber) > (($base.ns_op | tonumber) * (1 + $tolerance)))
    | [
        $row.lang,
        $row.name,
        "ns_op",
        ($base.ns_op | tostring),
        ($row.ns_op | tostring),
        (($tolerance * 100) | tostring)
      ]
      | @tsv
  ' <<<"$baseline"
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

  local line
  while IFS=$'\t' read -r lang name metric old new tolerance; do
    [[ -n "${lang:-}" ]] || continue
    echo "BENCHMARK_ADVISORY: ${lang}/${name} ${metric} ${old} → ${new} (>${tolerance}% tolerance; advisory only)"
  done < <(ns_advisories "$rows" "$baseline")

  local regressed=0
  local hard=()
  while IFS= read -r line; do
    [[ -n "$line" ]] || continue
    hard+=("$line")
    regressed=1
  done < <(hard_regressions "$rows" "$baseline")

  if ((regressed)); then
    echo "REGRESSION_ALERT:⚠️ Benchmark allocation regression on dev"
    echo "REGRESSION_ALERT:"
    while IFS=$'\t' read -r lang name metric old new tolerance; do
      echo "REGRESSION_ALERT:  • ${lang}/${name} ${metric}: ${old} → ${new} (>${tolerance}% tolerance)"
    done < <(printf '%s\n' "${hard[@]}")
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
