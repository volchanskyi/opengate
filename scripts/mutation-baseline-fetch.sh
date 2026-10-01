#!/usr/bin/env bash
# Reconstruct the previous per-language mutation baseline from VictoriaMetrics
# and print it as the one-line canonical HISTORY_FILE row that
# scripts/mutation-summarize.sh reads via previous_row(). VictoriaMetrics holds
# the trend, and a workflow run starts with no history of its own, so this read
# is what makes the summarizer's drop-rule — "score fell more than
# REGRESSION_DROP_PP from the previous run" — fire in CI at all. Without a
# restored baseline previous_row is null and only the absolute floor ever trips,
# leaving a gradual 92→89→86→84.9 slide invisible until the last step crosses it.
#
# For each canonical language it reads the previous night's mutation_score — the
# latest reading of the newest date before tonight's — through the shared
# read-back lib scripts/lib/vm-query.sh (the labels/metric emitted by
# scripts/mutation-vm-push.sh). A night is a date, so the previous night counts
# whatever code it ran, and a re-run of tonight is kept out by its date. The read
# is FAIL-OPEN: any VM / transport / parse failure yields an empty series, so a
# metrics outage degrades to floor-only rather than a false regression or a red
# run.
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
#
# Output: one canonical row {"scores":{"<lang>":{"score_pct":N},...}} on stdout
# for every language that has a prior VM sample; a language absent from VM is
# omitted so the summarizer applies floor-only to it. When no language has any
# history, nothing is printed at all (previous_row stays null ⇒ floor-only).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/vm-query.sh
. "$SCRIPT_DIR/lib/vm-query.sh"

# How far back the previous night is looked for: a week of nights that did not
# run is still a baseline, and a month is not.
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
