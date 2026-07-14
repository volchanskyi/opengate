#!/usr/bin/env bash
# Offline tests for mutation/PMAT/terraform-drift VM metric wrappers.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

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

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT
mkdir -p "$TMP_ROOT/bin"

cat >"$TMP_ROOT/bin/kubectl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >"$KUBECTL_ARGS_FILE"
cat >"$KUBECTL_STDIN_FILE"
SH
chmod +x "$TMP_ROOT/bin/kubectl"

run_push() {
  local script="$1"
  local input_file="$2"
  local args_file="$3"
  local payload_file="$4"

  PATH="$TMP_ROOT/bin:$PATH" \
    KUBECTL_ARGS_FILE="$args_file" \
    KUBECTL_STDIN_FILE="$payload_file" \
    VM_NAMESPACE="observability" \
    VM_SERVICE="private-vm" \
    "$REPO_ROOT/scripts/$script" "$input_file" >/dev/null 2>&1
}

echo "CI trend VM push wrappers:"

cat >"$TMP_ROOT/mutation-row.json" <<'JSON'
{
  "timestamp": "2026-06-19T00:00:00Z",
  "commit": "deadbeef",
  "scores": {
    "rust": {
      "killed": 10,
      "survived": 1,
      "timeout": 2,
      "no_coverage": null,
      "unviable": 0,
      "total": 13,
      "score_pct": 92.3
    },
    "go": {
      "killed": 20,
      "survived": 3,
      "timeout": 0,
      "no_coverage": 1,
      "unviable": 0,
      "total": 24,
      "score_pct": 83.3
    }
  }
}
JSON

if run_push "mutation-vm-push.sh" "$TMP_ROOT/mutation-row.json" "$TMP_ROOT/mutation.args" "$TMP_ROOT/mutation.prom"; then
  pass "mutation VM push exits 0"
else
  fail "mutation VM push should exit 0"
fi
if grep -qF 'mutation_score{commit="deadbeef",env="ci",language="go"} 83.3' "$TMP_ROOT/mutation.prom"; then
  pass "mutation score metric maps language label"
else
  fail "mutation score metric missing"
fi
if grep -qF 'mutation_survived{commit="deadbeef",env="ci",language="rust"} 1' "$TMP_ROOT/mutation.prom"; then
  pass "mutation survived metric maps counts"
else
  fail "mutation survived metric missing"
fi
if grep -q 'mutation_no_coverage.*language="rust"' "$TMP_ROOT/mutation.prom"; then
  fail "mutation no-coverage metric should skip null values"
else
  pass "mutation null no-coverage is skipped"
fi

cat >"$TMP_ROOT/mutation-status.json" <<'JSON'
{
  "commit": "deadbeef",
  "run_id": "123",
  "complete": false,
  "shards": {
    "rust-1": { "complete": true, "reason": "ok" },
    "go-api-core": { "complete": false, "reason": "missing" }
  }
}
JSON

if run_push "mutation-status-vm-push.sh" "$TMP_ROOT/mutation-status.json" "$TMP_ROOT/mutation-status.args" "$TMP_ROOT/mutation-status.prom"; then
  pass "mutation status VM push exits 0"
else
  fail "mutation status VM push should exit 0"
fi
if grep -qF 'mutation_run_complete{commit="deadbeef",env="ci"} 0' "$TMP_ROOT/mutation-status.prom"; then
  pass "mutation run completion metric maps incomplete=false to 0"
else
  fail "mutation run completion metric missing"
fi
if grep -qF 'mutation_shard_complete{commit="deadbeef",env="ci",shard="rust-1"} 1' "$TMP_ROOT/mutation-status.prom" \
  && grep -qF 'mutation_shard_complete{commit="deadbeef",env="ci",shard="go-api-core"} 0' "$TMP_ROOT/mutation-status.prom"; then
  pass "mutation shard completion metrics map complete/incomplete values"
else
  fail "mutation shard completion metrics missing"
fi

cat >"$TMP_ROOT/pmat-row.json" <<'JSON'
{
  "timestamp": "2026-06-19T00:00:00Z",
  "commit": "cafebabe",
  "repo_score": 88.5,
  "repo_grade": "B+",
  "below_bplus": 4,
  "categories": {
    "complexity": 91.25,
    "duplication": 100
  }
}
JSON

if run_push "pmat-vm-push.sh" "$TMP_ROOT/pmat-row.json" "$TMP_ROOT/pmat.args" "$TMP_ROOT/pmat.prom"; then
  pass "PMAT VM push exits 0"
else
  fail "PMAT VM push should exit 0"
fi
if grep -qF 'pmat_repo_score{commit="cafebabe",env="ci",grade="B+"} 88.5' "$TMP_ROOT/pmat.prom"; then
  pass "PMAT repo score metric includes grade label"
else
  fail "PMAT repo score metric missing"
fi
if grep -qF 'pmat_below_bplus{commit="cafebabe",env="ci"} 4' "$TMP_ROOT/pmat.prom"; then
  pass "PMAT below-B+ metric maps count"
else
  fail "PMAT below-B+ metric missing"
fi
if grep -qF 'pmat_category_score{commit="cafebabe",env="ci",category="complexity"} 91.25' "$TMP_ROOT/pmat.prom"; then
  pass "PMAT category metric maps category label"
else
  fail "PMAT category metric missing"
fi

cat >"$TMP_ROOT/drift-row.json" <<'JSON'
{
  "timestamp": "2026-06-19T00:00:00Z",
  "run_id": "999",
  "commit": "abc123",
  "drift_count": 3,
  "resource_changes": [
    { "address": "module.networking.oci_core_security_list.opengate", "actions": ["update"], "type": "oci_core_security_list" },
    { "address": "module.compute.oci_core_instance.opengate", "actions": ["delete", "create"], "type": "oci_core_instance" },
    { "address": "module.storage.oci_core_volume.opengate", "actions": ["delete"], "type": "oci_core_volume" }
  ],
  "summary": "3 resources drifted"
}
JSON

if run_push "terraform-drift-vm-push.sh" "$TMP_ROOT/drift-row.json" "$TMP_ROOT/drift.args" "$TMP_ROOT/drift.prom"; then
  pass "terraform drift VM push exits 0"
else
  fail "terraform drift VM push should exit 0"
fi
if grep -qF 'terraform_drift_count{commit="abc123",env="ci",run_id="999"} 3' "$TMP_ROOT/drift.prom"; then
  pass "terraform drift count metric maps run"
else
  fail "terraform drift count metric missing"
fi
if grep -qF 'terraform_drift_resources{commit="abc123",env="ci",run_id="999",action="delete",type="oci_core_instance"} 1' "$TMP_ROOT/drift.prom"; then
  pass "terraform drift resource metric maps action/type"
else
  fail "terraform drift resource metric missing"
fi

for wrapper in mutation-vm-push.sh mutation-status-vm-push.sh pmat-vm-push.sh terraform-drift-vm-push.sh; do
  rc=0
  "$REPO_ROOT/scripts/$wrapper" "$TMP_ROOT/missing.json" >/dev/null 2>&1 || rc=$?
  if [ "$rc" -eq 2 ]; then
    pass "$wrapper missing input exits 2"
  else
    fail "$wrapper missing input expected exit 2, got $rc"
  fi
done

if grep -qF 'http://private-vm.observability.svc:8428/api/v1/import/prometheus' "$TMP_ROOT/drift.args"; then
  pass "wrappers use configured VM endpoint"
else
  fail "configured VM endpoint missing"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
