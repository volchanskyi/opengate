#!/usr/bin/env bash
# Folds the weight, cleanup, runner and journey readings taken beside a run into its bundle,
# and fails a merge that finds nothing to merge.
#
# Usage:
#   loadtest-bundle-merge.sh <bundle.json> [--weight F] [--cleanup F] [--runner F] [--journeys F]...
set -euo pipefail

usage() {
  echo "usage: $0 <bundle.json> [--weight <fixture-weight.json>] [--cleanup <cleanup-proof.json>] [--runner <runner.json>] [--journeys <k6-export.json>]..." >&2
}

# journeys_from reads statistics flat (k6 v1) or nested under "values", and prefers a phase-tagged
# copy, since a whole-run percentile mixes the ramp, the load and the wind-down.
journeys_from() {
  jq '
    def windowed($name):
      (.metrics | to_entries
        | map(select(.key | startswith($name + "{phase:")))
        | first | .value) // null;
    def stats($name): (windowed($name) // .metrics[$name] // {}) | (.values // .);
    def carried: startswith("journey_") and endswith("_ms");
    def named:
      if . == "relay_msg_latency_ms" then "relay-session-echo"
      else (ltrimstr("journey_") | rtrimstr("_ms") | gsub("_"; "-")) end;

    . as $doc
    | [
        $doc.metrics
        | keys[]
        | split("{")[0]
        | select(carried or . == "relay_msg_latency_ms")
      ]
    | unique
    | map(. as $metric | ($doc | stats($metric)) as $s | {
        name: ($metric | named),
        requests: ($s.count // 0 | floor),
        error_rate: 0,
        latency_p50_ms: ($s["p(50)"] // $s.med // 0),
        latency_p95_ms: ($s["p(95)"] // 0)
      })
    | sort_by(.name)' "$1"
}

# arrivals_from prints, for each phase the export times arrivals in, how many of the technician
# arrivals completed and their 95th-percentile time.
arrivals_from() {
  jq '
    def stats($key): (.metrics[$key] // {}) | (.values // .);
    . as $doc
    | [.metrics | keys[] | select(startswith("iterations{phase:"))
        | ltrimstr("iterations{phase:") | rtrimstr("}")]
    | unique
    | map(. as $phase | {
        key: $phase,
        value: {
          completed: ($doc | stats("iterations{phase:" + $phase + "}").count // 0),
          p95: ($doc | stats("iteration_duration{phase:" + $phase + "}")["p(95)"])
        }
      })
    | from_entries' "$1"
}

# with_arrivals sets each timed phase's completed share against the arrivals it offered over its
# own span, with their 95th-percentile time; both are watched and judge nothing.
with_arrivals() {
  jq --argjson timed "$1" '
    def epoch:
      capture("^(?<d>[0-9-]+T[0-9:]+)(\\.(?<f>[0-9]+))?(?<z>Z|[+-][0-9]{2}:[0-9]{2})$")
      | ((.d + "Z") | fromdateiso8601) + (("0." + (.f // "0")) | tonumber)
        - (if .z == "Z" then 0
           else ((.z[0:1] + "1") | tonumber) * ((.z[1:3] | tonumber) * 3600 + (.z[4:6] | tonumber) * 60)
           end);
    if .phases == null then . else
      .phases |= map(
        . as $phase
        | ($timed[$phase.name] // null) as $t
        | (($phase.finished_at | epoch) - ($phase.started_at | epoch)) as $span
        | ($phase.offered_operator_arrivals_per_second // 0) as $rate
        | if $t == null or $rate <= 0 or $span <= 0 then .
          else
            .operator_completed_share = ($t.completed / ($rate * $span))
            | if $t.p95 == null then . else .operator_journey_p95_ms = $t.p95 end
          end)
    end'
}

# refusals_from prints one export's request count and its refused-request count over the whole run.
# A count the export lacks reads -1.
refusals_from() {
  jq -r '
    def stats($name): (.metrics[$name] // {}) | (.values // .);
    [(stats("http_reqs").count // -1), (stats("requests_refused").count // -1)]
    | map(floor) | @tsv' "$1"
}

main() {
  local bundle="" weight="" cleanup="" runner="" merged=0
  # Each export is named, since two generators write files of their own and a fold that
  # replaced the first would discard its numbers.
  local -a journeys=()
  if [ "$#" -lt 1 ]; then
    usage
    return 2
  fi
  bundle="$1"
  shift

  while [ "$#" -gt 0 ]; do
    case "$1" in
      --weight)
        weight="${2:-}"
        shift 2
        ;;
      --cleanup)
        cleanup="${2:-}"
        shift 2
        ;;
      --runner)
        runner="${2:-}"
        shift 2
        ;;
      --journeys)
        journeys+=("${2:-}")
        shift 2
        ;;
      *)
        usage
        return 2
        ;;
    esac
  done

  if [ ! -s "$bundle" ]; then
    echo "::error::there is no evidence bundle at $bundle to fold anything into." >&2
    return 1
  fi
  if [ -z "$weight" ] && [ -z "$cleanup" ] && [ -z "$runner" ] && [ "${#journeys[@]}" -eq 0 ]; then
    echo "::error::nothing was named to merge, so this call would report success for no work." >&2
    return 2
  fi

  local updated
  updated="$(cat "$bundle")"

  if [ -n "$weight" ]; then
    if [ ! -s "$weight" ]; then
      echo "::error::$weight holds no weighing, so the fleet's weight on disk would reach the evidence as a zero." >&2
      return 1
    fi
    # The series count is read where the weighing writes it, under `counts`.
    updated="$(
      jq --argjson w "$(jq '{fixture_bytes, telemetry_series: .counts.telemetry_series}' "$weight")" '
        .fixture.database_bytes = ($w.fixture_bytes // 0)
        | .fixture.telemetry_series = ($w.telemetry_series // 0)
      ' <<<"$updated"
    )"
    merged=$((merged + 1))
  fi

  # The cleanup step's proof is carried as counted, residue included, so the bundle's own
  # validation refuses an unclean run.
  if [ -n "$cleanup" ]; then
    if [ ! -s "$cleanup" ]; then
      echo "::error::$cleanup holds no cleanup proof, so what the run left behind would reach the evidence uncounted." >&2
      return 1
    fi
    updated="$(
      jq --argjson p "$(jq -c '.' "$cleanup")" '
        .cleanup = {
          verified: ($p.verified == true),
          orphan_users: $p.orphan_users,
          orphan_devices: $p.orphan_devices,
          orphan_organizations: $p.orphan_organizations,
          orphan_sites: $p.orphan_sites
        }
      ' <<<"$updated"
    )"
    merged=$((merged + 1))
  fi

  # What the runner did is carried as reported numbers, beside the readings it can explain.
  if [ -n "$runner" ]; then
    if [ ! -s "$runner" ]; then
      echo "::error::$runner holds no runner reading, so the leg would carry no account of its machine." >&2
      return 1
    fi
    updated="$(jq --argjson r "$(jq -c '.' "$runner")" '.runner = $r' <<<"$updated")"
    merged=$((merged + 1))
  fi

  if [ "${#journeys[@]}" -gt 0 ]; then
    local exported rows all='[]' asked turned_away requests=0 refused=0 timed='{}'
    for exported in "${journeys[@]}"; do
      if [ ! -s "$exported" ]; then
        echo "::error::$exported holds no export, so this night's journeys would reach the evidence as a null." >&2
        return 1
      fi
      rows="$(journeys_from "$exported")"
      if [ "$(jq 'length' <<<"$rows")" -eq 0 ]; then
        echo "::error::$exported names no journeys, so the screens this night timed are not in it." >&2
        return 1
      fi
      all="$(jq -c --argjson rows "$rows" '. + $rows' <<<"$all")"
      timed="$(jq -c --argjson more "$(arrivals_from "$exported")" '. + $more' <<<"$timed")"

      # A counter nobody incremented is absent from the export, so the scenarios add a zero per
      # answered request; an export with requests but no such counter did not count refusals.
      asked=""
      turned_away=""
      IFS=$'\t' read -r asked turned_away < <(refusals_from "$exported" 2>/dev/null) || true
      if [ "${asked:--1}" -le 0 ]; then
        echo "::error::$exported names no request the run made, so it cannot say that nothing was turned away." >&2
        return 1
      fi
      if [ "${turned_away:--1}" -lt 0 ]; then
        echo "::error::$exported counted no refusals at all, so a night refused at the door reads the same as one that was not." >&2
        return 1
      fi
      requests=$((requests + asked))
      refused=$((refused + turned_away))
      merged=$((merged + 1))
    done
    updated="$(jq --argjson j "$(jq -c 'sort_by(.name)' <<<"$all")" '.journeys = $j' <<<"$updated")"
    updated="$(with_arrivals "$timed" <<<"$updated")"
    updated="$(
      jq --argjson requests "$requests" --argjson refused "$refused" \
        '.refusals = { requests: $requests, refused: $refused }' <<<"$updated"
    )"
  fi

  printf '%s\n' "$updated" >"$bundle"
  echo "folded $merged reading(s) into $bundle"
  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
