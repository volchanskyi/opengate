#!/usr/bin/env bash
# Offline tests for the PMAT previous-value query against VictoriaMetrics.

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
set -euo pipefail
printf '%s\n' "$*" >"$KUBECTL_ARGS"
case "${VM_QUERY_FIXTURE:-values}" in
  values)
    cat <<'JSON'
{"metric":{"__name__":"pmat_repo_score","commit":"older","env":"ci"},"values":[60.5],"timestamps":[1000]}
{"metric":{"__name__":"pmat_repo_score","commit":"newer","env":"ci"},"values":[63.5],"timestamps":[2000]}
pod "vm-query-test" deleted from observability namespace
JSON
    ;;
  empty) ;;
  invalid) printf '%s\n' 'not-json' ;;
esac
exit "${KUBECTL_STATUS:-0}"
EOF
chmod +x "$bin_dir/kubectl"

run_query() {
  local field="$1"
  PATH="$bin_dir:$PATH" \
    KUBECTL_ARGS="$TMP_ROOT/kubectl.args" \
    VM_NAMESPACE="observability" \
    VM_SERVICE="private-vm" \
    "$REPO_ROOT/scripts/pmat-vm-query.sh" "$field"
}

echo "PMAT VM previous-value query:"

if value="$(run_query repo_score)" \
  && [ "$value" = "63.5" ] \
  && grep -qF -- '--rm -i --restart=Never' "$TMP_ROOT/kubectl.args" \
  && grep -qF 'http://private-vm.observability.svc:8428/api/v1/export' "$TMP_ROOT/kubectl.args" \
  && grep -qF 'pmat_repo_score{env="ci"}' "$TMP_ROOT/kubectl.args"; then
  pass "repo score returns the newest VM sample through an auto-cleaned pod"
else
  fail "repo score should return the newest VM sample"
fi

VM_QUERY_FIXTURE=values run_query below_bplus >/dev/null
if grep -qF 'pmat_below_bplus{env="ci"}' "$TMP_ROOT/kubectl.args"; then
  pass "below-B+ selects the matching metric"
else
  fail "below-B+ should select the matching metric"
fi

if value="$(VM_QUERY_FIXTURE=empty run_query repo_score)" && [ -z "$value" ]; then
  pass "an empty history is fail-soft"
else
  fail "an empty history should print nothing"
fi

if value="$(KUBECTL_STATUS=19 run_query repo_score 2>/dev/null)" && [ -z "$value" ]; then
  pass "a VM transport failure is fail-soft"
else
  fail "a VM transport failure should print nothing"
fi

if value="$(VM_QUERY_FIXTURE=invalid run_query repo_score 2>/dev/null)" && [ -z "$value" ]; then
  pass "an invalid VM response is fail-soft"
else
  fail "an invalid VM response should print nothing"
fi

if run_query unknown >/dev/null 2>&1; then
  fail "an unknown field should fail"
else
  pass "an unknown field fails"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
