#!/usr/bin/env bash
# Offline tests for scripts/loadtest-regression-check.sh.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECK="$REPO_ROOT/scripts/loadtest-regression-check.sh"
[ -x "$CHECK" ] || {
  echo "FAIL: $CHECK not executable" >&2
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
assert_eq() {
  local name="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then pass "$name"; else fail "$name (want=[$want] got=[$got])"; fi
}
assert_contains() {
  local name="$1" needle="$2" haystack="$3"
  if printf '%s\n' "$haystack" | grep -qF "$needle"; then pass "$name"; else fail "$name (missing [$needle])"; fi
}
assert_not_contains() {
  local name="$1" needle="$2" haystack="$3"
  if printf '%s\n' "$haystack" | grep -qF "$needle"; then fail "$name (unexpected [$needle])"; else pass "$name"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
BIN_DIR="$WORK/bin"
mkdir -p "$BIN_DIR"

cat >"$BIN_DIR/kubectl" <<'SH'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >>"${KUBECTL_ARGS:-/dev/null}"
args="$*"
vec() { printf '{"status":"success","data":{"resultType":"vector","result":[%s]}}\n' "$1"; }
s() { printf '{"metric":{"source":"%s","scenario":"%s","phase":"%s"},"value":[2000,"%s"]}' "$1" "$2" "$3" "$4"; }
case "${VM_PROFILE:-seeded}" in
  empty) ;;
  invalid) printf '%s\n' 'not-json' ;;
  seeded)
    if printf '%s' "$args" | grep -q '/api/v1/export'; then
      # Previous error_rate sample for the exact source/scenario/phase selector.
      printf '%s\n' '{"metric":{"__name__":"loadtest_error_rate","source":"quic","scenario":"quic-agents","phase":"aggregate","commit":"older","env":"ci"},"values":[0.001],"timestamps":[1000]}'
    elif printf '%s' "$args" | grep -q 'count_over_time'; then
      vec "$(s quic quic-agents connect 10),$(s quic quic-agents aggregate 10),$(s k6 api-baseline http 10),$(s k6 concurrent-agents http 10)"
    elif printf '%s' "$args" | grep -q 'loadtest_latency_p95_ms'; then
      vec "$(s quic quic-agents connect 200),$(s k6 api-baseline http 100)"
    elif printf '%s' "$args" | grep -q 'loadtest_latency_p50_ms'; then
      vec "$(s quic quic-agents connect 100),$(s k6 api-baseline http 50)"
    elif printf '%s' "$args" | grep -q 'loadtest_latency_p99_ms'; then
      vec "$(s quic quic-agents connect 300),$(s k6 api-baseline http 150)"
    elif printf '%s' "$args" | grep -q 'loadtest_rps'; then
      vec "$(s quic quic-agents aggregate 200),$(s k6 concurrent-agents http 30)"
    elif printf '%s' "$args" | grep -q 'loadtest_error_rate'; then
      vec "$(s quic quic-agents aggregate 0),$(s k6 api-baseline http 0)"
    fi
    ;;
esac
exit "${KUBECTL_STATUS:-0}"
SH
chmod +x "$BIN_DIR/kubectl"

run_check() {
  local summary="$1"
  (
    export PATH="$BIN_DIR:$PATH"
    export KUBECTL_ARGS="$WORK/kubectl.args"
    export VM_NAMESPACE="observability"
    export VM_SERVICE="private-vm"
    export GITHUB_SHA="deadbeef"
    "$CHECK" "$summary"
  )
}

write_summary() {
  local file="$1" body="$2"
  printf '%s\n' "$body" >"$file"
}

echo "load-test regression checker:"

write_summary "$WORK/p95-regression.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"connect","latency_p95_ms":700,"latency_p99_ms":900,"commit":"deadbeef","env":"ci"},
  {"source":"k6","scenario":"api-baseline","phase":"http","latency_p95_ms":110,"latency_p99_ms":180,"commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(run_check "$WORK/p95-regression.json" 2>&1)" || rc=$?
assert_eq "p95 window breach exits 1" "1" "$rc"
assert_contains "p95 regression names breached series" "quic/quic-agents/connect latency_p95_ms" "$out"
assert_contains "p95 regression includes p99 context" "p99=900" "$out"
assert_not_contains "clean peer series stays out of alert" "k6/api-baseline/http latency_p95_ms" "$out"
assert_contains "window query excludes current commit" 'commit!="deadbeef"' "$(cat "$WORK/kubectl.args")"

write_summary "$WORK/rps-regression.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"aggregate","rps":90,"commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(run_check "$WORK/rps-regression.json" 2>&1)" || rc=$?
assert_eq "rps drop exits 1" "1" "$rc"
assert_contains "rps alert is direction-aware" "quic/quic-agents/aggregate rps" "$out"

write_summary "$WORK/error-rate-regression.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"aggregate","error_rate":0.02,"commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(run_check "$WORK/error-rate-regression.json" 2>&1)" || rc=$?
assert_eq "error-rate ceiling exits 1" "1" "$rc"
assert_contains "error-rate alert names ceiling" "error_rate" "$out"

write_summary "$WORK/p99-only.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"connect","latency_p95_ms":220,"latency_p99_ms":5000,"commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(run_check "$WORK/p99-only.json" 2>&1)" || rc=$?
assert_eq "p99-only breach stays green" "0" "$rc"
assert_contains "p99-only breach emits advisory context" "P99_ADVISORY:" "$out"
assert_not_contains "p99-only breach does not emit regression alert" "REGRESSION_ALERT:" "$out"

write_summary "$WORK/cold-start-under-ceiling.json" '[
  {"source":"k6","scenario":"api-baseline","phase":"http","latency_p95_ms":180,"commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(VM_PROFILE=empty run_check "$WORK/cold-start-under-ceiling.json" 2>&1)" || rc=$?
assert_eq "cold-start under absolute ceiling stays green" "0" "$rc"
assert_not_contains "cold-start under ceiling has no regression alert" "REGRESSION_ALERT:" "$out"

write_summary "$WORK/cold-start-over-ceiling.json" '[
  {"source":"k6","scenario":"api-baseline","phase":"http","latency_p95_ms":250,"commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(VM_PROFILE=empty run_check "$WORK/cold-start-over-ceiling.json" 2>&1)" || rc=$?
assert_eq "cold-start over absolute ceiling exits 1" "1" "$rc"
assert_contains "cold-start over ceiling alert names ceiling" "absolute ceiling" "$out"

write_summary "$WORK/fail-open.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"connect","latency_p95_ms":700,"commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(KUBECTL_STATUS=19 run_check "$WORK/fail-open.json" 2>&1)" || rc=$?
assert_eq "VM transport failure is fail-open under absolute ceiling" "0" "$rc"
assert_not_contains "transport failure has no regression alert" "REGRESSION_ALERT:" "$out"

write_summary "$WORK/nulls.json" '[
  {"source":"quic","scenario":"quic-agents","phase":"connect","latency_p95_ms":null,"rps":null,"error_rate":null,"commit":"deadbeef","env":"ci"}
]'
rc=0
out="$(run_check "$WORK/nulls.json" 2>&1)" || rc=$?
assert_eq "null metrics are skipped per series" "0" "$rc"
assert_not_contains "null metrics have no regression alert" "REGRESSION_ALERT:" "$out"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
