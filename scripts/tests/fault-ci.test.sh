#!/usr/bin/env bash
# Policy tests for the FI6 fault-tolerance CI wiring:
#   .github/workflows/fault-tolerance.yml  (reusable + workflow_dispatch drill)
#   .github/workflows/cd.yml               (staging fault-drill gate)
#
# These are static-policy assertions over the workflow YAML — no runner needed.
# They pin the safety invariants FI6 must hold regardless of how the YAML is
# later reformatted: enumerated (never free-form) scenario inputs, a runtime
# allow-list guard that also covers the workflow_call path, a concurrency guard,
# an always() cleanup, evidence upload, the STAGING_FAULT_TESTS activation gate,
# a production gate on the drill result, and — the load-bearing negative — that
# the deferred Chaos Mesh / network path is never an enumerated gating scenario.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WF="$REPO_ROOT/.github/workflows/fault-tolerance.yml"
CD="$REPO_ROOT/.github/workflows/cd.yml"

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
assert_file() {
  local name="$1" path="$2"
  if [ -f "$path" ]; then pass "$name"; else fail "$name (missing [$path])"; fi
}
# assert_re <name> <file> <extended-regex>
assert_re() {
  local name="$1" file="$2" re="$3"
  if grep -Eq -- "$re" "$file"; then pass "$name"; else fail "$name (no match /$re/ in ${file##*/})"; fi
}
assert_no_re() {
  local name="$1" file="$2" re="$3"
  if grep -Eiq -- "$re" "$file"; then fail "$name (unexpected /$re/ in ${file##*/})"; else pass "$name"; fi
}
# assert_fixed <name> <file> <literal-string>
assert_fixed() {
  local name="$1" file="$2" lit="$3"
  if grep -Fq -- "$lit" "$file"; then pass "$name"; else fail "$name (no literal [$lit] in ${file##*/})"; fi
}

echo "FI6 fault-tolerance CI policy:"

assert_file "fault-tolerance.yml exists" "$WF"
assert_file "cd.yml exists" "$CD"
[ -f "$WF" ] || {
  echo
  echo "Summary: $PASS passed, $FAIL failed"
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
}

# --- Enumerated inputs, never free-form -------------------------------------
assert_re "workflow_dispatch scenario input is an enumerated choice" "$WF" \
  'type:[[:space:]]*choice'
assert_re "dispatch scenario exposes an options list" "$WF" '^[[:space:]]*options:'
# The four self-contained, reversible gating scenarios.
for s in pod-delete bad-rollout ingress-504 ingress-502; do
  assert_re "scenario '$s' is an enumerated option" "$WF" "-[[:space:]]*$s"
done

# --- Runtime allow-list guard (covers the workflow_call string path) ---------
# workflow_call passes scenario as a free string from a repo variable, so a
# runtime case-guard must reject anything outside the allow-list.
assert_fixed "a runtime guard validates the scenario against an allow-list" "$WF" \
  'pod-delete | bad-rollout | ingress-504 | ingress-502)'

# --- Reusable entry callable after staging E2E ------------------------------
assert_re "fault-tolerance is a reusable workflow (workflow_call)" "$WF" \
  '^[[:space:]]*workflow_call:'
assert_re "fault-tolerance is manually dispatchable" "$WF" \
  '^[[:space:]]*workflow_dispatch:'

# --- Concurrency guard (no two overlapping staging fault runs) --------------
assert_re "concurrency guard present" "$WF" '^concurrency:'

# --- Hard timeout longer than the longest bounded scenario ------------------
assert_re "hard job timeout present" "$WF" 'timeout-minutes:'

# --- always() cleanup + evidence upload -------------------------------------
assert_re "a cleanup/restore step runs under always()" "$WF" 'if:[[:space:]]*always\(\)'
assert_re "cleanup restores ingress faults" "$WF" 'ingress-restore\.sh'
assert_re "evidence is uploaded as an artifact" "$WF" 'upload-artifact'

# --- Staging-only namespace + OCI reach (Model: cluster-internal, not public) -
assert_re "drill targets the opengate-staging namespace" "$WF" 'opengate-staging'
assert_re "drill reaches the cluster via the OCI/kubeconfig action" "$WF" \
  'oci-kube-setup'

# --- The deferred Chaos Mesh / network (D1) path is NEVER a gating scenario ---
assert_no_re "no chaos-mesh scenario is wired into the workflow" "$WF" 'chaos.?mesh'
assert_no_re "no NetworkChaos/StressChaos scenario is wired in" "$WF" \
  'networkchaos|stresschaos'
assert_no_re "no 'network' fault profile is an enumerated option" "$WF" \
  '-[[:space:]]*network[[:space:]]*$'

# --- cd.yml: activation gate + production gate ------------------------------
assert_re "cd.yml invokes the reusable fault-tolerance workflow" "$CD" \
  'uses:[[:space:]]*\./\.github/workflows/fault-tolerance\.yml'
assert_re "the staging fault drill is gated by STAGING_FAULT_TESTS" "$CD" \
  'STAGING_FAULT_TESTS'
assert_re "cd.yml selects the drill subset via STAGING_FAULT_PROFILE" "$CD" \
  'STAGING_FAULT_PROFILE'
assert_re "production deploy needs the fault-drill job" "$CD" \
  'staging-fault-drills'
assert_re "production is gated on the fault-drill result" "$CD" \
  'needs\.staging-fault-drills\.result'

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
