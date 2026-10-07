#!/usr/bin/env bash
# Folds the weight, cleanup and journey readings taken beside a run into its evidence bundle.
# A merge that finds nothing to merge fails, so a step that produced nothing cannot pass.
#
# Usage:
#   loadtest-bundle-merge.sh <bundle.json> [--weight FILE] [--cleanup FILE] [--journeys FILE]...
set -euo pipefail

usage() {
  echo "usage: $0 <bundle.json> [--weight <fixture-weight.json>] [--cleanup <cleanup-proof.json>] [--journeys <k6-export.json>]..." >&2
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

# refusals_from prints one export's request count and its refused-request count over the whole run.
# A count the export lacks reads -1.
refusals_from() {
  jq -r '
    def stats($name): (.metrics[$name] // {}) | (.values // .);
    [(stats("http_reqs").count // -1), (stats("requests_refused").count // -1)]
    | map(floor) | @tsv' "$1"
}

main() {
  local bundle="" weight="" cleanup="" merged=0
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
  if [ -z "$weight" ] && [ -z "$cleanup" ] && [ "${#journeys[@]}" -eq 0 ]; then
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

  if [ "${#journeys[@]}" -gt 0 ]; then
    local exported rows all='[]' asked turned_away requests=0 refused=0
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
