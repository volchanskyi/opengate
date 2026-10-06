#!/usr/bin/env bash
# Offline tests for scripts/lib/vm-query.sh, which reads each measurement's latest value per date
# before tonight; transport and parse failures yield no history.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

PASS=0
FAIL=0
FAILURES=()
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}

fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}

assert_eq() {
  if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (want=[$2] got=[$3])"; fi
}

# Epoch milliseconds, as the export API writes timestamps.
ms() { printf '%s000' "$(date -u -d "$1" +%s)"; }

# Fixture: the 21st has two runs, the 29th is tonight's own point, the second series has one night.
bin_dir="$TMP_ROOT/bin"
mkdir -p "$bin_dir"
cat >"$TMP_ROOT/nights.json" <<JSON
{"metric":{"__name__":"loadtest_latency_p95_ms","env":"ci","phase":"http","scenario":"api-baseline","source":"k6"},"values":[10,12,14,11,99],"timestamps":[$(ms '2026-09-20 11:00'),$(ms '2026-09-21 11:00'),$(ms '2026-09-21 13:30'),$(ms '2026-09-22 11:05'),$(ms '2026-09-29 11:00')]}
{"metric":{"__name__":"loadtest_latency_p95_ms","env":"ci","phase":"connect","scenario":"quic-agents","source":"quic"},"values":[300],"timestamps":[$(ms '2026-09-22 11:05')]}
JSON
cat >"$bin_dir/kubectl" <<'STUB'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >"$KUBECTL_ARGS"
case "${VM_QUERY_FIXTURE:-nights}" in
  nights) cat "$VM_NIGHTS" ;;
  empty) ;;
  invalid) printf '%s\n' 'not-json' ;;
esac
exit "${KUBECTL_STATUS:-0}"
STUB
chmod +x "$bin_dir/kubectl"

# shellcheck source=scripts/lib/vm-query.sh
. "$REPO_ROOT/scripts/lib/vm-query.sh"

TONIGHT="$(date -u -d '2026-09-29 10:57' +%s)"

run_lib() {
  (
    export PATH="$bin_dir:$PATH"
    export KUBECTL_ARGS="$TMP_ROOT/kubectl.args"
    export VM_NIGHTS="$TMP_ROOT/nights.json"
    export VM_NAMESPACE="observability"
    export VM_SERVICE="private-vm"
    export VM_RUN_STARTED_AT="${STARTED_OVERRIDE-$TONIGHT}"
    "$@"
  )
}

echo "vm-query nightly reader:"

API='phase=http,scenario=api-baseline,source=k6'
QUIC='phase=connect,scenario=quic-agents,source=quic'

out="$(run_lib vm_query_nightly loadtest_latency_p95_ms 'env="ci"' 14)"
assert_eq "each date before tonight's gives its latest reading, per measurement" \
  "$(printf '%s\t2026-09-22\t300\n%s\t2026-09-20\t10\n%s\t2026-09-21\t14\n%s\t2026-09-22\t11' "env=ci,$QUIC" "env=ci,$API" "env=ci,$API" "env=ci,$API")" \
  "$out"
if grep -qF 'loadtest_latency_p95_ms{env="ci"}' "$TMP_ROOT/kubectl.args" \
  && grep -qF 'http://private-vm.observability.svc:8428/api/v1/export' "$TMP_ROOT/kubectl.args" \
  && grep -qF -- '--rm -i --restart=Never' "$TMP_ROOT/kubectl.args"; then
  pass "the readings are exported through an auto-cleaned pod"
else
  fail "the readings are exported through an auto-cleaned pod (args=[$(cat "$TMP_ROOT/kubectl.args")])"
fi
if grep -qF 'commit' "$TMP_ROOT/kubectl.args"; then
  fail "the reader asks nothing about commits"
else
  pass "the reader asks nothing about commits"
fi

out="$(run_lib vm_query_nightly loadtest_latency_p95_ms 'env="ci"' 2)"
assert_eq "the window is the latest N dates" \
  "$(printf '%s\t2026-09-22\t300\n%s\t2026-09-21\t14\n%s\t2026-09-22\t11' "env=ci,$QUIC" "env=ci,$API" "env=ci,$API")" \
  "$out"

out="$(run_lib vm_nightly_window loadtest_latency_p95_ms 'env="ci"' 14)"
assert_eq "the window is each measurement's median over its dates, their count, and the newest" \
  "$(printf '%s\t300\t1\t300\n%s\t11\t3\t11' "env=ci,$QUIC" "env=ci,$API")" \
  "$out"

TOMORROW="$(date -u -d '2026-09-30 10:57' +%s)"
out="$(STARTED_OVERRIDE="$TOMORROW" run_lib vm_nightly_window loadtest_latency_p95_ms 'env="ci"' 14)"
assert_eq "tomorrow reads tonight as the newest night" \
  "$(printf '%s\t300\t1\t300\n%s\t12.5\t4\t99' "env=ci,$QUIC" "env=ci,$API")" \
  "$out"

for fixture in empty invalid; do
  out="$(VM_QUERY_FIXTURE="$fixture" run_lib vm_query_nightly loadtest_latency_p95_ms 'env="ci"' 14 2>/dev/null)"
  assert_eq "an $fixture answer is no history rather than a failure" "" "$out"
done
out="$(KUBECTL_STATUS=19 run_lib vm_nightly_window loadtest_latency_p95_ms 'env="ci"' 14 2>/dev/null)"
assert_eq "an unreachable store is no history rather than a failure" "" "$out"

if STARTED_OVERRIDE="" run_lib vm_query_nightly loadtest_latency_p95_ms 'env="ci"' 14 >/dev/null 2>&1; then
  fail "a reader that does not know tonight's date refuses"
else
  pass "a reader that does not know tonight's date refuses"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
