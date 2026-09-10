#!/usr/bin/env bash
# The pods a run puts on the shared staging node fit in what that node has left.
#
# Staging and production sit on one worker node, and everything already standing
# on it has reserved a share. What is left is small, and a pod asking for more
# than the remainder is not scheduled at all — the run then waits for a pod that
# was never placed and gives up with a message about a timeout.
#
# The nightly network drill lost a night to it: four pods asking for 185
# millicores against 150 free, the fleet's own 100 arriving last. Nothing
# anywhere added those four numbers up, and every one of them is text.
#
# So they are added up here, per workflow, and held under the remainder. The
# workflows named below each take the staging lease before they create anything,
# so only one of them is ever on the node at a time and each is judged alone.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# What the node has left, in millicores, read off the live cluster on
# 2026-09-10: 1830m allocatable against 1680m already requested by the cluster's
# standing tenants, staging's own server and database among them. Re-read it
# with:
#
#   kubectl describe node <node> | sed -n '/Allocated resources/,/Events/p'
#
# A run may claim the remainder and no more.
NODE_MILLICORES_FREE=150

# Every workflow that creates pods in the staging namespace, and every script
# they create them through. A pod's reservation is written in one or the other.
declare -a WORKFLOWS=(
  "$REPO_ROOT/.github/workflows/network-drill.yml"
  "$REPO_ROOT/.github/workflows/load-test.yml"
  "$REPO_ROOT/.github/workflows/cd.yml"
)

# Pod manifests the workflows above hand to kubectl, and how many pods each
# workflow makes from them. cd.yml stands up two machines from the one manifest,
# and two pods reserve twice.
declare -A MANIFEST_OF=(
  ["deploy/scripts/netfault-shaper-pod.sh"]="network-drill.yml:1"
  ["deploy/scripts/e2e-machine-pod.sh"]="network-drill.yml:1 cd.yml:2"
)

PASS=0
FAIL=0
FAILURES=()
pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}
fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}

# millicores turns a Kubernetes processor quantity into millicores. It reads the
# two shapes a manifest in this repository uses — "250m" and a bare "1" or "0.5".
millicores() {
  local raw="$1"
  if [[ "$raw" == *m ]]; then
    printf '%s\n' "${raw%m}"
    return
  fi
  awk -v v="$raw" 'BEGIN { printf "%d\n", v * 1000 }'
}

# workflow_requests sums the processor a workflow's inline pod overrides reserve.
# Each override is one JSON blob on one line, so the requests block is read out
# of it directly.
workflow_requests() {
  local file="$1" total=0 value
  while IFS= read -r value; do
    total=$((total + $(millicores "$value")))
  done < <(grep -oE '"requests":\{"cpu":"[^"]+"' "$file" | grep -oE '"[0-9.]+m?"$' | tr -d '"')
  printf '%s\n' "$total"
}

# manifest_requests sums the processor a pod manifest reserves. These are YAML,
# where the requested figure is the one under a `requests:` key.
manifest_requests() {
  local file="$1" total=0 value
  while IFS= read -r value; do
    total=$((total + $(millicores "$value")))
  done < <(awk '/requests:/ { inreq = 1; next }
                inreq && /cpu:/ { print $2; inreq = 0; next }
                inreq && /^[[:space:]]*[a-z]+:/ { next }' "$file")
  printf '%s\n' "$total"
}

# The sweep has to reach something. A pattern that stopped matching would
# otherwise report every workflow as reserving nothing at all, and pass.
read_anything=0

for workflow in "${WORKFLOWS[@]}"; do
  name="$(basename "$workflow")"
  [ -f "$workflow" ] || {
    fail "$name is named here and is not in the repository"
    continue
  }

  total="$(workflow_requests "$workflow")"
  for manifest in "${!MANIFEST_OF[@]}"; do
    for use in ${MANIFEST_OF[$manifest]}; do
      [ "${use%%:*}" = "$name" ] || continue
      total=$((total + $(manifest_requests "$REPO_ROOT/$manifest") * ${use##*:}))
    done
  done

  if [ "$total" -le 0 ]; then
    fail "$name reserves nothing at all, which is a sweep that read nothing"
    continue
  fi
  read_anything=$((read_anything + 1))

  if [ "$total" -le "$NODE_MILLICORES_FREE" ]; then
    pass "$name reserves ${total}m of the ${NODE_MILLICORES_FREE}m the node has left"
  else
    fail "$name reserves ${total}m against the ${NODE_MILLICORES_FREE}m the node has left — its last pod will not be scheduled"
  fi
done

if [ "$read_anything" -eq 0 ]; then
  fail "the sweep read no workflow at all"
fi

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '%s\n' "${FAILURES[@]}" >&2
  exit 1
fi
