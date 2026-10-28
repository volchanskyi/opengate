#!/usr/bin/env bash
# Static policy assertions over fault-tolerance.yml and cd.yml; no runner needed.
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
assert_re() {
  local name="$1" file="$2" re="$3"
  if grep -Eq -- "$re" "$file"; then pass "$name"; else fail "$name (no match /$re/ in ${file##*/})"; fi
}
assert_no_re() {
  local name="$1" file="$2" re="$3"
  if grep -Eiq -- "$re" "$file"; then fail "$name (unexpected /$re/ in ${file##*/})"; else pass "$name"; fi
}
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

# The runtime guard is the sole allow-list, so each scenario appears there and as a dispatch branch.
for s in pod-delete bad-rollout ingress-504 ingress-502; do
  assert_fixed "scenario '$s' is on the runtime allow-list" "$WF" "$s"
  assert_re "scenario '$s' has a dispatch branch" "$WF" "^[[:space:]]*$s\)"
done

# workflow_call passes scenario as a free string, so a runtime case-guard rejects the rest.
assert_fixed "a runtime guard validates the scenario against an allow-list" "$WF" \
  'pod-delete | bad-rollout | ingress-504 | ingress-502)'

assert_re "fault-tolerance is a reusable workflow (workflow_call)" "$WF" \
  '^[[:space:]]*workflow_call:'
# Only the deploy pipeline starts a staging drill, so every run is attached to the deploy it gates.
assert_no_re "fault-tolerance has no manual dispatch entry point" "$WF" \
  '^[[:space:]]*workflow_dispatch:'

assert_re "concurrency guard present" "$WF" '^concurrency:'

assert_re "hard job timeout present" "$WF" 'timeout-minutes:'

assert_re "a cleanup/restore step runs under always()" "$WF" 'if:[[:space:]]*always\(\)'
assert_re "cleanup restores ingress faults" "$WF" 'ingress-restore\.sh'
assert_re "evidence is uploaded as an artifact" "$WF" 'upload-artifact'

assert_re "drill targets the opengate-staging namespace" "$WF" 'opengate-staging'
assert_re "drill reaches the cluster via the OCI/kubeconfig action" "$WF" \
  'oci-kube-setup'

assert_no_re "no chaos-mesh scenario is wired into the workflow" "$WF" 'chaos.?mesh'
assert_no_re "no NetworkChaos/StressChaos scenario is wired in" "$WF" \
  'networkchaos|stresschaos'
assert_no_re "no 'network' fault profile is an enumerated option" "$WF" \
  '-[[:space:]]*network[[:space:]]*$'

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

# Evaluates a job's condition the way GitHub does, prepending success() over every job upstream
# when the condition states no status of its own. Prints one ok or FAIL line per case.
gate_cases="$(
  python3 - "$CD" <<'PY'
import re
import sys

import yaml

jobs = yaml.safe_load(open(sys.argv[1], encoding="utf-8"))["jobs"]
STATUS = re.compile(r"\b(always|success|failure|cancelled)\(\)")


def upstream(job, seen=None):
    seen = set() if seen is None else seen
    needs = jobs[job].get("needs") or []
    for parent in [needs] if isinstance(needs, str) else needs:
        if parent not in seen:
            seen.add(parent)
            upstream(parent, seen)
    return seen


def runs(job, ctx):
    expression = str(jobs[job].get("if", "") or "true").strip()
    expression = re.sub(r"^\$\{\{(.*)\}\}$", r"\1", expression, flags=re.S)
    implicit = not STATUS.search(expression)
    expression = re.sub(r"\b(needs\.[\w-]+\.(?:result|outputs\.[\w-]+)|vars\.\w+)",
                        lambda m: repr(ctx.get(m.group(1), "")), expression)
    expression = expression.replace("&&", " and ").replace("||", " or ")
    expression = re.sub(r"!(?!=)", " not ", expression)
    expression = expression.replace("cancelled()", repr(ctx.get("cancelled", False)))
    expression = expression.replace("always()", "True").replace("true", "True")
    verdict = eval(expression, {"__builtins__": {}})
    if implicit:
        verdict = verdict and not ctx.get("cancelled", False) and all(
            ctx.get(f"needs.{parent}.result", "success") == "success" for parent in upstream(job))
    return bool(verdict)


staged = {"needs.resolve-tag.result": "success", "needs.resolve-tag.outputs.should_skip_staging": "false",
          "needs.cluster.result": "skipped", "needs.deploy-staging-k8s.result": "success"}
on = dict(staged, **{"vars.STAGING_FAULT_TESTS": "true"})
cases = [
    ("staging-fault-drills", on, True, "the drills run once staging deployed, with no cluster change"),
    ("staging-fault-drills", dict(on, **{"needs.deploy-staging-k8s.result": "failure"}), False,
     "the drills wait for a staging that deployed"),
    ("staging-fault-drills", dict(on, cancelled=True), False, "a cancel stops the drills"),
    ("staging-fault-drills", staged, False, "the drills stay off unless STAGING_FAULT_TESTS is true"),
    ("deploy-production-k8s", dict(on, **{"needs.staging-fault-drills.result": "success"}), True,
     "production promotes after drills that passed"),
    ("deploy-production-k8s", dict(on, **{"needs.staging-fault-drills.result": "skipped"}), False,
     "production refuses drills skipped while they are switched on"),
    ("deploy-production-k8s", dict(staged, **{"needs.staging-fault-drills.result": "skipped"}), True,
     "production promotes past drills that are switched off"),
    ("deploy-production-k8s", dict(on, cancelled=True, **{"needs.staging-fault-drills.result": "success"}),
     False, "a cancel stops the production deploy"),
    ("deploy-staging-k8s", dict(staged, cancelled=True), False, "a cancel stops the staging deploy"),
]
for job, ctx, expected, name in cases:
    print(("ok" if runs(job, ctx) == expected else "FAIL") + "\t" + name)
PY
)"
while IFS=$'\t' read -r verdict name; do
  if [ "$verdict" = ok ]; then pass "$name"; else fail "$name"; fi
done <<<"$gate_cases"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
