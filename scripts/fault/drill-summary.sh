#!/usr/bin/env bash
# Writes a staging fault drill's job summary, as Markdown: what the drill does, its result, the
# recovery time the drill recorded beside its limit, and whether a fault was left on the edge.
#
# Usage: drill-summary.sh SCENARIO EVIDENCE_DIR DRILL_OUTCOME EDGE_OUTCOME
#   SCENARIO       pod-delete, bad-rollout, ingress-504 or ingress-502
#   EVIDENCE_DIR   where the drill wrote SCENARIO-recovery.txt as "<seconds> <limit>"
#   DRILL_OUTCOME  the drill step's outcome: success, failure, cancelled or skipped
#   EDGE_OUTCOME   the leftover-fault check's outcome, in the same words
#
# Exit codes:
#   0  summary written
#   2  usage
set -euo pipefail

[ "$#" -eq 4 ] || {
  sed -n '5,9p' "${BASH_SOURCE[0]}" | sed 's/^# //' >&2
  exit 2
}
SCENARIO="$1"
EVIDENCE_DIR="$2"
DRILL="$3"
EDGE="$4"

case "$SCENARIO" in
  pod-delete) PURPOSE='Deletes the staging server pod; a Ready replacement has to come up inside the limit.' ;;
  bad-rollout) PURPOSE='Deploys a revision that cannot start; readiness has to refuse it and a rollback restore the release inside the limit.' ;;
  ingress-504) PURPOSE="Cuts the staging edge's proxy timeout to 5 s, then restores it." ;;
  ingress-502) PURPOSE='Takes the staging server away from behind the edge, then restores it.' ;;
  *) PURPOSE='' ;;
esac

secs=""
limit=""
if [ -r "$EVIDENCE_DIR/$SCENARIO-recovery.txt" ]; then
  read -r secs limit <"$EVIDENCE_DIR/$SCENARIO-recovery.txt" || true
fi
recovery="not measured"
[ -n "$secs" ] && [ -n "$limit" ] && recovery="$secs s (limit $limit s)"

# word OUTCOME prints how the summary reads a step outcome.
word() {
  case "$1" in
    success) echo pass ;;
    failure) echo FAIL ;;
    cancelled) echo cancelled ;;
    *) echo "not run" ;;
  esac
}

edge="not checked"
case "$EDGE" in
  success) edge="none" ;;
  failure) edge="FOUND" ;;
esac

if [ "$DRILL" = cancelled ] || [ "$EDGE" = cancelled ]; then
  result="cancelled."
elif [ "$EDGE" = failure ]; then
  result="FAIL: a fault was left on the staging edge."
elif [ "$DRILL" = failure ] && [ -z "$secs" ]; then
  result="FAIL: the drill stopped before the server came back."
elif [ "$DRILL" = failure ] && [ "$secs" -gt "$limit" ]; then
  result="FAIL: the server took $secs s, past its $limit s limit."
elif [ "$DRILL" = failure ]; then
  result="FAIL: the drill failed after the server came back in $secs s."
elif [ "$DRILL" = success ] && [ -n "$secs" ]; then
  result="pass: the server came back in $secs s, inside its $limit s limit."
elif [ "$DRILL" = success ]; then
  result="pass: the drill ran and left the edge clean."
else
  result="the drill did not run."
fi

printf '## Staging fault drill: %s\n\n' "$SCENARIO"
[ -z "$PURPOSE" ] || printf '%s\n\n' "$PURPOSE"
printf '**Result:** %s\n\n' "$result"
printf '| Drill | Result | Recovery | Fault left on the edge |\n|---|---|---|---|\n'
printf '| %s | %s | %s | %s |\n' "$SCENARIO" "$(word "$DRILL")" "$recovery" "$edge"
