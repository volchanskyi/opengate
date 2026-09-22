#!/usr/bin/env bash
# Read back what the run wrote about itself, and fail the step when it says the
# system was never measured.
#
# The harness classifies every run it finishes — valid, failed, or invalid — and
# puts the answer in the evidence bundle. Valid and failed are both
# measurements: one of a system that held, one of a system that did not. Invalid
# is the third, and it is the one that goes missing: the run did not measure the
# system at all.
#
# Nothing read it. The volume family passed a run whose bundle said "invalid" and
# whose fleet was 0 of 500 machines, so the sweep read as partly working when
# none of it was. This is the same shape as a cache save that warns and exits
# zero: the work was refused, the step is green, and the only way anyone finds
# out is by going to look.
#
# An absent or unreadable bundle fails here as loudly as an invalid one. The step
# runs on every path, including the one where the harness died before writing
# anything, and a guard that answers yes when it cannot ask is the false green it
# was written to close.
#
# Usage: perf-bundle-verdict.sh <bundle.json>
set -euo pipefail

usage() {
  echo "usage: $0 <bundle.json>" >&2
}

# The share of refused requests past which the night is worth a second look. A
# healthy run refuses nothing: every technician presents an address of its own
# and offers a few requests a second against an allowance of a hundred. One in
# a hundred is therefore already a chain that has partly broken rather than a
# busy moment.
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

  # A run that measured nothing says why it did not, and that account belongs
  # beside the failure rather than in the output above it.
  if [ "$result" = "invalid" ]; then
    [ -z "$reasons" ] || printf '  %s\n' "$reasons" >&2
    echo "::error::this run did not measure the system, so its numbers describe nothing." >&2
    return 1
  fi

  [ -z "$reasons" ] || printf '  %s\n' "$reasons"
  report_refusals "$bundle"
  return 0
}

# What share of the run's requests the server would not serve.
#
# The server counts requests per address, so a run whose presented addresses were
# not believed spends one allowance between every virtual user: it fills with
# refusals, reds the error-rate gate, and produces a night shaped exactly like
# one against a slow server. A reading that only lives in an artifact somebody
# has to know to open is not what separates the two, so it is printed here,
# beside the verdict, where the step's own log carries it.
#
# Absent is a run that took no such reading — every venue without a browser-side
# generator — and it is silence rather than a nought, for the reason the bundle
# keeps the field absent in the first place.
report_refusals() {
  local bundle="$1" asked turned_away share
  # Numbers only. Anything else is a bundle this reader cannot speak about, and
  # a comparison against it would end the step rather than the sentence.
  asked="$(jq -r '.refusals.requests | numbers // empty' "$bundle" 2>/dev/null || true)"
  turned_away="$(jq -r '.refusals.refused | numbers // empty' "$bundle" 2>/dev/null || true)"
  if [ -z "$asked" ] || [ -z "$turned_away" ] || [ "$asked" -le 0 ]; then
    return 0
  fi

  share="$(awk -v r="$turned_away" -v a="$asked" 'BEGIN { printf "%.2f", (r * 100) / a }')"
  echo "requests refused at the door: $turned_away of $asked (${share}%)"

  # Said rather than gated. No night of this reading has been taken yet, so a
  # ceiling here would be a number nobody has bracketed — and the run still
  # reported, which is what the verdict above is about. What the note buys is
  # the reader looking at the right thing: past this share the night is far more
  # likely to be measuring the limiter than the server.
  if awk -v s="$share" -v worth="$REFUSAL_SHARE_WORTH_SAYING" 'BEGIN { exit !(s >= worth) }'; then
    echo "::warning::${share}% of this run's requests were refused. The server counts requests per address, so check that the generator's presented addresses were believed — an unbelieved address puts the whole run behind one allowance, and what the night then measured is the limiter rather than the server."
  fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
