#!/usr/bin/env bash
# Reads a night's canonical rows against the profile's own limits.
# A limit whose measurement never arrived fails the night, so a missing row cannot read as a pass.
#
# Usage:
#   loadtest-gate-check.sh <profile.yaml> <loadtest-summary.json> [breaches.json]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/loadtest-profile.sh
. "$SCRIPT_DIR/lib/loadtest-profile.sh"

usage() {
  echo "usage: $0 <profile.yaml> <loadtest-summary.json> [breaches.json]" >&2
}

# breaches_for prints one "blocking<TAB>message" or "advisory<TAB>message" line per breached limit.
# jq does the comparison because a shell compares these floats as strings and ranks 90 above 200.
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
        "\($kind)\t\($gate.series) \($gate.metric) is limited, and no row for it ever arrived — a limit on a measurement nothing produced reads as a limit nothing can breach"
      else
        ($matched[0][$gate.metric]) as $value
        | if $value == null then
            "\($kind)\t\($gate.series) \($gate.metric) is limited, and the row that arrived carries no such number"
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

  # A profile with no limits fails, since its output matches a night that cleared everything.
  if [ "$(jq 'length' <<<"$gates")" -eq 0 ]; then
    echo "::error::$profile declares no limits, so this call would report a clean night having checked nothing." >&2
    return 2
  fi

  local lines blocking=0 advisory=0
  lines="$(breaches_for "$gates" "$summary")"

  # Each entry records whether the profile enforces it, so a mark that only reports never fails
  # the night.
  local recorded="[]"
  while IFS=$'\t' read -r kind message; do
    [ -n "${message:-}" ] || continue
    if [ "$kind" = "blocking" ]; then
      blocking=$((blocking + 1))
      echo "::error::$message" >&2
    else
      advisory=$((advisory + 1))
      echo "reported, not enforced: $message"
    fi
    recorded="$(jq -c --arg message "$message" --argjson enforced "$([ "$kind" = blocking ] && echo true || echo false)" \
      '. + [{enforced: $enforced, message: $message}]' <<<"$recorded")"
  done <<<"$lines"

  if [ -n "$out" ]; then
    jq '.' <<<"$recorded" >"$out"
  fi

  echo "gates: $(jq 'length' <<<"$gates") read, $blocking breached, $advisory reported"
  [ "$blocking" -eq 0 ] || return 1
  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
