#!/usr/bin/env bash
# Turns a run's evidence bundle into the canonical rows that loadtest-gate-check.sh reads.
# Only the series a limit names are emitted; an unreadable bundle fails, as an empty array passes.
#
# Usage:
#   loadtest-bundle-rows.sh <bundle.json> [rows.json]
set -euo pipefail

usage() {
  echo "usage: $0 <bundle.json> [rows.json]" >&2
}

# rows_from turns a bundle's observations into canonical rows: a bundle names what the harness
# measured, and a row names where in a night's shape the measurement sits.
rows_from() {
  jq -c '
    def share($part; $whole): if ($whole // 0) > 0 then ($part // 0) / $whole else null end;
    def metrics: ["error_rate", "latency_p50_ms", "latency_p95_ms", "arrival_share", "within_5s_share",
      "machines_caught_up", "fill_seconds", "unanswered", "deferred", "readings_per_second",
      "live_within_5s_share", "duration_seconds", "cpu_share", "memory_share", "peak_memory_share",
      "store_failures", "retained_goroutines_per_operation", "retained_bytes_per_operation", "growing_sites",
      "online_p95_seconds", "online_all_seconds"];
    (.observations // []) as $observed
    | (reduce $observed[] as $o ({}; .[$o.series] = $o.value)) as $value
    | {
        source: "quic",
        scenario: "quic-agents",
        commit: (.run.commit // "unknown"),
        env: (.run.environment // "unknown"),
        timestamp: (.run.finished_at // null),
        target_cpus: (.target.cpus // null)
      } as $base
    | [
        ($base + {phase: "aggregate", error_rate: $value.aggregate_error_rate}),
        ($base + {phase: "connect", latency_p95_ms: $value.connect_p95_ms}),
        # A run the server did not answer has no registration row, so its limits fail the night.
        # The p50 rides along: at the largest fleet the p95 is the queue and the p50 the write.
        (if $value.register_p95_ms == null then empty
         else ($base + {phase: "register",
                        latency_p95_ms: $value.register_p95_ms}
                     + (if $value.register_p50_ms == null then {}
                        else {latency_p50_ms: $value.register_p50_ms} end))
         end),
        # Each phase the harness timed, from its start to 95% and to every one of its machines online.
        (.phases[]? | select(.fleet_online_p95_seconds != null or .fleet_online_all_seconds != null)
         | $base + {scenario: "online", phase: .name, online_p95_seconds: .fleet_online_p95_seconds,
                    online_all_seconds: .fleet_online_all_seconds}),
        # Each phase where machines signed in, timed by the server over that phase alone.
        (.phases[]? | select(.register_p95_ms != null)
         | $base + {scenario: "register", phase: .name, latency_p50_ms: .register_p50_ms,
                    latency_p95_ms: .register_p95_ms}),
        # Only a measured phase counts a wave, so a run that judged none has no wave row.
        (if $value.wave_online_share == null then empty
         else ($base + {phase: "wave", arrival_share: $value.wave_online_share}
                     + (if $value.wave_online_p95_ms == null then {}
                        else {latency_p95_ms: $value.wave_online_p95_ms} end))
         end),
        # The catch-up promises: live readings within 5 s by the count the server keeps, a reading never
        # stored a miss, and every machine caught up by when.
        (if .live_acceptance == null then empty
         else ($base + {source: "catchup", scenario: "live", phase: "drain",
                        within_5s_share: (if (.live_acceptance.received // 0) > 0
                          then .live_acceptance.within_five_seconds / .live_acceptance.received
                          else 0 end)})
         end),
        (if .catch_up == null then empty
         else ($base + {source: "catchup", scenario: "gaps", phase: "drain",
                        machines_caught_up: (.catch_up.complete // 0),
                        fill_seconds: .catch_up.last_stored_seconds}),
              # Nothing lost is every batch answered; the batches told to wait sit beside the fill.
              ($base + {source: "catchup", scenario: "batches", phase: "drain",
                        unanswered: (.catch_up.unanswered // 0), deferred: (.catch_up.deferred // 0)})
         end),
        # The live minutes the watched machines sent, as they saw them stored.
        (if .live_delay == null then empty
         else ($base + {source: "catchup", scenario: "watched", phase: "drain",
                        within_5s_share: share(.live_delay.within_five_seconds; .live_delay.sent)})
         end),
        # The store probe, each sender and the quiet minute against the marks its profile names.
        (if .store_probe == null then empty
         else .store_probe as $p
           | ([$p.one_sender, $p.two_senders] | map(select(. != null) | .store.memory_limit_bytes) | first) as $limit
           | ($base + {source: "catchup", scenario: "probe-one", phase: "probe",
                       readings_per_second: $p.one_sender.catch_up.readings_per_second,
                       live_within_5s_share: share($p.one_sender.live.within_five_seconds; $p.one_sender.live.requests),
                       duration_seconds: $p.one_sender.duration_seconds}),
             ($base + {source: "catchup", scenario: "probe-two", phase: "probe",
                       readings_per_second: $p.two_senders.catch_up.readings_per_second,
                       live_within_5s_share: share($p.two_senders.live.within_five_seconds; $p.two_senders.live.requests),
                       duration_seconds: $p.two_senders.duration_seconds}),
             ($base + {source: "catchup", scenario: "probe-quiet", phase: "probe",
                       cpu_share: $p.quiet.cpu_share, memory_share: $p.quiet.memory_share}),
             ($base + {source: "catchup", scenario: "probe-store", phase: "probe",
                       peak_memory_share: ([$p.one_sender.store.peak_memory_bytes, $p.two_senders.store.peak_memory_bytes]
                         | map(select(. != null)) | if length == 0 or ($limit // 0) == 0 then null
                           else max / $limit end),
                       store_failures: ([$p.oom_killed, $p.one_sender.store.restarted, $p.one_sender.store.stopped_answering,
                         $p.two_senders.store.restarted, $p.two_senders.store.stopped_answering]
                         | map(select(. == true)) | length)})
         end),
        # The technicians, timed beside the fleet, under the names the in-cluster test gives them.
        ((.journeys // [])[] as $j
         | ({"command-accept": ["api-baseline", "command_accept"], "device-detail": ["api-baseline", "device_detail"],
             "device-list": ["api-baseline", "device_list"], "relay-session-echo": ["relay-throughput", "relay"]}[$j.name]
             // ["journeys", $j.name]) as $where
         | $base + {source: "k6", scenario: $where[0], phase: $where[1], error_rate: $j.error_rate,
                    latency_p50_ms: $j.latency_p50_ms, latency_p95_ms: $j.latency_p95_ms}),
        # The leak readings of a soak: what each completed operation left behind, and the heap sites
        # that grew in more than half the intervals.
        (if .leak_trail == null then empty
         else ($base + {source: "leak", scenario: "server", phase: "run",
                        retained_goroutines_per_operation: $value.target_retained_goroutines_per_operation,
                        retained_bytes_per_operation: $value.target_retained_bytes_per_operation,
                        growing_sites: ([(.leak_trail.growth // [])[]
                          | select(.intervals_grown * 2 > (.intervals // 0))] | length)})
         end),
        # The rungs of a ladder: the worst of those that held, against the ordinary promises, and the one
        # that gave, against the same marks as a watched line.
        (if .breaking_point == null then empty
         else .breaking_point as $bp
           | [.phases[] | select(.name | startswith("step-"))] as $rungs
           | ([$rungs[] | select(.offered_connected_agents <= ($bp.held_agents // 0))]) as $held
           | (if ($held | length) == 0 then empty
              else ($base + {phase: "rungs-held", error_rate: ([$held[].error_rate] | max),
                             latency_p95_ms: ([$held[].latency_p95_ms] | max)}) end),
             ([$rungs[] | select(.name == $bp.gave_at)][0] as $gave
              | if $gave == null then empty
                else ($base + {phase: "rung-gave", error_rate: $gave.error_rate,
                               latency_p95_ms: $gave.latency_p95_ms}) end)
         end)
      ]
    | map(select(. as $row | [metrics[] | $row[.]] | any(. != null)))
  ' "$1"
}

# shellcheck source=scripts/lib/loadtest-levels.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/loadtest-levels.sh"

main() {
  if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
    usage
    return 2
  fi

  local bundle="$1" out="${2:-}"

  if [ ! -s "$bundle" ]; then
    echo "::error::there is no evidence bundle at $bundle, so the numbers this night is judged by cannot be read — which is not the same as no limit being breached." >&2
    return 2
  fi

  local rows levels
  if ! rows="$(rows_from "$bundle")" || ! levels="$(levels_rows "$bundle")"; then
    echo "::error::$bundle could not be read as an evidence bundle." >&2
    return 2
  fi

  # Every machine-side bundle states both series; answering with one would hand the other's
  # limits a night they cannot fail.
  local phase
  for phase in aggregate connect; do
    if [ "$(jq --arg p "$phase" '[.[] | select(.phase == $p)] | length' <<<"$rows")" -eq 0 ]; then
      echo "::error::$bundle states no $phase reading, so the limits held against it would be read against nothing." >&2
      return 2
    fi
  done

  # The levels are read wherever the run took them, so they join after the machine-side check.
  rows="$(jq -c --argjson levels "$levels" '. + $levels' <<<"$rows")"

  if [ -n "$out" ]; then
    printf '%s\n' "$rows" >"$out"
    echo "read $(jq 'length' <<<"$rows") row(s) off $bundle into $out"
    return 0
  fi

  printf '%s\n' "$rows"
  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
