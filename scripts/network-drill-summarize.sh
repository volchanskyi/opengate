#!/usr/bin/env bash
# Collects the runner's one-object-per-measurement lines into the network-drill row array.
set -euo pipefail

MEASUREMENTS_FILE="${1:-${MEASUREMENTS_FILE:-netdrill-measurements.jsonl}}"
COMMIT_SHA="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if [[ ! -f "$MEASUREMENTS_FILE" ]]; then
  echo "missing: $MEASUREMENTS_FILE" >&2
  exit 2
fi

# An empty file means every scenario was inconclusive; exit 2 with no rows keeps downstream
# from pushing a night that measured zero.
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
