#!/usr/bin/env bash
# Read a night's canonical rows against the profile's own limits.
#
# Every profile declares limits, keyed by the same source/scenario/phase triple
# the rows carry. Nothing read them. The schema checked each was well-formed and
# then no code consumed one, so every limit in all seven profiles was
# decoration — including the ones marked as failing the run.
#
# They could not be read where the run classifies itself, either: that happens
# inside the machine-side harness, which holds phases and machines and no
# browser-side row at all. Twelve of the sixteen limits across the profiles name
# a browser-side measurement, so from in there they were unreachable by
# construction. This runs in the publish step, where both halves of the night
# have been joined into one file, which is the first place both exist.
#
# A limit whose measurement never arrived fails the night. It reads as a passing
# limit forever otherwise — the same false green as a step that reports success
# for work it was refused.
#
# Usage: loadtest-gate-check.sh <profile.yaml> <loadtest-summary.json> [breaches.json]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/loadtest-profile.sh
. "$SCRIPT_DIR/lib/loadtest-profile.sh"

usage() {
  echo "usage: $0 <profile.yaml> <loadtest-summary.json> [breaches.json]" >&2
}

# breaches_for compares every limit against the rows and prints one line per
# breach: "blocking<TAB>message" or "advisory<TAB>message".
#
# The comparison is done in jq rather than in a shell loop because these are
# floating-point numbers, and a shell that compares them as strings decides 90
# is greater than 200.
breaches_for() {
  local gates="$1" rows="$2"

  jq -r --argjson gates "$gates" '
    . as $rows
    | $gates[]
    | . as $gate
    | ($gate.series | split("/")) as $parts
    | ($rows | map(select(
        .source == $parts[0] and .scenario == $parts[1] and .phase == $parts[2]
      ))) as $matched
    | (if $gate.blocking then "blocking" else "advisory" end) as $kind
    | if ($matched | length) == 0 then
        "\($kind)\t\($gate.series) \($gate.metric) is limited, and no row for it never arrived — a limit on a measurement nothing produced reads as a limit nothing can breach"
      else
        ($matched[0][$gate.metric]) as $value
        | if $value == null then
            "\($kind)\t\($gate.series) \($gate.metric) is limited, and the row that never arrived carries no such number"
          elif ($gate.max != null and $value > $gate.max) then
            "\($kind)\t\($gate.series) \($gate.metric) is \($value), past the \($gate.max) it is held to"
          elif ($gate.min != null and $value < $gate.min) then
            "\($kind)\t\($gate.series) \($gate.metric) is \($value), below the \($gate.min) it is held to"
          else empty end
      end
  ' "$rows"
}

main() {
  if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
    usage
    return 2
  fi

  local profile="$1" summary="$2" out="${3:-}"

  if [ ! -s "$summary" ]; then
    echo "::error::there are no canonical rows at $summary, so nothing can be read against the profile's limits." >&2
    return 2
  fi

  local gates
  gates="$(profile_gates "$profile")" || return 2

  # A profile that declares nothing must not read as a night that cleared
  # everything. The two are the same output and opposite facts.
  if [ "$(jq 'length' <<<"$gates")" -eq 0 ]; then
    echo "::error::$profile declares no limits, so this call would report a clean night having checked nothing." >&2
    return 2
  fi

  local lines blocking=0 advisory=0
  lines="$(breaches_for "$gates" "$summary")"

  local messages=()
  while IFS=$'\t' read -r kind message; do
    [ -n "${message:-}" ] || continue
    messages+=("$message")
    if [ "$kind" = "blocking" ]; then
      blocking=$((blocking + 1))
      echo "::error::$message" >&2
    else
      advisory=$((advisory + 1))
      echo "reported, not enforced: $message"
    fi
  done <<<"$lines"

  if [ -n "$out" ]; then
    printf '%s\n' "${messages[@]+"${messages[@]}"}" \
      | jq -Rn '[inputs | select(length > 0)]' >"$out"
  fi

  echo "gates: $(jq 'length' <<<"$gates") read, $blocking breached, $advisory reported"
  [ "$blocking" -eq 0 ] || return 1
  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
