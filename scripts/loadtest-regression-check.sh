#!/usr/bin/env bash
# Gates load-test trend rows against the median of the latest reading of each of the previous
# fourteen dates (three needed), read through scripts/lib/vm-query.sh.
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/vm-query.sh
. "$SCRIPT_DIR/lib/vm-query.sh"

WINDOW_DAYS=14
MIN_WINDOW_SAMPLES=3

# Consecutive nights a p99 advisory fires before it escalates; each bad night enters the window,
# so the comparison point climbs and only a counter catches the repeat.
P99_ESCALATE_NIGHTS=3

# Days back a previous night's streak count is read from, covering a night that did not run.
P99_STREAK_LOOKBACK_DAYS=3

P99_STREAK_FILE="${P99_STREAK_FILE:-loadtest-p99-streaks.json}"

# Tolerance bands wide enough to clear the variance of shared runners and a free-tier cluster;
# error_rate and the absolute floors carry the correctness signal.
LATENCY_REL_TOL=4.0
RPS_REL_TOL=0.65
P99_REL_TOL=3.0
ERROR_RATE_REL_TOL=1.0

usage() {
  echo "usage: $0 [loadtest-summary.json|-]" >&2
}

read_rows() {
  local input="${1:-loadtest-summary.json}"
  if [ "$input" = "-" ]; then
    jq -c 'sort_by(.source, .scenario, .phase)' -
    return
  fi
  [[ -f "$input" ]] || {
    echo "missing: $input" >&2
    return 2
  }
  jq -c 'sort_by(.source, .scenario, .phase)' "$input"
}

vm_metric_name() {
  case "$1" in
    latency_p50_ms) printf '%s\n' "loadtest_latency_p50_ms" ;;
    latency_p95_ms) printf '%s\n' "loadtest_latency_p95_ms" ;;
    latency_p99_ms) printf '%s\n' "loadtest_latency_p99_ms" ;;
    rps) printf '%s\n' "loadtest_rps" ;;
    error_rate) printf '%s\n' "loadtest_error_rate" ;;
    *) return 2 ;;
  esac
}

# Absolute limits live in the profile and are read by scripts/loadtest-gate-check.sh; a slowly
# worsening product drags the window median down, and those limits are the floor under it.

num_gt() {
  awk -v a="$1" -v b="$2" 'BEGIN { exit !(a > b) }'
}

num_lt() {
  awk -v a="$1" -v b="$2" 'BEGIN { exit !(a < b) }'
}

num_ge() {
  awk -v a="$1" -v b="$2" 'BEGIN { exit !(a >= b) }'
}

num_pos() {
  awk -v a="$1" 'BEGIN { exit !(a > 0) }'
}

mul() {
  awk -v a="$1" -v b="$2" 'BEGIN { printf "%.6f", a * b }'
}

pct() {
  awk -v a="$1" 'BEGIN { printf "%.0f", a * 100 }'
}

prom_label_escape() {
  sed 's/\\/\\\\/g; s/"/\\"/g' <<<"$1"
}

window_stats_for_metric() {
  local metric="$1" vm_metric
  vm_metric="$(vm_metric_name "$metric")" || return 0
  vm_nightly_window "$vm_metric" 'env="ci"' "$WINDOW_DAYS" | awk -F'\t' -v metric="$metric" '
    {
      sig = $1; median = $2; count = $3
      source = ""; scenario = ""; phase = ""; workload = ""
      n = split(sig, parts, ",")
      for (i = 1; i <= n; i++) {
        split(parts[i], kv, "=")
        if (kv[1] == "source") source = kv[2]
        if (kv[1] == "scenario") scenario = kv[2]
        if (kv[1] == "phase") phase = kv[2]
        if (kv[1] == "workload") workload = kv[2]
      }
      if (source == "" || scenario == "" || phase == "") next
      # The workload is part of the key, so a scenario rewritten under its old name compares
      # against its own nights alone.
      print metric "/" source "/" scenario "/" phase "/" workload "\t" median "\t" count
    }
  '
}

window_map() {
  local map
  map="$(
    {
      window_stats_for_metric latency_p50_ms
      window_stats_for_metric latency_p95_ms
      window_stats_for_metric latency_p99_ms
      window_stats_for_metric rps
      window_stats_for_metric error_rate
    } | jq -Rn '
      [ inputs
        | split("\t")
        | select(length >= 3)
        | { key: .[0], value: { median: .[1], count: (.[2] | tonumber) } }
      ] | from_entries
    ' 2>/dev/null || true
  )"
  [[ -n "$map" ]] && printf '%s\n' "$map" || printf '{}\n'
}

window_entry() {
  local map="$1" metric="$2" source="$3" scenario="$4" phase="$5" workload="${6:-}"
  jq -c \
    --arg key "${metric}/${source}/${scenario}/${phase}/${workload}" \
    '.[$key] // null' <<<"$map" 2>/dev/null || printf 'null\n'
}

