#!/usr/bin/env bash
# Reads the run verdict from the evidence bundle and fails the step when it is invalid, meaning the
# system was never measured; an absent or unreadable bundle fails the same way.
#
# Usage: perf-bundle-verdict.sh <bundle.json>
set -euo pipefail

usage() {
  echo "usage: $0 <bundle.json>" >&2
}

# The refused-request share, in percent, past which the night earns a warning; a healthy run refuses
# nothing, so one in a hundred already marks a partly broken chain.
REFUSAL_SHARE_WORTH_SAYING=1

main() {
  if [ "$#" -ne 1 ]; then
    usage
    return 2
  fi

  local bundle="$1" result reasons
  if [ ! -s "$bundle" ]; then
    echo "::error::the run wrote no evidence bundle at $bundle, so what it measured is unknown." >&2
    return 1
  fi

  if ! result="$(jq -er '.verdict.result' "$bundle" 2>/dev/null)"; then
    echo "::error::$bundle carries no verdict, so whether the run measured anything is unknown." >&2
    return 1
  fi

  reasons="$(jq -r '.verdict.reasons // [] | .[]' "$bundle")"
  echo "run verdict: $result"

  # The reasons for an invalid run print beside the failure.
  if [ "$result" = "invalid" ]; then
    [ -z "$reasons" ] || printf '  %s\n' "$reasons" >&2
    echo "::error::this run did not measure the system, so its numbers describe nothing." >&2
    return 1
  fi

  [ -z "$reasons" ] || printf '  %s\n' "$reasons"
  report_refusals "$bundle"
  return 0
}

# report_refusals prints the share of requests the server refused; the server counts per address,
# so unbelieved presented addresses share one allowance. An absent reading prints nothing.
report_refusals() {
  local bundle="$1" asked turned_away share
  # Numbers only: any other value is unreadable and would end the step in the comparison.
  asked="$(jq -r '.refusals.requests | numbers // empty' "$bundle" 2>/dev/null || true)"
  turned_away="$(jq -r '.refusals.refused | numbers // empty' "$bundle" 2>/dev/null || true)"
  if [ -z "$asked" ] || [ -z "$turned_away" ] || [ "$asked" -le 0 ]; then
    return 0
  fi

  share="$(awk -v r="$turned_away" -v a="$asked" 'BEGIN { printf "%.2f", (r * 100) / a }')"
  echo "requests refused at the door: $turned_away of $asked (${share}%)"

  # Reported as a warning; no ceiling for this share has been bracketed.
  if awk -v s="$share" -v worth="$REFUSAL_SHARE_WORTH_SAYING" 'BEGIN { exit !(s >= worth) }'; then
    echo "::warning::${share}% of this run's requests were refused. The server counts requests per address, so check that the generator's presented addresses were believed — an unbelieved address puts the whole run behind one allowance, and what the night then measured is the limiter rather than the server."
  fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
