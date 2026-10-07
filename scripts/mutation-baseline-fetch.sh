#!/usr/bin/env bash
# Prints the previous night's mutation_score per language as the HISTORY_FILE row that
# scripts/mutation-summarize.sh reads; a failed read yields nothing, so only its floor applies.
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/vm-query.sh
. "$SCRIPT_DIR/lib/vm-query.sh"

LOOKBACK_DATES=7

vm_tonight >/dev/null || exit 2

scores="{}"
for lang in rust go web; do
  score="$(vm_nightly_window mutation_score "language=\"$lang\",env=\"ci\"" "$LOOKBACK_DATES" \
    | awk -F'\t' 'NR == 1 { print $4 }')"
  [ -n "$score" ] || continue
  scores="$(jq -c --arg l "$lang" --argjson v "$score" \
    '. + {($l): {score_pct: $v}}' <<<"$scores")"
done

# No language had a prior sample: emit nothing so mutation-summarize.sh's
# previous_row stays null and the run degrades to floor-only.
if [ "$scores" = "{}" ]; then
  exit 0
fi

jq -cn --argjson scores "$scores" '{scores: $scores}'
