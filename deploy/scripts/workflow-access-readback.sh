#!/usr/bin/env bash
# Asks the cluster, as the deploy's own login, whether it may read the master key's secret or list
# the secrets of its namespace, and fails unless both answers are a plain no.
#
# Usage: deploy/scripts/workflow-access-readback.sh
#
# Environment:
#   NAMESPACE  the deploy's namespace, default opengate
#
# Exit codes:
#   0  both answers are no
#   1  either answer is yes, or the cluster gave no answer
set -euo pipefail

NAMESPACE="${NAMESPACE:-opengate}"
failed=0

# ask WHAT ARGS... puts a can-i question and holds the answer to a plain no.
ask() {
  local what="$1" answer
  shift
  answer="$(kubectl auth can-i "$@" 2>/dev/null || true)"
  case "$answer" in
    no) echo "workflow-access-readback: no, $what" ;;
    yes)
      echo "workflow-access-readback: this login may $what" >&2
      failed=1
      ;;
    *)
      echo "workflow-access-readback: the cluster gave no answer to whether this login may $what" >&2
      failed=1
      ;;
  esac
}

ask "read the master key's secret" get secret/opengate-authority -n opengate
ask "list every secret in $NAMESPACE" list secrets -n "$NAMESPACE"
exit "$failed"
