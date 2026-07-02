#!/usr/bin/env bash
# Offline tests for the shared VictoriaMetrics read-back library scripts/lib/vm-query.sh.
# Both modes must be fail-open: any transport/empty/invalid input yields empty
# output and exit 0. vm_query_latest reads the newest single sample; vm_query_window
# parses an /api/v1/query vector into per-series values keyed by sorted labels.

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

# Mock kubectl serves the export API (newest-sample) and the query API (window
# vector) from separate fixtures, chosen by inspecting the requested URL.
bin_dir="$TMP_ROOT/bin"
mkdir -p "$bin_dir"
cat >"$bin_dir/kubectl" <<'EOF'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >"$KUBECTL_ARGS"
if printf '%s' "$*" | grep -q '/api/v1/query'; then
  case "${VM_WINDOW_FIXTURE:-vector}" in
    vector)
      cat <<'JSON'
{"status":"success","data":{"resultType":"vector","result":[
  {"metric":{"scenario":"login","phase":"steady","source":"edge"},"value":[2000,"12.5"]},
  {"metric":{"scenario":"login","phase":"steady","source":"central"},"value":[2000,"9"]}
]}}
JSON
      ;;
    empty) ;;
    invalid) printf '%s\n' 'not-json' ;;
  esac
else
  case "${VM_QUERY_FIXTURE:-values}" in
    values)
      cat <<'JSON'
{"metric":{"__name__":"pmat_repo_score","commit":"older","env":"ci"},"values":[60.5],"timestamps":[1000]}
{"metric":{"__name__":"pmat_repo_score","commit":"newer","env":"ci"},"values":[63.5],"timestamps":[2000]}
JSON
      ;;
    empty) ;;
    invalid) printf '%s\n' 'not-json' ;;
  esac
fi
exit "${KUBECTL_STATUS:-0}"
EOF
chmod +x "$bin_dir/kubectl"

# shellcheck source=scripts/lib/vm-query.sh
. "$REPO_ROOT/scripts/lib/vm-query.sh"

# Run a library function with the mock kubectl on PATH and the private-VM
# transport env. Per-case fixtures (VM_QUERY_FIXTURE / VM_WINDOW_FIXTURE /
# KUBECTL_STATUS / VM_EXCLUDE_COMMIT) are inherited from the caller.
run_lib() {
  (
    export PATH="$bin_dir:$PATH"
    export KUBECTL_ARGS="$TMP_ROOT/kubectl.args"
    export VM_NAMESPACE="observability"
    export VM_SERVICE="private-vm"
    "$@"
  )
}

echo "vm-query latest mode:"

if value="$(run_lib vm_query_latest pmat_repo_score 'env="ci"')" \
  && [ "$value" = "63.5" ] \
  && grep -qF -- '--rm -i --restart=Never' "$TMP_ROOT/kubectl.args" \
  && grep -qF 'http://private-vm.observability.svc:8428/api/v1/export' "$TMP_ROOT/kubectl.args" \
  && grep -qF 'pmat_repo_score{env="ci"}' "$TMP_ROOT/kubectl.args"; then
  pass "latest returns the newest VM sample through an auto-cleaned pod"
else
  fail "latest should return the newest VM sample"
fi

if value="$(VM_QUERY_FIXTURE=empty run_lib vm_query_latest pmat_repo_score 'env="ci"')" \
  && [ -z "$value" ]; then
  pass "latest empty history is fail-open"
else
  fail "latest empty history should print nothing"
fi

if value="$(KUBECTL_STATUS=19 run_lib vm_query_latest pmat_repo_score 'env="ci"' 2>/dev/null)" \
  && [ -z "$value" ]; then
  pass "latest transport failure is fail-open"
else
  fail "latest transport failure should print nothing and exit 0"
fi

if value="$(VM_QUERY_FIXTURE=invalid run_lib vm_query_latest pmat_repo_score 'env="ci"' 2>/dev/null)" \
  && [ -z "$value" ]; then
  pass "latest invalid response is fail-open"
else
  fail "latest invalid response should print nothing"
fi

VM_EXCLUDE_COMMIT="deadbeef" run_lib vm_query_latest pmat_repo_score 'env="ci"' >/dev/null
if grep -qF 'pmat_repo_score{env="ci",commit!="deadbeef"}' "$TMP_ROOT/kubectl.args"; then
  pass "latest excludes the current commit when VM_EXCLUDE_COMMIT is set"
else
  fail "latest should exclude the current commit when VM_EXCLUDE_COMMIT is set"
fi

echo "vm-query window mode:"

if output="$(run_lib vm_query_window 'quantile(0.5, latency_ms[1h])')" \
  && [ "$(printf '%s\n' "$output" | grep -c .)" = "2" ] \
  && printf '%s\n' "$output" | grep -qP '^phase=steady,scenario=login,source=edge\t12\.5$' \
  && printf '%s\n' "$output" | grep -qP '^phase=steady,scenario=login,source=central\t9$' \
  && grep -qF 'http://private-vm.observability.svc:8428/api/v1/query' "$TMP_ROOT/kubectl.args" \
  && grep -qF 'quantile(0.5, latency_ms[1h])' "$TMP_ROOT/kubectl.args"; then
  pass "window returns per-series values keyed by sorted labels"
else
  fail "window should return per-series values keyed by sorted labels"
fi

if output="$(VM_WINDOW_FIXTURE=empty run_lib vm_query_window 'quantile(0.5, latency_ms[1h])')" \
  && [ -z "$output" ]; then
  pass "window empty response is fail-open"
else
  fail "window empty response should print nothing"
fi

if output="$(VM_WINDOW_FIXTURE=invalid run_lib vm_query_window 'quantile(0.5, latency_ms[1h])' 2>/dev/null)" \
  && [ -z "$output" ]; then
  pass "window invalid response is fail-open"
else
  fail "window invalid response should print nothing"
fi

if output="$(KUBECTL_STATUS=19 run_lib vm_query_window 'quantile(0.5, latency_ms[1h])' 2>/dev/null)" \
  && [ -z "$output" ]; then
  pass "window transport failure is fail-open"
else
  fail "window transport failure should print nothing and exit 0"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
