#!/usr/bin/env bash
# Build canonical load-test trend rows from k6 summary-export JSON files and
# the QUIC load-test harness text output.
set -euo pipefail

K6_SUMMARY_DIR="${K6_SUMMARY_DIR:-loadtest-k6}"
QUIC_OUTPUT_FILE="${QUIC_OUTPUT_FILE:-loadtest-quic.txt}"
COMMIT_SHA="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

duration_to_ms() {
  local input="${1//µs/us}"
  local rest="$input"
  local total="0"
  local amount unit

  if [ -z "$rest" ]; then
    echo "empty duration" >&2
    return 2
  fi

  while [[ "$rest" =~ ^([0-9]+([.][0-9]+)?)(ns|us|ms|s|m|h)(.*)$ ]]; do
    amount="${BASH_REMATCH[1]}"
    unit="${BASH_REMATCH[3]}"
    rest="${BASH_REMATCH[4]}"
    total="$(
      awk -v total="$total" -v amount="$amount" -v unit="$unit" '
        BEGIN {
          factor = 0
          if (unit == "ns") factor = 0.000001
          if (unit == "us") factor = 0.001
          if (unit == "ms") factor = 1
          if (unit == "s") factor = 1000
          if (unit == "m") factor = 60000
          if (unit == "h") factor = 3600000
          printf "%.6f", total + (amount * factor)
        }
      '
    )" || return 2
  done

  if [ -n "$rest" ]; then
    echo "invalid duration: $input" >&2
    return 2
  fi

  awk -v value="$total" 'BEGIN { if (value == int(value)) printf "%.0f\n", value; else printf "%.6f\n", value }'
}

emit_k6_rows() {
  local file="$1"
  local scenario="$2"

  jq -c \
    --arg commit "$COMMIT_SHA" \
    --arg timestamp "$TIMESTAMP" \
    --arg scenario "$scenario" \
    '
      def values($name): (.metrics[$name].values // {});
      def compact: with_entries(select(.value != null));
      def base($phase): {
        source: "k6",
        scenario: $scenario,
        phase: $phase,
        commit: $commit,
        env: "ci",
        timestamp: $timestamp
      };

      (base("http") + {
        latency_p50_ms: (values("http_req_duration")["p(50)"] // values("http_req_duration").med // null),
        latency_p95_ms: (values("http_req_duration")["p(95)"] // null),
        latency_p99_ms: (values("http_req_duration")["p(99)"] // null),
        rps: (values("http_reqs").rate // null),
        error_rate: (values("http_req_failed").rate // null)
      } | compact),
      (if (.metrics.relay_msg_latency_ms.values? != null) then
        (base("relay") + {
          latency_p50_ms: (values("relay_msg_latency_ms")["p(50)"] // values("relay_msg_latency_ms").med // null),
          latency_p95_ms: (values("relay_msg_latency_ms")["p(95)"] // null),
          latency_p99_ms: (values("relay_msg_latency_ms")["p(99)"] // null),
          rps: (values("relay_msg_count").rate // null)
        } | compact)
      else empty end)
    ' "$file"
}

emit_quic_phase_row() {
  local phase="$1"
  local p50="$2"
  local p95="$3"
  local p99="$4"
  local p50_ms p95_ms p99_ms

  p50_ms="$(duration_to_ms "$p50")" || return 2
  p95_ms="$(duration_to_ms "$p95")" || return 2
  p99_ms="$(duration_to_ms "$p99")" || return 2

  jq -nc \
    --arg commit "$COMMIT_SHA" \
    --arg timestamp "$TIMESTAMP" \
    --arg phase "$phase" \
    --argjson p50 "$p50_ms" \
    --argjson p95 "$p95_ms" \
    --argjson p99 "$p99_ms" \
    '{
      source: "quic",
      scenario: "quic-agents",
      phase: $phase,
      latency_p50_ms: $p50,
      latency_p95_ms: $p95,
      latency_p99_ms: $p99,
      commit: $commit,
      env: "ci",
      timestamp: $timestamp
    }'
}

emit_quic_rows() {
  local file="$1"
  [ -f "$file" ] || return 0

  local total_duration agents_line successes total_agents total_ms rps error_rate
  total_duration="$(awk '/^Total time:/ { sub(/^Total time:[[:space:]]*/, ""); print; exit }' "$file")"
  agents_line="$(awk '/^Agents:/ { print; exit }' "$file")"

  if [[ ! "$agents_line" =~ ^Agents:[[:space:]]+([0-9]+)/([0-9]+)[[:space:]]+succeeded$ ]]; then
    echo "malformed QUIC agents line in $file" >&2
    return 2
  fi
  successes="${BASH_REMATCH[1]}"
  total_agents="${BASH_REMATCH[2]}"
  total_ms="$(duration_to_ms "$total_duration")" || return 2
  rps="$(awk -v successes="$successes" -v total_ms="$total_ms" 'BEGIN { if (total_ms <= 0) print 0; else printf "%.6f", successes / (total_ms / 1000) }')"
  error_rate="$(awk -v successes="$successes" -v total_agents="$total_agents" 'BEGIN { if (total_agents <= 0) print 0; else printf "%.6f", (total_agents - successes) / total_agents }')"

  jq -nc \
    --arg commit "$COMMIT_SHA" \
    --arg timestamp "$TIMESTAMP" \
    --argjson rps "$rps" \
    --argjson error_rate "$error_rate" \
    '{
      source: "quic",
      scenario: "quic-agents",
      phase: "aggregate",
      rps: $rps,
      error_rate: $error_rate,
      commit: $commit,
      env: "ci",
      timestamp: $timestamp
    }'

  local phase label line
  for label in Connect Handshake Register; do
    phase="${label,,}"
    line="$(awk -v prefix="$label:" '$1 == prefix { print; exit }' "$file")"
    if [ -z "$line" ]; then
      if [ "$successes" -eq 0 ]; then
        continue
      fi
      echo "missing QUIC $label latency line in $file" >&2
      return 2
    fi
    if [[ ! "$line" =~ p50=([^[:space:]]+)[[:space:]]+p95=([^[:space:]]+)[[:space:]]+p99=([^[:space:]]+) ]]; then
      echo "malformed QUIC $label latency line in $file" >&2
      return 2
    fi
    emit_quic_phase_row "$phase" "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "${BASH_REMATCH[3]}" || return 2
  done
}

build_rows() {
  local tmp file scenario status
  tmp="$(mktemp)"
  status=0

  if [ -d "$K6_SUMMARY_DIR" ]; then
    while IFS= read -r -d '' file; do
      scenario="$(basename "$file" .json)"
      emit_k6_rows "$file" "$scenario" >>"$tmp" || {
        status=2
        break
      }
    done < <(find "$K6_SUMMARY_DIR" -maxdepth 1 -type f -name '*.json' -print0 | sort -z)
  fi

  if [ "$status" -eq 0 ]; then
    emit_quic_rows "$QUIC_OUTPUT_FILE" >>"$tmp" || status=2
  fi

  if [ "$status" -ne 0 ]; then
    rm -f "$tmp"
    return "$status"
  fi

  if [ ! -s "$tmp" ]; then
    echo "no load-test summaries found" >&2
    rm -f "$tmp"
    return 2
  fi

  jq -s 'sort_by(.source, .scenario, .phase)' "$tmp"
  status=$?
  rm -f "$tmp"
  return "$status"
}

main() {
  if [ "$#" -gt 0 ]; then
    echo "usage: $0" >&2
    return 2
  fi

  build_rows
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