latency_regression_line() {
  local source="$1" scenario="$2" phase="$3" metric="$4" current="$5" p99="$6" window="$7" workload="$8"
  local series="${source}/${scenario}/${phase}"
  local entry count median threshold detail
  entry="$(window_entry "$window" "$metric" "$source" "$scenario" "$phase" "$workload")"
  count="$(jq -r '.count // 0' <<<"$entry")"
  median="$(jq -r '.median // empty' <<<"$entry")"

  if [ -n "$median" ] && num_ge "$count" "$MIN_WINDOW_SAMPLES" && num_pos "$median"; then
    threshold="$(mul "$median" "$(awk -v tol="$LATENCY_REL_TOL" 'BEGIN { printf "%.6f", 1 + tol }')")"
    if num_gt "$current" "$threshold"; then
      detail="${series} ${metric}: ${median} -> ${current} (>$(pct "$LATENCY_REL_TOL")% over window median"
      if [ -n "$p99" ]; then
        detail="${detail}; p99=${p99} advisory-only"
      fi
      detail="${detail})"
      printf '%s\n' "$detail"
      return
    fi
  fi

}

rps_regression_line() {
  local source="$1" scenario="$2" phase="$3" current="$4" window="$5" workload="$6"
  local series="${source}/${scenario}/${phase}"
  local entry count median threshold
  entry="$(window_entry "$window" rps "$source" "$scenario" "$phase" "$workload")"
  count="$(jq -r '.count // 0' <<<"$entry")"
  median="$(jq -r '.median // empty' <<<"$entry")"

  if [ -n "$median" ] && num_ge "$count" "$MIN_WINDOW_SAMPLES" && num_pos "$median"; then
    threshold="$(mul "$median" "$(awk -v tol="$RPS_REL_TOL" 'BEGIN { printf "%.6f", 1 - tol }')")"
    if num_lt "$current" "$threshold"; then
      printf '%s\n' "${series} rps: ${median} -> ${current} (<$(pct "$RPS_REL_TOL")% below window median floor)"
      return
    fi
  fi

}

error_rate_regression_line() {
  local source="$1" scenario="$2" phase="$3" current="$4" window="$5" workload="$6"
  local series="${source}/${scenario}/${phase}"
  local entry count median threshold
  entry="$(window_entry "$window" error_rate "$source" "$scenario" "$phase" "$workload")"
  count="$(jq -r '.count // 0' <<<"$entry")"
  median="$(jq -r '.median // empty' <<<"$entry")"
  if [ -n "$median" ] && num_ge "$count" "$MIN_WINDOW_SAMPLES" && num_pos "$median"; then
    threshold="$(mul "$median" "$(awk -v tol="$ERROR_RATE_REL_TOL" 'BEGIN { printf "%.6f", 1 + tol }')")"
    if num_gt "$current" "$threshold"; then
      printf '%s\n' "${series} error_rate: ${median} -> ${current} (>$(pct "$ERROR_RATE_REL_TOL")% over window median)"
    fi
  fi
}

# Streak counts from the newest night before tonight, keyed like the window and read from the
# store the run writes to.
streak_map() {
  local map oldest
  oldest="$(date -u -d "$(vm_tonight) - ${P99_STREAK_LOOKBACK_DAYS} days" +%Y-%m-%d)"
  map="$(
    vm_query_nightly loadtest_p99_advisory_streak 'env="ci"' 1 \
      | awk -F'\t' -v oldest="$oldest" '
        $2 < oldest { next }
        {
          sig = $1; val = $3
          source = ""; scenario = ""; phase = ""; workload = ""
          n = split(sig, parts, ",")
          for (i = 1; i <= n; i++) {
            split(parts[i], kv, "=")
            if (kv[1] == "source") source = kv[2]
            if (kv[1] == "scenario") scenario = kv[2]
            if (kv[1] == "phase") phase = kv[2]
            if (kv[1] == "workload") workload = kv[2]
          }
          if (source == "" || scenario == "" || phase == "") next
          print source "/" scenario "/" phase "/" workload "\t" val
        }
      ' | jq -Rn '
        [ inputs
          | split("\t")
          | select(length >= 2)
          | { key: .[0], value: (.[1] | tonumber) }
        ] | from_entries
      ' 2>/dev/null || true
  )"
  [[ -n "$map" ]] && printf '%s\n' "$map" || printf '{}\n'
}

streak_entry() {
  local map="$1" source="$2" scenario="$3" phase="$4" workload="${5:-}"
  jq -r \
    --arg key "${source}/${scenario}/${phase}/${workload}" \
    '.[$key] // 0' <<<"$map" 2>/dev/null || printf '0\n'
}

