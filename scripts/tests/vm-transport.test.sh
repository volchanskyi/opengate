#!/usr/bin/env bash
# Offline payload and transport tests for private in-cluster VictoriaMetrics access.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

PASS=0
FAIL=0
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}

fail() {
  FAIL=$((FAIL + 1))
  printf '  FAIL %s\n' "$1" >&2
}

bin_dir="$TMP_ROOT/bin"
mkdir -p "$bin_dir"
cat >"$bin_dir/kubectl" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >"$KUBECTL_ARGS"
cat >"$KUBECTL_STDIN"
exit "${KUBECTL_STATUS:-0}"
EOF
chmod +x "$bin_dir/kubectl"

run_vm_push() {
  local args_file="$1"
  local stdin_file="$2"
  shift 2
  env \
    PATH="$bin_dir:$PATH" \
    KUBECTL_ARGS="$args_file" \
    KUBECTL_STDIN="$stdin_file" \
    VM_NAMESPACE="observability" \
    VM_SERVICE="private-vm" \
    "$@"
}

echo "vm transport:"

cat >"$TMP_ROOT/metrics.prom" <<'EOF'
# TYPE mutation_score gauge
mutation_score{commit="abc123",env="ci",lang="go"} 85.5
EOF

if output="$(
  run_vm_push "$TMP_ROOT/push.args" "$TMP_ROOT/push.stdin" \
    "$REPO_ROOT/scripts/lib/vm-push.sh" "$TMP_ROOT/metrics.prom" 2>&1
)" \
  && grep -qF -- "--rm -i --restart=Never" "$TMP_ROOT/push.args" \
  && grep -qF -- "--image=docker.io/curlimages/curl:8.11.1" "$TMP_ROOT/push.args" \
  && grep -qF "http://private-vm.observability.svc:8428/api/v1/import/prometheus" "$TMP_ROOT/push.args" \
  && grep -qF "Content-Type: text/plain; version=0.0.4" "$TMP_ROOT/push.args" \
  && cmp -s "$TMP_ROOT/metrics.prom" "$TMP_ROOT/push.stdin" \
  && [ -z "$output" ]; then
  pass "VM push uses an auto-cleaned kubectl pod and preserves Prometheus text"
else
  fail "VM push uses an auto-cleaned kubectl pod and preserves Prometheus text"
fi

cat >"$TMP_ROOT/stdin.prom" <<'EOF'
pmat_repo_score{commit="def456",env="ci"} 91.25
EOF
if run_vm_push "$TMP_ROOT/stdin.args" "$TMP_ROOT/stdin.captured" \
  "$REPO_ROOT/scripts/lib/vm-push.sh" <"$TMP_ROOT/stdin.prom" \
  && cmp -s "$TMP_ROOT/stdin.prom" "$TMP_ROOT/stdin.captured"; then
  pass "VM push reads Prometheus text from stdin when no file is provided"
else
  fail "VM push reads Prometheus text from stdin when no file is provided"
fi

cat >"$TMP_ROOT/missing-label.prom" <<'EOF'
mutation_score{commit="abc123",lang="go"} 85.5
EOF
if run_vm_push "$TMP_ROOT/missing-label.args" "$TMP_ROOT/missing-label.stdin" \
  "$REPO_ROOT/scripts/lib/vm-push.sh" "$TMP_ROOT/missing-label.prom" >/dev/null 2>&1; then
  fail "VM push rejects metrics missing mandatory env label"
elif [ ! -s "$TMP_ROOT/missing-label.args" ]; then
  pass "VM push rejects metrics missing mandatory env label before kubectl"
else
  fail "VM push rejects metrics missing mandatory env label before kubectl"
fi

cat >"$TMP_ROOT/malformed.prom" <<'EOF'
not a prometheus sample
EOF
if run_vm_push "$TMP_ROOT/malformed.args" "$TMP_ROOT/malformed.stdin" \
  "$REPO_ROOT/scripts/lib/vm-push.sh" "$TMP_ROOT/malformed.prom" >/dev/null 2>&1; then
  fail "VM push rejects malformed Prometheus text"
elif [ ! -s "$TMP_ROOT/malformed.args" ]; then
  pass "VM push rejects malformed Prometheus text before kubectl"
else
  fail "VM push rejects malformed Prometheus text before kubectl"
fi

if KUBECTL_STATUS=19 run_vm_push "$TMP_ROOT/fail.args" "$TMP_ROOT/fail.stdin" \
  "$REPO_ROOT/scripts/lib/vm-push.sh" "$TMP_ROOT/metrics.prom" >/dev/null 2>&1; then
  fail "kubectl/VM transport failure propagates"
else
  pass "kubectl/VM transport failure propagates"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
