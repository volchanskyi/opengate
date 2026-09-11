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
      # k6 v1.x writes a metric statistics flat on the metric object; v0.x
      # nested them under "values". Accept both so the extraction does not
      # depend on the exporter generation.
      #
      # The phase-tagged copy wins where there is one. A percentile over the
      # whole run spans the climb to the load, the load itself and the wind-down
      # away from it, so it describes a mixture of three systems, and that
      # mixture moves whenever the climb takes a different share of the run —
      # which is a change nobody made to the product. A scenario tags every
      # request with the phase it belongs to and names the measured phase in a
      # threshold, which is what puts that phase into the export; only one phase
      # is ever marked, so at most one such key exists.
      def windowed($name):
        (.metrics | to_entries
          | map(select(.key | startswith($name + "{phase:")))
          | first | .value) // null;
      def values($name): (windowed($name) // .metrics[$name] // {}) | (.values // .);
      # Deliberately not windowed. k6 divides a sub-metric count by the whole
      # run rather than by the phase, so a windowed rate reads a phase as slower
      # than it was, in proportion to how much of the run it did not cover.
      # Measured against a floor that asks whether the generator offered work at
      # all, the whole run is the honest denominator anyway.
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
        # The generator saying it could not keep the rate the profile declared.
        # An arrival-rate run with nothing reading this degrades quietly back
        # into the closed loop it replaced: offered load falls with the server,
        # latency stays flat, and the night reports a healthy system nobody
        # finished asking. It is taken over the whole run rather than over one
        # phase, because a generator that fell behind anywhere fell behind.
        dropped_iterations: (wholeRun("dropped_iterations").count // null)
      } | compact),
      # Present the relay row whenever the scenario recorded the metric at all.
      # Keying this on the v0.x "values" nesting made the row unreachable under
      # the pinned exporter while three ceilings still named it, so the guard
      # reads the same shape-tolerant helper the statistics below do.
      (if ((values("relay_msg_latency_ms") | length) > 0) then
        (base("relay") + {
          latency_p50_ms: (values("relay_msg_latency_ms")["p(50)"] // values("relay_msg_latency_ms").med // null),
          latency_p95_ms: (values("relay_msg_latency_ms")["p(95)"] // null),
          latency_p99_ms: (values("relay_msg_latency_ms")["p(99)"] // null),
          rps: (wholeRun("relay_msg_count").rate // null)
        } | compact)
      else empty end),
      # One row per operator journey, present only where the scenario timed it.
      # A single interface-wide figure could not say whether a slow night was a
      # slow fleet list or a slow machine page, and those are different pieces of
      # work with different marks.
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

# workload_name is what a scenario measures, named.
#
# A trend compares a number against the numbers before it, and that is sound only
# while the scenario keeps measuring the same thing. The relay scenario had been
# timing an unauthenticated health check; it was rewritten to open a real session
# and time its own frame coming back, kept its name, and the first night of the
# new work was reported as a collapse against the old work's figures — a window
# median of 1ms judging an 8ms session create, and a throughput floor built from
# a request that did no relaying. Nothing in the stored data could say the two
# were different work.
#
# So the name travels with every sample. The gate keys its window by it, which
# makes a rewritten scenario a new series: it compares against itself, or, until
# three nights of it exist, against the absolute ceilings alone — which are
# recalibrated in the same commit that does the rewriting.
#
# Changing what a scenario measures means changing the name here. A scenario
# with no name cannot enter the trend: an unnamed workload is exactly the
# ambiguity this removes.
#
# Resizing the system the work is offered to counts as changing it, for the same
# reason and with the same consequence, and so does changing the fleet the work
# runs against. Three of those landed together: staging now reserves and is
# capped at what production is, a quarter of a processor where it used to burst
# to half; the nightly walks the everyday profile rather than offering its whole
# fleet at once; and the fleet it holds is five hundred machines rather than a
# hundred. Every figure from here is a reading of a different pair, and a window
# median spanning both would be a comparison nobody could interpret.
#
# The browser-side names are at /3, and the change that moved them is the
# largest of the three so far: the load each offers is the profile's now rather
# than a shape the scenario carried, it is offered as an arrival rate rather
# than as a fixed count of virtual users waiting on their own replies, each
# technician presents an address of its own so the work is not held behind one
# request allowance, and the percentile is taken over the phase the profile
# marks rather than over the climb and the wind-down as well. Every one of those
# changes what the number is a reading of, so a window median spanning both
# sides of it would be a comparison nobody could interpret.
#
# The machine-side name is untouched: nothing above it changed.
workload_name() {
  case "$1" in
    api-baseline) printf '%s\n' "member-journeys/3" ;;
    concurrent-agents) printf '%s\n' "fleet-reads/3" ;;
    relay-throughput) printf '%s\n' "relay-session-echo/3" ;;
    quic-agents) printf '%s\n' "fleet-arrival/2" ;;
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

  # The arrival window, not the run's own clock. A run holds its fleet connected
  # so the k6 relay scenario has machines to open sessions against, and the hold
  # is nearly all of the wall clock — so a rate taken from it reports the hold
  # rather than the arrival, and a fleet that entirely arrived reads as one that
  # collapsed. The harness prints the window it measured; this divides by that.
  window_duration="$(awk '/^Arrival window:/ { sub(/^Arrival window:[[:space:]]*/, ""); print; exit }' "$file")"
  agents_line="$(awk '/^Agents:/ { print; exit }' "$file")"

  if [[ ! "$agents_line" =~ ^Agents:[[:space:]]+([0-9]+)/([0-9]+)[[:space:]]+succeeded$ ]]; then
    echo "malformed QUIC agents line in $file" >&2
    return 2
  fi
  successes="${BASH_REMATCH[1]}"
  total_agents="${BASH_REMATCH[2]}"

  # Machines the run itself cancelled when a level came down. They never
  # registered, so the succeeded count is short by them while nothing failed —
  # and reading that shortfall as errors publishes an error rate about the
  # harness's own wind-down against a limit held at nought. The line is absent
  # on a run that stood nobody down.
  stood_down="$(awk '/^Stood down:/ { print $3; exit }' "$file")"
  [ -n "$stood_down" ] || stood_down=0

  # A block reporting arrivals with no window has no denominator this may use.
  # Falling back to the run's clock is the defect above, arrived at quietly, so
  # the extraction refuses rather than publishing a number it cannot stand behind.
  if [ -z "$window_duration" ] && [ "$successes" -gt 0 ]; then
    echo "missing QUIC arrival window line in $file" >&2
    return 2
  fi

  window_ms=0
  if [ -n "$window_duration" ]; then
    window_ms="$(duration_to_ms "$window_duration")" || return 2
  fi
  rps="$(awk -v successes="$successes" -v window_ms="$window_ms" 'BEGIN { if (window_ms <= 0) print 0; else printf "%.6f", successes / (window_ms / 1000) }')"
  # Over the machines that actually asked the server for something, which is
  # every machine the run offered less the ones it stood down itself.
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
      # Registration is the server's figure, taken where the device row lands,
      # and a run the server did not answer has none. Publishing the harness's
      # own clock instead is what two ceilings sat on: it stops at a local send
      # buffer and reports microseconds whatever the write behind it costs. So
      # the row is absent rather than wrong. Connect and handshake are the
      # generator's own side of the wire and it is the only side that can see
      # them, so their absence is a malformed block.
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
