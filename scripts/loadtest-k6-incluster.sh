#!/usr/bin/env bash
# Run one k6 invocation inside the staging cluster and bring its summary export
# back to the runner.
#
# This is a drop-in for `K6_BIN` in scripts/loadtest-k6-run.sh: it takes the k6
# argument list unchanged, runs it next to the server, and leaves the export at
# the same path the caller asked for. The keep/discard rule stays in the runner,
# which remains the single place that decides whether a scenario measured
# anything.
#
# Why k6 runs in the cluster: the generator then reaches the server over one
# network hop, with no intermediary to saturate, stall, or add its own latency to
# every number the trend keeps. What the run measures is the server.
#
# Environment:
#   LOADTEST_K6_POD  pod holding /tmp/k6 and the staged load/ tree (required)
#   NAMESPACE        namespace the pod runs in (default opengate-staging)
#   LOADTEST_K6_BIN  k6 path inside the pod (default /tmp/k6)
#
# Usage: loadtest-k6-incluster.sh run [k6 args...]
set -euo pipefail

main() {
  local pod="${LOADTEST_K6_POD:-}"
  local namespace="${NAMESPACE:-opengate-staging}"
  local pod_k6="${LOADTEST_K6_BIN:-/tmp/k6}"

  if [ -z "$pod" ]; then
    echo "loadtest-k6-incluster: LOADTEST_K6_POD must name the staged k6 pod" >&2
    return 2
  fi

  # The export path is the one value that means different things on the two
  # sides: k6 writes it inside the pod, the runner reads it here. Everything
  # else — the scenario path, --env, the trend stats — is already pod-side or
  # side-independent, so it passes through untouched.
  local export_path="" prev=""
  local arg
  for arg in "$@"; do
    [ "$prev" = "--summary-export" ] && export_path="$arg"
    prev="$arg"
  done

  local status=0
  kubectl -n "$namespace" exec "$pod" -- "$pod_k6" "$@" || status=$?

  # Copied whatever the run produced, including after an abort: a partial export
  # is evidence the runner needs in order to discard it deliberately rather than
  # mistake a crashed scenario for one that never wrote a file.
  if [ -n "$export_path" ]; then
    kubectl -n "$namespace" cp "$pod:$export_path" "$export_path" 2>/dev/null \
      || echo "::warning::k6 wrote no summary export at $export_path inside $pod" >&2
  fi

  return "$status"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
