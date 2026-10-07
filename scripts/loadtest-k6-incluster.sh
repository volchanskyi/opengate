#!/usr/bin/env bash
# Runs one k6 invocation next to the server in the staging cluster and returns its summary export.
# It stands in for K6_BIN in loadtest-k6-run.sh, which keeps the rule that discards empty runs.
#
# Environment:
#   LOADTEST_K6_POD  pod holding /tmp/k6 and the staged load/ tree (required)
#   NAMESPACE        namespace the pod runs in (default opengate-staging)
#   LOADTEST_K6_BIN  k6 path inside the pod (default /tmp/k6)
#
# Usage:
#   loadtest-k6-incluster.sh run [k6 args...]
set -euo pipefail

# The helper marks which cluster calls may be repeated after a dropped connection.
# shellcheck source=scripts/lib/kubectl-retry.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/kubectl-retry.sh"

main() {
  local pod="${LOADTEST_K6_POD:-}"
  local namespace="${NAMESPACE:-opengate-staging}"
  local pod_k6="${LOADTEST_K6_BIN:-/tmp/k6}"

  if [ -z "$pod" ]; then
    echo "loadtest-k6-incluster: LOADTEST_K6_POD must name the staged k6 pod" >&2
    return 2
  fi

  # The export path is the one value that differs by side: k6 writes it in the pod and the
  # runner reads it here; every other argument passes through untouched.
  local export_path="" prev=""
  local arg
  for arg in "$@"; do
    [ "$prev" = "--summary-export" ] && export_path="$arg"
    prev="$arg"
  done

  local status=0
  # Not retried: the exec carries the workload, and a dropped connection leaves the pod's process
  # running, so a second attempt would add a second generator to the same server.
  kubectl -n "$namespace" exec "$pod" -- "$pod_k6" "$@" || status=$?

  # Copied whatever the run produced, including after an abort, so a partial export is kept.
  if [ -n "$export_path" ]; then
    collect_export "$namespace" "$pod" "$export_path"
  fi

  return "$status"
}

# collect_export copies one summary export out of the pod and reports whether the copy failed or
# k6 wrote nothing; the runner refuses an absent export, so a failed copy is not fatal here.
collect_export() {
  local namespace="$1" pod="$2" export_path="$3" refusal=""

  # Retried, since the run is over and a dropped connection here would discard finished work.
  if refusal="$(kubectl_retry -n "$namespace" cp "$pod:$export_path" "$export_path" 2>&1)"; then
    return 0
  fi

  if kubectl_retry -n "$namespace" exec "$pod" -- test -f "$export_path" >/dev/null 2>&1; then
    echo "::warning::$export_path is inside $pod and could not be copied out: $refusal" >&2
  else
    echo "::warning::k6 wrote no summary export at $export_path inside $pod" >&2
  fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
