#!/usr/bin/env bash
# Build the canonical network-drill trend rows from the runner's measurements.
#
# The runner appends one JSON object per measurement as it goes; this collects
# them into the single array the push, the regression check and the evidence
# bundle all read. Nothing is invented here: a scenario that emitted no row for
# a metric has no row for it, and this refuses to fill one in.
set -euo pipefail

MEASUREMENTS_FILE="${1:-${MEASUREMENTS_FILE:-netdrill-measurements.jsonl}}"
COMMIT_SHA="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if [[ ! -f "$MEASUREMENTS_FILE" ]]; then
  echo "missing: $MEASUREMENTS_FILE" >&2
  exit 2
fi

# A run whose scenarios were all inconclusive has an empty file. That is a real
# outcome and it is reported as one — exit 2, no rows — rather than as an array
# of nothing, which downstream would push as a night that measured zero.
if [[ ! -s "$MEASUREMENTS_FILE" ]]; then
  echo "no measurements in $MEASUREMENTS_FILE — every scenario was inconclusive" >&2
  exit 2
fi

rows="$(
  jq -s \
    --arg commit "$COMMIT_SHA" --arg ts "$TIMESTAMP" '
      map(
        select(.metric != null and .value != null)
        | {
            metric: .metric,
            scenario: (.scenario // "unknown"),
            victim: (.victim // "unknown"),
            commit: (.commit // $commit),
            env: (.env // "ci"),
            value: (.value | tonumber),
            timestamp: $ts
          }
      )
      | sort_by(.scenario, .metric, .victim)
    ' "$MEASUREMENTS_FILE"
)" || {
  echo "could not read measurements from $MEASUREMENTS_FILE" >&2
  exit 2
}

if [[ "$(jq -r 'length' <<<"$rows")" -eq 0 ]]; then
  echo "no usable measurements in $MEASUREMENTS_FILE" >&2
  exit 2
fi

printf '%s\n' "$rows"
