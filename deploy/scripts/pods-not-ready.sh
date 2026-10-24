#!/usr/bin/env bash
# Names each pod in a namespace with a container short of ready, the reason the cluster gives, and
# the last lines that container wrote; a deploy calls it when its wait for a release runs out.
#
# Usage: deploy/scripts/pods-not-ready.sh <namespace>
#
# Exit codes:
#   0  every pod short of ready was named, or every pod is ready
#   1  the cluster gave no answer about the namespace's pods
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/kubectl-retry.sh
. "$ROOT/scripts/lib/kubectl-retry.sh"

NAMESPACE="${1:?a namespace is required}"
LINES=20

if ! pods="$(kubectl_retry -n "$NAMESPACE" get pods -o json)"; then
  echo "::error::pods-not-ready: the pods of $NAMESPACE could not be read" >&2
  exit 1
fi

# One line per container short of ready, or per pod none of whose containers started: the pod, the
# container, its restarts, and the reason. A finished init container counts as done.
# shellcheck disable=SC2016 # the filter's $names belong to jq
unready="$(jq -r '
  def ended: "\(.reason), exit \(.exitCode)";
  .items[] | .metadata.name as $pod
  | ((.status.initContainerStatuses // []) + (.status.containerStatuses // [])) as $containers
  | if ($containers | length) == 0 then
      select(.status.phase != "Succeeded")
      | [$pod, "-", "0", "\(.status.phase): " + ([.status.conditions[]? | select(.status == "False")
          | .message // .reason] | join("; "))]
    else
      $containers[] | select((.ready | not) and .state.terminated.reason != "Completed")
      | [$pod, .name, (.restartCount | tostring),
         ([if .state.waiting then .state.waiting.reason
             + (if .state.waiting.message then " (\(.state.waiting.message))" else "" end)
           elif .state.terminated then "ended \(.state.terminated | ended)"
           else "running, not ready" end,
           (.lastState.terminated // empty | "last ended \(ended)")] | join("; "))]
    end
  | @tsv' <<<"$pods")"

if [ -z "$unready" ]; then
  echo "pods-not-ready: every pod in $NAMESPACE is ready"
  exit 0
fi

while IFS=$'\t' read -r pod container restarts reason; do
  if [ "$container" = "-" ]; then
    echo "::error::pods-not-ready: $NAMESPACE/$pod: $reason"
    continue
  fi
  echo "::error::pods-not-ready: $NAMESPACE/$pod $container: $reason"
  # A container that has restarted is between runs, and the run before wrote why it stopped.
  previous=()
  [ "$restarts" -eq 0 ] || previous=(--previous)
  echo "The last $LINES lines $pod/$container wrote:"
  kubectl_retry -n "$NAMESPACE" logs "$pod" -c "$container" "${previous[@]}" --tail="$LINES" </dev/null \
    || echo "  the cluster gave no log for $pod/$container"
done <<<"$unready"
