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

# Frozen tolerance bands, calibrated once offline from the live 14-30d VM series.
# Deliberately broad: staging load crosses GitHub-hosted runners, a kubectl
# port-forward, and the OKE cluster, so run-to-run variance is large.
LATENCY_REL_TOL=2.0
RPS_REL_TOL=0.50
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

latency_abs_ceiling() {
  local source="$1" scenario="$2" phase="$3" metric="$4"
  case "$source/$scenario/$phase/$metric" in
    k6/api-baseline/http/*) printf '%s\n' "200" ;;
    k6/concurrent-agents/http/*) printf '%s\n' "500" ;;
    k6/relay-throughput/relay/*) printf '%s\n' "150" ;;
    k6/relay-throughput/http/*) printf '%s\n' "500" ;;
    quic/quic-agents/connect/latency_p50_ms) printf '%s\n' "600" ;;
    quic/quic-agents/connect/latency_p95_ms) printf '%s\n' "1000" ;;
    quic/quic-agents/handshake/latency_p50_ms) printf '%s\n' "300" ;;
    quic/quic-agents/handshake/latency_p95_ms) printf '%s\n' "500" ;;
    quic/quic-agents/register/latency_p50_ms) printf '%s\n' "100" ;;
    quic/quic-agents/register/latency_p95_ms) printf '%s\n' "250" ;;
    *) printf '%s\n' "1000" ;;
  esac
}

p99_abs_ceiling() {
  local source="$1" scenario="$2" phase="$3"
  case "$source/$scenario/$phase" in
    k6/api-baseline/http) printf '%s\n' "400" ;;
    k6/concurrent-agents/http) printf '%s\n' "500" ;;
    k6/relay-throughput/relay) printf '%s\n' "300" ;;
    k6/relay-throughput/http) printf '%s\n' "1000" ;;
    quic/quic-agents/connect) printf '%s\n' "2000" ;;
    quic/quic-agents/handshake) printf '%s\n' "1000" ;;
    quic/quic-agents/register) printf '%s\n' "500" ;;
    *) printf '%s\n' "2000" ;;
  esac
}

rps_abs_floor() {
  local source="$1" scenario="$2" phase="$3"
  case "$source/$scenario/$phase" in
    quic/quic-agents/aggregate) printf '%s\n' "50" ;;
    k6/api-baseline/http) printf '%s\n' "5" ;;
    k6/concurrent-agents/http) printf '%s\n' "5" ;;
    k6/relay-throughput/http) printf '%s\n' "5" ;;
    k6/relay-throughput/relay) printf '%s\n' "0.25" ;;
    *) printf '%s\n' "" ;;
  esac
}

error_rate_ceiling() {
  local source="$1" scenario="$2" phase="$3"
  case "$source/$scenario/$phase" in
    k6/api-baseline/http) printf '%s\n' "0.01" ;;
    k6/concurrent-agents/http) printf '%s\n' "0.001" ;;
    quic/quic-agents/aggregate) printf '%s\n' "0" ;;
    *) printf '%s\n' "0.01" ;;
  esac
}

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
  local source scenario phase
  source="$(prom_label_escape "$1")"
  scenario="$(prom_label_escape "$2")"
  phase="$(prom_label_escape "$3")"
  printf 'env="ci",source="%s",scenario="%s",phase="%s"' "$source" "$scenario" "$phase"
}

window_stats_for_metric() {
  local metric="$1" vm_metric selector window
  vm_metric="$(vm_metric_name "$metric")" || return 0
  selector="${vm_metric}{$(vm_query_selector 'env="ci"')}"
  window="[${WINDOW_DAYS}d]"
  {
    vm_query_window "quantile(0.5, median_over_time(${selector}${window})) by (source, scenario, phase)" \
      | sed "s/^/M\t${metric}\t/"
    vm_query_window "count(count_over_time(${selector}${window})) by (source, scenario, phase)" \
      | sed "s/^/C\t${metric}\t/"
  } | awk -F'\t' '
    {
      kind = $1; metric = $2; sig = $3; val = $4
      source = ""; scenario = ""; phase = ""
      n = split(sig, parts, ",")
      for (i = 1; i <= n; i++) {
        split(parts[i], kv, "=")
        if (kv[1] == "source") source = kv[2]
        if (kv[1] == "scenario") scenario = kv[2]
        if (kv[1] == "phase") phase = kv[2]
      }
      if (source == "" || scenario == "" || phase == "") next
      key = metric "/" source "/" scenario "/" phase
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
  local map="$1" metric="$2" source="$3" scenario="$4" phase="$5"
  jq -c \
    --arg key "${metric}/${source}/${scenario}/${phase}" \
    '.[$key] // null' <<<"$map" 2>/dev/null || printf 'null\n'
}

previous_error_rate() {
  local source="$1" scenario="$2" phase="$3" value
  value="$(vm_query_latest loadtest_error_rate "$(series_selector "$source" "$scenario" "$phase")" 2>/dev/null || true)"
  printf '%s\n' "$value"
}

latency_regression_line() {
  local source="$1" scenario="$2" phase="$3" metric="$4" current="$5" p99="$6" window="$7"
  local series="${source}/${scenario}/${phase}"
  local entry count median threshold ceiling detail
  entry="$(window_entry "$window" "$metric" "$source" "$scenario" "$phase")"
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

  ceiling="$(latency_abs_ceiling "$source" "$scenario" "$phase" "$metric")"
  if [ -n "$ceiling" ] && num_gt "$current" "$ceiling"; then
    detail="${series} ${metric}: ${ceiling} -> ${current} (absolute ceiling"
    if [ -n "$p99" ]; then
      detail="${detail}; p99=${p99} advisory-only"
    fi
    detail="${detail})"
    printf '%s\n' "$detail"
  fi
}

rps_regression_line() {
  local source="$1" scenario="$2" phase="$3" current="$4" window="$5"
  local series="${source}/${scenario}/${phase}"
  local entry count median threshold floor
  entry="$(window_entry "$window" rps "$source" "$scenario" "$phase")"
  count="$(jq -r '.count // 0' <<<"$entry")"
  median="$(jq -r '.median // empty' <<<"$entry")"

  if [ -n "$median" ] && num_ge "$count" "$MIN_WINDOW_SAMPLES" && num_pos "$median"; then
    threshold="$(mul "$median" "$(awk -v tol="$RPS_REL_TOL" 'BEGIN { printf "%.6f", 1 - tol }')")"
    if num_lt "$current" "$threshold"; then
      printf '%s\n' "${series} rps: ${median} -> ${current} (<$(pct "$RPS_REL_TOL")% below window median floor)"
      return
    fi
  fi

  floor="$(rps_abs_floor "$source" "$scenario" "$phase")"
  if [ -n "$floor" ] && num_pos "$floor" && num_lt "$current" "$floor"; then
    printf '%s\n' "${series} rps: ${floor} -> ${current} (absolute floor)"
  fi
}

error_rate_regression_line() {
  local source="$1" scenario="$2" phase="$3" current="$4"
  local series="${source}/${scenario}/${phase}"
  local ceiling prev threshold
  ceiling="$(error_rate_ceiling "$source" "$scenario" "$phase")"
  if [ -n "$ceiling" ] && num_gt "$current" "$ceiling"; then
    printf '%s\n' "${series} error_rate: ${ceiling} -> ${current} (absolute ceiling)"
    return
  fi

  prev="$(previous_error_rate "$source" "$scenario" "$phase")"
  if [ -n "$prev" ] && num_pos "$prev"; then
    threshold="$(mul "$prev" "$(awk -v tol="$ERROR_RATE_REL_TOL" 'BEGIN { printf "%.6f", 1 + tol }')")"
    if num_gt "$current" "$threshold"; then
      printf '%s\n' "${series} error_rate: ${prev} -> ${current} (>$(pct "$ERROR_RATE_REL_TOL")% previous-sample increase)"
    fi
  fi
}

p99_advisory_line() {
  local source="$1" scenario="$2" phase="$3" current="$4" window="$5"
  local series="${source}/${scenario}/${phase}"
  local entry count median threshold ceiling
  entry="$(window_entry "$window" latency_p99_ms "$source" "$scenario" "$phase")"
  count="$(jq -r '.count // 0' <<<"$entry")"
  median="$(jq -r '.median // empty' <<<"$entry")"

  if [ -n "$median" ] && num_ge "$count" "$MIN_WINDOW_SAMPLES" && num_pos "$median"; then
    threshold="$(mul "$median" "$(awk -v tol="$P99_REL_TOL" 'BEGIN { printf "%.6f", 1 + tol }')")"
    if num_gt "$current" "$threshold"; then
      printf '%s\n' "${series} latency_p99_ms: ${median} -> ${current} (advisory-only window)"
      return
    fi
  fi

  ceiling="$(p99_abs_ceiling "$source" "$scenario" "$phase")"
  if [ -n "$ceiling" ] && num_gt "$current" "$ceiling"; then
    printf '%s\n' "${series} latency_p99_ms: ${ceiling} -> ${current} (advisory-only ceiling)"
  fi
}

regression_check() {
  local rows="$1" window="$2"
  local branch="${GITHUB_REF_NAME:-dev}"
  local regression_lines=()
  local p99_lines=()
  local row source scenario phase p50 p95 p99 rps error_rate line

  while IFS= read -r row; do
    source="$(jq -r '.source // "unknown"' <<<"$row")"
    scenario="$(jq -r '.scenario // "unknown"' <<<"$row")"
    phase="$(jq -r '.phase // "aggregate"' <<<"$row")"
    p50="$(jq -r '.latency_p50_ms // empty' <<<"$row")"
    p95="$(jq -r '.latency_p95_ms // empty' <<<"$row")"
    p99="$(jq -r '.latency_p99_ms // empty' <<<"$row")"
    rps="$(jq -r '.rps // empty' <<<"$row")"
    error_rate="$(jq -r '.error_rate // empty' <<<"$row")"

    if [ -n "$p50" ]; then
      line="$(latency_regression_line "$source" "$scenario" "$phase" latency_p50_ms "$p50" "$p99" "$window")"
      [ -z "$line" ] || regression_lines+=("$line")
    fi
    if [ -n "$p95" ]; then
      line="$(latency_regression_line "$source" "$scenario" "$phase" latency_p95_ms "$p95" "$p99" "$window")"
      [ -z "$line" ] || regression_lines+=("$line")
    fi
    if [ -n "$rps" ]; then
      line="$(rps_regression_line "$source" "$scenario" "$phase" "$rps" "$window")"
      [ -z "$line" ] || regression_lines+=("$line")
    fi
    if [ -n "$error_rate" ]; then
      line="$(error_rate_regression_line "$source" "$scenario" "$phase" "$error_rate")"
      [ -z "$line" ] || regression_lines+=("$line")
    fi
    if [ -n "$p99" ]; then
      line="$(p99_advisory_line "$source" "$scenario" "$phase" "$p99" "$window")"
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
