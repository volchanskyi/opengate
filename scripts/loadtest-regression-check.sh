#!/usr/bin/env bash
# Gate canonical load-test trend rows against VictoriaMetrics read-back baselines.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/vm-query.sh
. "$SCRIPT_DIR/lib/vm-query.sh"

COMMIT_SHA="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
export VM_EXCLUDE_COMMIT="${VM_EXCLUDE_COMMIT:-$COMMIT_SHA}"

WINDOW_DAYS=14
MIN_WINDOW_SAMPLES=3

# Frozen tolerance bands, calibrated offline from the live VM series.
# Deliberately broad: staging load crosses GitHub-hosted runners, a kubectl
# port-forward, and a shared free-tier OKE cluster, so run-to-run variance is
# large. A contended night degrades throughput and latency together while every
# agent still succeeds — measured over a 24-run window, aggregate rps swings
# between 0.36x and 1.22x its median and connect p95 reaches 4.5x its median with
# error_rate flat at zero. The bands below clear the widest excursion in that
# window, so they red on a collapse rather than on a busy neighbour; error_rate
# and the absolute floors carry the correctness signal.
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

# The absolute limits this file used to hold now live in the profile, beside the
# phases they judge, and are read by scripts/loadtest-gate-check.sh.
#
# They were here and there at once, keyed by the same source/scenario/phase
# triple, with different values for the same measurement — 200 in this file and
# 100 in the profile, one enforced and one read by nothing. An edit to either
# did not do what it said, which is a worse failure than either number being
# wrong.
#
# What stays here is the method: tonight against a typical night from the last
# fortnight, with tolerances wide enough to clear the spread a shared cluster
# produces on its own. That comparison has a blind spot by construction — a
# product that gets slowly worse drags its own window median down with it — and
# the profile's limits are the floor under that slide.

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

series_selector() {
  local source scenario phase workload
  source="$(prom_label_escape "$1")"
  scenario="$(prom_label_escape "$2")"
  phase="$(prom_label_escape "$3")"
  workload="$(prom_label_escape "${4:-}")"
  printf 'env="ci",source="%s",scenario="%s",phase="%s",workload="%s"' \
    "$source" "$scenario" "$phase" "$workload"
}

window_stats_for_metric() {
  local metric="$1" vm_metric selector window
  vm_metric="$(vm_metric_name "$metric")" || return 0
  selector="${vm_metric}{$(vm_query_selector 'env="ci"')}"
  window="[${WINDOW_DAYS}d]"
  {
    vm_query_window "quantile(0.5, median_over_time(${selector}${window})) by (source, scenario, phase, workload)" \
      | sed "s/^/M\t${metric}\t/"
    vm_query_window "count(count_over_time(${selector}${window})) by (source, scenario, phase, workload)" \
      | sed "s/^/C\t${metric}\t/"
  } | awk -F'\t' '
    {
      kind = $1; metric = $2; sig = $3; val = $4
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
      # A sample produced before the workload was named carries no label, and
      # groups under the empty one. No current row keys there, which is the
      # point: what produced it cannot be established, so it compares to nothing.
      key = metric "/" source "/" scenario "/" phase "/" workload
      if (kind == "M") med[key] = val; else cnt[key] = val
    }
    END {
      for (k in med) {
        c = (k in cnt) ? cnt[k] : 0
        print k "\t" med[k] "\t" c
      }
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

previous_error_rate() {
  local source="$1" scenario="$2" phase="$3" workload="$4" value
  value="$(vm_query_latest loadtest_error_rate "$(series_selector "$source" "$scenario" "$phase" "$workload")" 2>/dev/null || true)"
  printf '%s\n' "$value"
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
  local source="$1" scenario="$2" phase="$3" current="$4" workload="$5"
  local series="${source}/${scenario}/${phase}"
  local prev threshold
  prev="$(previous_error_rate "$source" "$scenario" "$phase" "$workload")"
  if [ -n "$prev" ] && num_pos "$prev"; then
    threshold="$(mul "$prev" "$(awk -v tol="$ERROR_RATE_REL_TOL" 'BEGIN { printf "%.6f", 1 + tol }')")"
    if num_gt "$current" "$threshold"; then
      printf '%s\n' "${series} error_rate: ${prev} -> ${current} (>$(pct "$ERROR_RATE_REL_TOL")% previous-sample increase)"
    fi
  fi
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
  local row source scenario phase workload p50 p95 p99 rps error_rate line

  while IFS= read -r row; do
    source="$(jq -r '.source // "unknown"' <<<"$row")"
    scenario="$(jq -r '.scenario // "unknown"' <<<"$row")"
    phase="$(jq -r '.phase // "aggregate"' <<<"$row")"
    # A row that names no workload cannot say what produced it, so it keys to
    # the same empty bucket the unnamed history sits in and is judged by the
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
      line="$(error_rate_regression_line "$source" "$scenario" "$phase" "$error_rate" "$workload")"
      [ -z "$line" ] || regression_lines+=("$line")
    fi
    if [ -n "$p99" ]; then
      line="$(p99_advisory_line "$source" "$scenario" "$phase" "$p99" "$window" "$workload")"
      [ -z "$line" ] || p99_lines+=("$line")
    fi
  done < <(jq -c '.[]' <<<"$rows")

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
  rows="$(read_rows "${1:-loadtest-summary.json}")" || return 2
  echo "$rows"

  window="$(window_map)"
  regression_check "$rows" "$window"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
