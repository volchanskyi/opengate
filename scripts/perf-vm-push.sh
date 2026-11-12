#!/usr/bin/env bash
# Pushes each leg's bundle rows into the trend, leaving out and naming a leg that measured nothing.
# A store probe always goes in; a series perf-regression-check.sh flagged carries flagged="1".
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
#   PERF_FLAGGED_FILE  the check's flagged series, one leg<TAB>phase<TAB>measurement a line
#
# Usage: perf-vm-push.sh <bundle.json>...   (a leg is named by the bundle's directory)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/loadtest-profile.sh
. "$HERE/lib/loadtest-profile.sh"

ROWS_FILE=""
cleanup() { [ -z "$ROWS_FILE" ] || rm -f "$ROWS_FILE"; }
trap cleanup EXIT

# catch_up_samples prints one part of the catch-up leg's readings: "probe" is the store probe and
# the runner's benchmark, pushed whatever the fleet's verdict; "fleet" is the fleet's own readings.
catch_up_samples() { # bundle leg workload processor part
  jq -r --arg leg "$2" --arg workload "$3" --arg processor "$4" --arg part "$5" '
    def label_escape: tostring | gsub("\\\\"; "\\\\") | gsub("\""; "\\\"");
    def cpu: if $processor == "" then "" else "processor=\"\($processor | label_escape)\"," end;
    def sample($name; $extra; $value):
      select($value != null)
      | "perf_\($name){env=\"ci\",leg=\"\($leg | label_escape)\",\(cpu)\($extra)workload=\"\($workload | label_escape)\"} \($value)";
    def share($part; $whole): if ($whole // 0) > 0 then (($part // 0) / $whole * 1000 | round / 1000) else null end;
    (.store_probe // {}) as $probe
    | if $part == "probe" then
        ([$probe.one_sender, $probe.two_senders] | map(select(. != null))[]
          | "senders=\"\(.senders)\"," as $s
          | sample("catchup_readings_per_second"; $s; .catch_up.readings_per_second),
            sample("catchup_live_within_5s_share"; $s; share(.live.within_five_seconds; .live.requests)),
            sample("catchup_live_p99_ms"; $s; .live.p99_ms),
            sample("catchup_store_peak_memory_bytes"; $s; .store.peak_memory_bytes)),
        sample("catchup_quiet_cpu_share"; ""; $probe.quiet.cpu_share),
        sample("catchup_quiet_memory_share"; ""; $probe.quiet.memory_share),
        sample("runner_benchmark_rounds_per_second"; ""; .runner.cpu_benchmark_rounds_per_second)
      else
        sample("catchup_fill_seconds"; ""; .catch_up.last_stored_seconds),
        sample("catchup_machines_caught_up"; ""; .catch_up.complete),
        sample("catchup_live_accepted_within_5s_share"; "";
          share(.live_acceptance.within_five_seconds; .live_acceptance.received))
      end
  ' "$1"
}

# capacity_samples prints the machines a ladder held and, for a scaling rung, whether its machines
# kept the profile's ordinary sign-in promise; each is read against its own nights on its chip.
capacity_samples() { # bundle leg workload processor
  local bundle="$1" leg="$2" workload="$3" processor="$4" name promise kept=""
  name="$(jq -r '.run.profile_name // empty' "$bundle")"
  if [ "$name" = scaling ]; then
    promise="$(signin_promise "$HERE/../load/profiles/$name.yaml")" || return 2
    [ -z "$promise" ] || kept="$(signin_promise_kept "$bundle" "$promise")"
  fi
  jq -r --arg leg "$leg" --arg workload "$workload" --arg processor "$processor" --arg kept "$kept" '
    def label_escape: tostring | gsub("\\\\"; "\\\\") | gsub("\""; "\\\"");
    def labels($phase): "env=\"ci\",leg=\"\($leg | label_escape)\",phase=\"\($phase)\","
      + (if $processor == "" then "" else "processor=\"\($processor | label_escape)\"," end)
      + "workload=\"\($workload | label_escape)\"";
    (if .breaking_point.held_agents == null then empty
     else "perf_breakpoint_held_agents{\(labels("ladder"))} \(.breaking_point.held_agents)" end),
    (if $kept == "" then empty else "perf_signin_promise_kept{\(labels("register"))} \($kept)" end)
  ' "$bundle"
}

# sizing_sample prints the smallest processor share whose scaling rung kept the ordinary sign-in
# promise, once every rung of the sweep measured the system; a rung missing would read as a rise.
sizing_sample() { # bundle...
  local bundle rungs=() name promise="" kept smallest="" version="" verdict cpus
  for bundle in "$@"; do
    [ "$(jq -r '.run.profile_name // empty' "$bundle")" = scaling ] && rungs+=("$bundle")
  done
  [ "${#rungs[@]}" -gt 0 ] || return 0
  for bundle in "${rungs[@]}"; do
    verdict="$(jq -r '.verdict.result // empty' "$bundle")"
    if [ "$verdict" = invalid ] || [ -z "$verdict" ]; then
      echo "::notice::the scaling sweep is missing a rung that measured the system, so no sizing figure is pushed." >&2
      return 0
    fi
    if [ -z "$promise" ]; then
      name="$(jq -r '.run.profile_name' "$bundle")"
      promise="$(signin_promise "$HERE/../load/profiles/$name.yaml")" || return 2
      [ -n "$promise" ] || return 0
      version="$(jq -r '.run.profile_version' "$bundle")"
    fi
    kept="$(signin_promise_kept "$bundle" "$promise")"
    cpus="$(jq -r '.target.cpus // empty' "$bundle")"
    if [ "$kept" = 1 ] && [ -n "$cpus" ] && { [ -z "$smallest" ] || awk -v a="$cpus" -v b="$smallest" 'BEGIN { exit !(a + 0 < b + 0) }'; }; then
      smallest="$cpus"
    fi
  done
  [ -z "$smallest" ] || printf 'perf_scaling_sizing_cpus{env="ci",workload="scaling/%s"} %s\n' "$version" "$smallest"
}

main() {
  if [ "$#" -eq 0 ]; then
    echo "usage: $0 <bundle.json>..." >&2
    return 2
  fi

  local bundle leg workload processor verdict rows metrics="" flagged='[]'
  if [ -n "${PERF_FLAGGED_FILE:-}" ] && [ -s "$PERF_FLAGGED_FILE" ]; then
    flagged="$(jq -Rnc '[inputs | split("\t") | select(length == 3)
      | {leg: .[0], phase: .[1], measurement: .[2]}]' <"$PERF_FLAGGED_FILE")"
  fi
  ROWS_FILE="$(mktemp)"
  rows="$ROWS_FILE"
  for bundle in "$@"; do
    if [ ! -s "$bundle" ]; then
      echo "::error::no evidence bundle at $bundle" >&2
      return 2
    fi
    leg="$(basename "$(dirname "$bundle")")"
    verdict="$(jq -r '.verdict.result // empty' "$bundle")"
    workload="$(jq -r '"\(.run.profile_name)/\(.run.profile_version)/\(.run.profile_digest)"' "$bundle")"
    processor="$(jq -r '.runner.processor_model // empty' "$bundle")"
    metrics+="$(catch_up_samples "$bundle" "$leg" "$workload" "$processor" probe)"$'\n'
    if [ "$verdict" = "invalid" ] || [ -z "$verdict" ]; then
      echo "::notice::leg $leg did not measure the system (verdict ${verdict:-absent}), so its rows stay out of the trend."
      continue
    fi
    "$HERE/loadtest-bundle-rows.sh" "$bundle" "$rows" >/dev/null
    metrics+="$(
      jq -r --arg leg "$leg" --arg workload "$workload" --arg processor "$processor" \
        --argjson flagged "$flagged" '
        def label_escape: tostring | gsub("\\\\"; "\\\\") | gsub("\""; "\\\"");
        # The processor a leg ran on, where it recorded one, so nights compare like with like.
        def cpu: if $processor == "" then "" else "processor=\"\($processor | label_escape)\"," end;
        def sample($measurement; $value):
          select($value != null)
          | . as $row
          | (if any($flagged[]; .leg == $leg and .phase == $row.phase and .measurement == $measurement)
              then "flagged=\"1\"," else "" end) as $flag
          | "perf_\($measurement){env=\"ci\",\($flag)leg=\"\($leg | label_escape)\",phase=\"\(.phase | label_escape)\",\(cpu)workload=\"\($workload | label_escape)\"} \($value)";
        # A level names its scenario, since the server and the database share the measured phase.
        def level($measurement; $value):
          select($value != null)
          | "perf_\($measurement){env=\"ci\",leg=\"\($leg | label_escape)\",phase=\"\(.phase | label_escape)\",\(cpu)scenario=\"\(.scenario | label_escape)\",workload=\"\($workload | label_escape)\"} \($value)";
        .[]
        | if .source == "service" then
            level("cpu_percent"; .cpu_percent), level("memory_percent"; .memory_percent)
          else
            sample("latency_p95_ms"; .latency_p95_ms),
            sample("latency_p50_ms"; .latency_p50_ms),
            sample("error_rate"; .error_rate)
          end
      ' "$rows"
    )"$'\n'
    metrics+="$(capacity_samples "$bundle" "$leg" "$workload" "$processor")"$'\n'
    metrics+="$(catch_up_samples "$bundle" "$leg" "$workload" "$processor" fleet)"$'\n'
  done

  metrics+="$(sizing_sample "$@")"$'\n'

  if [ -z "${metrics//$'\n'/}" ]; then
    echo "::error::no leg measured the system, so there is nothing for the trend." >&2
    return 1
  fi

  # shellcheck source=lib/vm-push.sh
  . "$HERE/lib/vm-push.sh"
  printf '%s' "$metrics" | vm_push
}

main "$@"
