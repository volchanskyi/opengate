#!/usr/bin/env bash
# Reconstruct the previous per-language mutation baseline from VictoriaMetrics
# and print it as the one-line canonical HISTORY_FILE row that
# scripts/mutation-summarize.sh reads via previous_row(). This is what makes the
# summarizer's drop-rule — "score fell more than REGRESSION_DROP_PP from the
# previous run" — actually fire in CI: the in-repo docs/mutation-history.jsonl
# was retired, so without a restored baseline previous_row is always null and
# only the absolute floor ever trips (a gradual 92→89→86→84.9 slide stays
# invisible until the last step crosses the floor).
#
# For each canonical language it reads the newest prior mutation_score sample
# through the shared read-back lib scripts/lib/vm-query.sh (the labels/metric
# emitted by scripts/mutation-vm-push.sh). The read is FAIL-OPEN: any VM /
# transport / parse failure yields an empty series, so a metrics outage degrades
# to floor-only (today's behavior) rather than a false regression or a red run.
# Set VM_EXCLUDE_COMMIT to the current commit so a workflow re-run never compares
# against its own just-pushed sample.
#
# Output: one canonical row {"scores":{"<lang>":{"score_pct":N},...}} on stdout
# for every language that has a prior VM sample; a language absent from VM is
# omitted so the summarizer applies floor-only to it. When no language has any
# history, nothing is printed at all (previous_row stays null ⇒ floor-only).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/vm-query.sh
. "$SCRIPT_DIR/lib/vm-query.sh"

scores="{}"
for lang in rust go web; do
  score="$(vm_query_latest mutation_score "language=\"$lang\",env=\"ci\"")"
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
