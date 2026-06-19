#!/usr/bin/env bash
# Offline tests for benchmark-vm-push.sh metric mapping.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PUSH="$REPO_ROOT/scripts/benchmark-vm-push.sh"
[ -x "$PUSH" ] || {
  echo "FAIL: $PUSH not executable" >&2
  exit 1
}

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

cat >"$TMP_ROOT/rows.json" <<'JSON'
[
  {
    "name": "BenchmarkEncodeFrame",
    "lang": "go",
    "ns_op": 123.4,
    "allocs_op": 2,
    "bytes_op": 64,
    "commit": "deadbeef",
    "env": "ci"
  },
  {
    "name": "encode_frame",
    "lang": "rust",
    "ns_op": 987.6,
    "allocs_op": null,
    "bytes_op": null,
    "commit": "deadbeef",
    "env": "ci"
  }
]
JSON

echo "benchmark VM push:"
PATH="$TMP_ROOT/bin:$PATH" \
  KUBECTL_ARGS_FILE="$TMP_ROOT/kubectl.args" \
  KUBECTL_STDIN_FILE="$TMP_ROOT/payload.prom" \
  VM_NAMESPACE="observability" \
  VM_SERVICE="private-vm" \
  "$PUSH" "$TMP_ROOT/rows.json" >/dev/null 2>&1
RC=$?
if [ "$RC" -eq 0 ]; then pass "push exits 0"; else fail "push exited $RC"; fi

if grep -qF 'http://private-vm.observability.svc:8428/api/v1/import/prometheus' "$TMP_ROOT/kubectl.args"; then
  pass "uses configured VM endpoint"
else
  fail "VM endpoint missing from kubectl args"
fi

if grep -qF 'benchmark_ns_op{commit="deadbeef",env="ci",benchmark="BenchmarkEncodeFrame",lang="go"} 123.4' "$TMP_ROOT/payload.prom"; then
  pass "maps Go ns/op"
else
  fail "Go ns/op metric missing"
fi
if grep -qF 'benchmark_allocs_op{commit="deadbeef",env="ci",benchmark="BenchmarkEncodeFrame",lang="go"} 2' "$TMP_ROOT/payload.prom"; then
  pass "maps Go allocs/op"
else
  fail "Go allocs/op metric missing"
fi
if grep -qF 'benchmark_bytes_op{commit="deadbeef",env="ci",benchmark="BenchmarkEncodeFrame",lang="go"} 64' "$TMP_ROOT/payload.prom"; then
  pass "maps Go B/op"
else
  fail "Go B/op metric missing"
fi
if grep -qF 'benchmark_ns_op{commit="deadbeef",env="ci",benchmark="encode_frame",lang="rust"} 987.6' "$TMP_ROOT/payload.prom"; then
  pass "maps Rust criterion ns/op"
else
  fail "Rust ns/op metric missing"
fi
if grep -q 'benchmark_allocs_op.*lang="rust"' "$TMP_ROOT/payload.prom"; then
  fail "Rust allocation metric should be skipped when unavailable"
else
  pass "skips unavailable Rust allocations"
fi

rc=0
"$PUSH" "$TMP_ROOT/missing.json" >/dev/null 2>&1 || rc=$?
if [ "$rc" -eq 2 ]; then pass "missing rows exits 2"; else fail "missing rows expected exit 2, got $rc"; fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
