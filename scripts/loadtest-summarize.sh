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
  local workload

  if ! workload="$(workload_name "$scenario")"; then
    echo "scenario $scenario declares no workload; add it to workload_name in $0" >&2
    return 2
  fi

  jq -c \
    --arg commit "$COMMIT_SHA" \
    --arg timestamp "$TIMESTAMP" \
    --arg scenario "$scenario" \
    --arg workload "$workload" \
    '
      # values accepts k6 v1.x flat and v0.x nested statistics and prefers the phase-tagged copy,
      # since a whole-run percentile mixes the climb, the load and the wind-down.
      def windowed($name):
        (.metrics | to_entries
          | map(select(.key | startswith($name + "{phase:")))
          | first | .value) // null;
      def values($name): (windowed($name) // .metrics[$name] // {}) | (.values // .);
      # wholeRun reads the whole run: k6 divides a sub-metric count by the run, so a windowed rate
      # reads a phase as slower than it was.
      def wholeRun($name): (.metrics[$name] // {}) | (.values // .);
      def compact: with_entries(select(.value != null));
      def base($phase): {
        source: "k6",
        scenario: $scenario,
        phase: $phase,
        workload: $workload,
        commit: $commit,
        env: "ci",
        timestamp: $timestamp
      };

      (base("http") + {
        latency_p50_ms: (values("http_req_duration")["p(50)"] // values("http_req_duration").med // null),
        latency_p95_ms: (values("http_req_duration")["p(95)"] // null),
        latency_p99_ms: (values("http_req_duration")["p(99)"] // null),
        rps: (wholeRun("http_reqs").rate // null),
        # Rate metrics carry the ratio as "value"; "rate" is the counter shape.
        error_rate: (values("http_req_failed").value // values("http_req_failed").rate // null),
        # dropped_iterations counts arrivals the generator could not offer, over the whole run, since
        # falling behind anywhere counts.
        dropped_iterations: (wholeRun("dropped_iterations").count // null)
      } | compact),
      # The relay row appears whenever the scenario recorded the metric, in either exporter shape.
      (if ((values("relay_msg_latency_ms") | length) > 0) then
        (base("relay") + {
          latency_p50_ms: (values("relay_msg_latency_ms")["p(50)"] // values("relay_msg_latency_ms").med // null),
          latency_p95_ms: (values("relay_msg_latency_ms")["p(95)"] // null),
          latency_p99_ms: (values("relay_msg_latency_ms")["p(99)"] // null),
          rps: (wholeRun("relay_msg_count").rate // null)
        } | compact)
      else empty end),
      # One row per operator journey the scenario timed, so a slow fleet list and a slow machine
      # page read apart.
      (["device_list", "device_detail", "command_accept"][] as $journey
        | ("journey_" + $journey + "_ms") as $metric
        | if ((values($metric) | length) > 0) then
            (base($journey) + {
              latency_p50_ms: (values($metric)["p(50)"] // values($metric).med // null),
              latency_p95_ms: (values($metric)["p(95)"] // null),
              latency_p99_ms: (values($metric)["p(99)"] // null)
            } | compact)
          else empty end)
    ' "$file"
}

# workload_name names what a scenario measures; the gate keys its window by it, so a changed
# workload, system size or fleet size takes a new name and starts a new series.
workload_name() {
  case "$1" in
    api-baseline) printf '%s\n' "member-journeys/3" ;;
    concurrent-agents) printf '%s\n' "fleet-reads/3" ;;
    relay-throughput) printf '%s\n' "relay-session-echo/3" ;;
    quic-agents) printf '%s\n' "fleet-arrival/3" ;;
    *) return 2 ;;
  esac
}

emit_quic_phase_row() {
  local phase="$1"
  local p50="$2"
  local p95="$3"
  local p99="$4"
  local workload="$5"
  local p50_ms p95_ms p99_ms

  p50_ms="$(duration_to_ms "$p50")" || return 2
  p95_ms="$(duration_to_ms "$p95")" || return 2
  p99_ms="$(duration_to_ms "$p99")" || return 2

  jq -nc \
    --arg commit "$COMMIT_SHA" \
    --arg timestamp "$TIMESTAMP" \
    --arg phase "$phase" \
    --arg workload "$workload" \
    --argjson p50 "$p50_ms" \
    --argjson p95 "$p95_ms" \
    --argjson p99 "$p99_ms" \
    '{
      source: "quic",
      scenario: "quic-agents",
      phase: $phase,
      workload: $workload,
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

  local window_duration agents_line successes total_agents stood_down window_ms rps error_rate workload

  if ! workload="$(workload_name quic-agents)"; then
    echo "scenario quic-agents declares no workload; add it to workload_name in $0" >&2
    return 2
  fi

  # The rate divides by the harness's arrival window; the fleet hold dominates the run's own clock
  # and would make a fleet that fully arrived read as collapsed.
  window_duration="$(awk '/^Arrival window:/ { sub(/^Arrival window:[[:space:]]*/, ""); print; exit }' "$file")"
  agents_line="$(awk '/^Agents:/ { print; exit }' "$file")"

  if [[ ! "$agents_line" =~ ^Agents:[[:space:]]+([0-9]+)/([0-9]+)[[:space:]]+succeeded$ ]]; then
    echo "malformed QUIC agents line in $file" >&2
    return 2
  fi
  successes="${BASH_REMATCH[1]}"
  total_agents="${BASH_REMATCH[2]}"

  # Machines cancelled when a level came down never registered, so counting them as errors would
  # publish the harness's own wind-down; the line is absent when none were stood down.
  stood_down="$(awk '/^Stood down:/ { print $3; exit }' "$file")"
  [ -n "$stood_down" ] || stood_down=0

  # The declared fleet is the denominator only while every machine lives once; a run that replaces
  # machines arrives more than it declared, and its evidence bundle states the share instead.
  if [ "$successes" -gt "$((total_agents - stood_down))" ]; then
    echo "$file arrived more machines ($successes) than the $total_agents it declared less the $stood_down it stood down, so the declared fleet is not the denominator of its error rate" >&2
    return 2
  fi

  # Arrivals with no window have no denominator, and the run's clock cannot stand in for it.
  if [ -z "$window_duration" ] && [ "$successes" -gt 0 ]; then
    echo "missing QUIC arrival window line in $file" >&2
    return 2
  fi

  window_ms=0
  if [ -n "$window_duration" ]; then
    window_ms="$(duration_to_ms "$window_duration")" || return 2
  fi
  rps="$(awk -v successes="$successes" -v window_ms="$window_ms" 'BEGIN { if (window_ms <= 0) print 0; else printf "%.6f", successes / (window_ms / 1000) }')"
  # Asked machines are every machine offered less the ones the run stood down.
  error_rate="$(awk -v successes="$successes" -v total_agents="$total_agents" -v stood="$stood_down" '
    BEGIN {
      asked = total_agents - stood
      if (asked <= 0) print 0
      else printf "%.6f", (asked - successes) / asked
    }')"

  jq -nc \
    --arg commit "$COMMIT_SHA" \
    --arg timestamp "$TIMESTAMP" \
    --argjson rps "$rps" \
    --argjson error_rate "$error_rate" \
    --arg workload "$workload" \
    '{
      source: "quic",
      scenario: "quic-agents",
      phase: "aggregate",
      workload: $workload,
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
      # Registration is the server's figure, so a run the server did not answer has none; connect and
      # handshake come from the generator alone, so their absence is a malformed block.
      if [ "$label" = "Register" ]; then
        continue
      fi
      echo "missing QUIC $label latency line in $file" >&2
      return 2
    fi
    if [[ ! "$line" =~ p50=([^[:space:]]+)[[:space:]]+p95=([^[:space:]]+)[[:space:]]+p99=([^[:space:]]+) ]]; then
      echo "malformed QUIC $label latency line in $file" >&2
      return 2
    fi
    emit_quic_phase_row "$phase" "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "${BASH_REMATCH[3]}" "$workload" || return 2
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
