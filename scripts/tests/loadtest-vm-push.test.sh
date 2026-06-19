#!/usr/bin/env bash
# Offline tests for loadtest-vm-push.sh metric mapping.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PUSH="$REPO_ROOT/scripts/loadtest-vm-push.sh"
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

cat >"$TMP_ROOT/loadtest-summary.json" <<'JSON'
[
  {
    "source": "k6",
    "scenario": "api-baseline",
    "phase": "http",
    "latency_p50_ms": 50.5,
    "latency_p95_ms": 123.4,
    "latency_p99_ms": 222.2,
    "rps": 42.5,
    "error_rate": 0.005,
    "commit": "deadbeef",
    "env": "ci"
  },
  {
    "source": "quic",
    "scenario": "quic-agents",
    "phase": "connect",
    "latency_p50_ms": 10,
    "latency_p95_ms": 750,
    "latency_p99_ms": 1500,
    "commit": "deadbeef",
    "env": "ci"
  },
  {
    "source": "quic",
    "scenario": "quic-agents",
    "phase": "aggregate",
    "rps": 20,
    "error_rate": 0.02,
    "commit": "deadbeef",
    "env": "ci"
  }
]
JSON

echo "load-test VM push:"
RC=0
PATH="$TMP_ROOT/bin:$PATH" \
  KUBECTL_ARGS_FILE="$TMP_ROOT/kubectl.args" \
  KUBECTL_STDIN_FILE="$TMP_ROOT/payload.prom" \
  VM_NAMESPACE="observability" \
  VM_SERVICE="private-vm" \
  "$PUSH" "$TMP_ROOT/loadtest-summary.json" >/dev/null 2>&1 || RC=$?
if [ "$RC" -eq 0 ]; then pass "push exits 0"; else fail "push exited $RC"; fi

if grep -qF 'http://private-vm.observability.svc:8428/api/v1/import/prometheus' "$TMP_ROOT/kubectl.args"; then
  pass "uses configured VM endpoint"
else
  fail "VM endpoint missing from kubectl args"
fi

if grep -qF 'loadtest_latency_p95_ms{commit="deadbeef",env="ci",source="k6",scenario="api-baseline",phase="http"} 123.4' "$TMP_ROOT/payload.prom"; then
  pass "maps k6 latency"
else
  fail "k6 latency metric missing"
fi
if grep -qF 'loadtest_rps{commit="deadbeef",env="ci",source="k6",scenario="api-baseline",phase="http"} 42.5' "$TMP_ROOT/payload.prom"; then
  pass "maps k6 rps"
else
  fail "k6 rps metric missing"
fi
if grep -qF 'loadtest_error_rate{commit="deadbeef",env="ci",source="k6",scenario="api-baseline",phase="http"} 0.005' "$TMP_ROOT/payload.prom"; then
  pass "maps k6 error rate"
else
  fail "k6 error rate metric missing"
fi
if grep -qF 'loadtest_latency_p99_ms{commit="deadbeef",env="ci",source="quic",scenario="quic-agents",phase="connect"} 1500' "$TMP_ROOT/payload.prom"; then
  pass "maps QUIC latency"
else
  fail "QUIC latency metric missing"
fi
if grep -qF 'loadtest_error_rate{commit="deadbeef",env="ci",source="quic",scenario="quic-agents",phase="aggregate"} 0.02' "$TMP_ROOT/payload.prom"; then
  pass "maps QUIC aggregate error rate"
else
  fail "QUIC aggregate error metric missing"
fi

rc=0
"$PUSH" "$TMP_ROOT/missing.json" >/dev/null 2>&1 || rc=$?
if [ "$rc" -eq 2 ]; then pass "missing summary exits 2"; else fail "missing summary expected exit 2, got $rc"; fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
