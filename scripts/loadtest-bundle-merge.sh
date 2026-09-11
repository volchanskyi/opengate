#!/usr/bin/env bash
# Fold the readings taken beside a run into the run's own evidence.
#
# Two of the numbers a bundle declares are measured by steps other than the
# harness, and both were reaching nothing. The fleet's weight on disk is read
# from the database after the fleet exists, which is after the harness has
# finished; the technician journeys are timed by the browser-side generator,
# which runs in a different pod. Each wrote its figure into a file of its own
# that no later reader opened, so the bundle — the one artifact that outlives
# the metrics store's thirty days — carried a null where the family's whole
# finding belongs.
#
# This is not a later query of the system under test. The doctrine the bundle is
# built on refuses those, because a bundle assembled from a query describes the
# system at the time of the query rather than the moment being reported. These
# are measurements of this run, taken by this run, arriving at the only place a
# reader can find them.
#
# A merge that found nothing to merge fails. The alternative is the shape this
# exists to close: a step that produced nothing, exited zero, and left the gap
# to surface later as a field nobody can explain.
#
# Usage:
#   loadtest-bundle-merge.sh <bundle.json> [--weight <fixture-weight.json>] [--journeys <k6-export.json>]
set -euo pipefail

usage() {
  echo "usage: $0 <bundle.json> [--weight <fixture-weight.json>] [--journeys <k6-export.json>]" >&2
}

# journeys_from turns a browser-side export into the bundle's own journey shape.
# Only the named journeys are carried: every other series in that export belongs
# to the request path rather than to a screen somebody opens.
#
# The statistics are read whichever way the exporter nests them. k6 v1.x writes
# them flat on the metric and v0.x nested them under "values", and reading only
# the nested shape is not a wrong number — it is three zeros. Every field falls
# back to nought, so a bundle produced by the pinned exporter declared that
# opening a fleet list, opening a machine and sending an instruction each took
# no time at all, which is the healthiest figure a server can report. The
# canonical row extraction had already been bitten by this exact nesting and
# repaired; this reader was not, and its fixtures were written in the shape it
# reads rather than in the shape the exporter writes.
#
# A phase-tagged copy wins where there is one, for the reason the extraction
# gives: a percentile over the whole run is a mixture of the climb to the load,
# the load, and the wind-down away from it.
journeys_from() {
  jq '
    def windowed($name):
      (.metrics | to_entries
        | map(select(.key | startswith($name + "{phase:")))
        | first | .value) // null;
    def stats($name): (windowed($name) // .metrics[$name] // {}) | (.values // .);

    . as $doc
    | [
        $doc.metrics
        | keys[]
        | select(startswith("journey_") and endswith("_ms"))
        | split("{")[0]
      ]
    | unique
    | map(. as $metric | ($doc | stats($metric)) as $s | {
        name: ($metric | ltrimstr("journey_") | rtrimstr("_ms") | gsub("_"; "-")),
        requests: ($s.count // 0 | floor),
        error_rate: 0,
        latency_p50_ms: ($s["p(50)"] // $s.med // 0),
        latency_p95_ms: ($s["p(95)"] // 0)
      })
    | sort_by(.name)' "$1"
}

main() {
  local bundle="" weight="" journeys="" merged=0
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
      --journeys)
        journeys="${2:-}"
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
  if [ -z "$weight" ] && [ -z "$journeys" ]; then
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
    updated="$(
      jq --argjson w "$(jq '{fixture_bytes, telemetry_series}' "$weight")" '
        .fixture.database_bytes = ($w.fixture_bytes // 0)
        | .fixture.telemetry_series = ($w.telemetry_series // 0)
      ' <<<"$updated"
    )"
    merged=$((merged + 1))
  fi

  if [ -n "$journeys" ]; then
    if [ ! -s "$journeys" ]; then
      echo "::error::$journeys holds no export, so this night's journeys would reach the evidence as a null." >&2
      return 1
    fi
    local rows
    rows="$(journeys_from "$journeys")"
    if [ "$(jq 'length' <<<"$rows")" -eq 0 ]; then
      echo "::error::$journeys names no journeys, so the screens this night timed are not in it." >&2
      return 1
    fi
    updated="$(jq --argjson j "$rows" '.journeys = $j' <<<"$updated")"
    merged=$((merged + 1))
  fi

  printf '%s\n' "$updated" >"$bundle"
  echo "folded $merged reading(s) into $bundle"
  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