p99_advisory_line() {
  local source="$1" scenario="$2" phase="$3" current="$4" window="$5" workload="$6"
  local series="${source}/${scenario}/${phase}"
  local entry count median threshold
  entry="$(window_entry "$window" latency_p99_ms "$source" "$scenario" "$phase" "$workload")"
  count="$(jq -r '.count // 0' <<<"$entry")"
  median="$(jq -r '.median // empty' <<<"$entry")"

  if [ -n "$median" ] && num_ge "$count" "$MIN_WINDOW_SAMPLES" && num_pos "$median"; then
    threshold="$(mul "$median" "$(awk -v tol="$P99_REL_TOL" 'BEGIN { printf "%.6f", 1 + tol }')")"
    if num_gt "$current" "$threshold"; then
      printf '%s\n' "${series} latency_p99_ms: ${median} -> ${current} (advisory-only window)"
      return
    fi
  fi

}

regression_check() {
  local rows="$1" window="$2"
  local branch="${GITHUB_REF_NAME:-dev}"
  local regression_lines=()
  local p99_lines=()
  local streak_rows=()
  local streaks
  local row source scenario phase workload p50 p95 p99 rps error_rate line
  streaks="$(streak_map)"

  while IFS= read -r row; do
    source="$(jq -r '.source // "unknown"' <<<"$row")"
    scenario="$(jq -r '.scenario // "unknown"' <<<"$row")"
    phase="$(jq -r '.phase // "aggregate"' <<<"$row")"
    # A row without a workload keys to the empty bucket of unnamed history and is judged by the
    # absolute rules alone.
    workload="$(jq -r '.workload // ""' <<<"$row")"
    p50="$(jq -r '.latency_p50_ms // empty' <<<"$row")"
    p95="$(jq -r '.latency_p95_ms // empty' <<<"$row")"
    p99="$(jq -r '.latency_p99_ms // empty' <<<"$row")"
    rps="$(jq -r '.rps // empty' <<<"$row")"
    error_rate="$(jq -r '.error_rate // empty' <<<"$row")"

    if [ -n "$p50" ]; then
      line="$(latency_regression_line "$source" "$scenario" "$phase" latency_p50_ms "$p50" "$p99" "$window" "$workload")"
      [ -z "$line" ] || regression_lines+=("$line")
    fi
    if [ -n "$p95" ]; then
      line="$(latency_regression_line "$source" "$scenario" "$phase" latency_p95_ms "$p95" "$p99" "$window" "$workload")"
      [ -z "$line" ] || regression_lines+=("$line")
    fi
    if [ -n "$rps" ]; then
      line="$(rps_regression_line "$source" "$scenario" "$phase" "$rps" "$window" "$workload")"
      [ -z "$line" ] || regression_lines+=("$line")
    fi
    if [ -n "$error_rate" ]; then
      line="$(error_rate_regression_line "$source" "$scenario" "$phase" "$error_rate" "$window" "$workload")"
      [ -z "$line" ] || regression_lines+=("$line")
    fi
    if [ -n "$p99" ]; then
      line="$(p99_advisory_line "$source" "$scenario" "$phase" "$p99" "$window" "$workload")"
      # Every series gets a count, zero included, so three bad nights in a fortnight differ from
      # three in a row.
      local previous streak
      previous="$(streak_entry "$streaks" "$source" "$scenario" "$phase" "$workload")"
      if [ -n "$line" ]; then
        streak="$(awk -v p="$previous" 'BEGIN { printf "%d", p + 1 }')"
        p99_lines+=("${line} (${streak} consecutive nights)")
        if num_ge "$streak" "$P99_ESCALATE_NIGHTS"; then
          regression_lines+=("${source}/${scenario}/${phase} latency_p99_ms has been past its window for ${streak} consecutive nights — an advisory that repeats is a finding, and this one raises its own comparison point until it goes quiet")
        fi
      else
        streak=0
      fi
      streak_rows+=("$(jq -nc \
        --arg source "$source" --arg scenario "$scenario" --arg phase "$phase" \
        --arg workload "$workload" --argjson streak "$streak" \
        '{source: $source, scenario: $scenario, phase: $phase, workload: $workload, streak: $streak}')")
    fi
  done < <(jq -c '.[]' <<<"$rows")

  if ((${#streak_rows[@]})); then
    printf '%s\n' "${streak_rows[@]}" | jq -s '.' >"$P99_STREAK_FILE"
  else
    printf '[]\n' >"$P99_STREAK_FILE"
  fi

  local line
  for line in "${p99_lines[@]}"; do
    echo "P99_ADVISORY:${line}"
  done

  if ((${#regression_lines[@]})); then
    echo "REGRESSION_ALERT:Load-test regression on ${branch}"
    echo "REGRESSION_ALERT:"
    for line in "${regression_lines[@]}"; do
      echo "REGRESSION_ALERT:  - ${line}"
    done
    return 1
  fi

  return 0
}

main() {
  if [ "$#" -gt 1 ]; then
    usage
    return 2
  fi

  local rows window
  # Without tonight's date the window would read tonight into itself.
  vm_tonight >/dev/null || return 2
  rows="$(read_rows "${1:-loadtest-summary.json}")" || return 2
  echo "$rows"

  window="$(window_map)"
  regression_check "$rows" "$window"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
