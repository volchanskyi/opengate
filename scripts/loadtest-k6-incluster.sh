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
    collect_export "$namespace" "$pod" "$export_path"
  fi

  return "$status"
}

# collect_export brings one summary export out of the pod, and when it does not
# arrive, says which of the two things happened.
#
# They are opposite findings. A k6 that wrote nothing is a scenario to look at;
# a copy that failed is a measurement that exists, in a pod the next step
# deletes, and the night is short a scenario for a reason that has nothing to do
# with the load. The copy's own account of itself is the only thing that tells
# them apart, and it was being sent to /dev/null under a message asserting the
# first — an absence announced by something that never asked. Run 34443201348
# reported that k6 wrote no export after k6 had run 7,256 requests with every
# threshold green.
#
# A copy that failed is reported and not fatal: the keep/discard rule stays in
# scripts/loadtest-k6-run.sh, which sees an absent export and refuses there, so
# the scenario's own status travels back unchanged.
collect_export() {
  local namespace="$1" pod="$2" export_path="$3" refusal=""

  if refusal="$(kubectl -n "$namespace" cp "$pod:$export_path" "$export_path" 2>&1)"; then
    return 0
  fi

  if kubectl -n "$namespace" exec "$pod" -- test -f "$export_path" >/dev/null 2>&1; then
    echo "::warning::$export_path is inside $pod and could not be copied out: $refusal" >&2
  else
    echo "::warning::k6 wrote no summary export at $export_path inside $pod" >&2
  fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
