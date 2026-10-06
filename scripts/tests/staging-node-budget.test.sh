#!/usr/bin/env bash
# Each workflow's pod CPU requests, summed, fit in what the shared staging node has left.
# The workflows hold the staging lease, so one at a time is on the node and each is judged alone.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Millicores of CPU the node has left after its standing tenants' requests.
NODE_MILLICORES_FREE=150

declare -a WORKFLOWS=(
  "$REPO_ROOT/.github/workflows/network-drill.yml"
  "$REPO_ROOT/.github/workflows/load-test.yml"
  "$REPO_ROOT/.github/workflows/cd.yml"
)

# Pod manifests per workflow, as name:pod-count; cd.yml makes two pods from one manifest.
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

millicores() {
  local raw="$1"
  if [[ "$raw" == *m ]]; then
    printf '%s\n' "${raw%m}"
    return
  fi
  awk -v v="$raw" 'BEGIN { printf "%d\n", v * 1000 }'
}

workflow_requests() {
  local file="$1" total=0 value
  while IFS= read -r value; do
    total=$((total + $(millicores "$value")))
  done < <(grep -oE '"requests":\{"cpu":"[^"]+"' "$file" | grep -oE '"[0-9.]+m?"$' | tr -d '"')
  printf '%s\n' "$total"
}

manifest_requests() {
  local file="$1" total=0 value
  while IFS= read -r value; do
    total=$((total + $(millicores "$value")))
  done < <(awk '/requests:/ { inreq = 1; next }
                inreq && /cpu:/ { print $2; inreq = 0; next }
                inreq && /^[[:space:]]*[a-z]+:/ { next }' "$file")
  printf '%s\n' "$total"
}

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
