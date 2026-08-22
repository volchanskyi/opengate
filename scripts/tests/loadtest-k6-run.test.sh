#!/usr/bin/env bash
# Tests for scripts/loadtest-k6-run.sh — the k6 scenario runner that decides
# whether a run produced a measurement worth trending.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RUNNER="$REPO_ROOT/scripts/loadtest-k6-run.sh"
[ -x "$RUNNER" ] || {
  echo "FAIL: $RUNNER not executable" >&2
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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# A k6 stand-in that writes the summary export it was asked for and then exits
# with the code the case under test needs. Real k6 writes the export for an
# aborted run too, which is the whole reason the runner has a decision to make.
make_fake_k6() {
  local exit_code="$1"
  cat >"$WORK/k6" <<EOF
#!/usr/bin/env bash
for arg in "\$@"; do
  case "\$prev" in
    --summary-export) printf '{"metrics":{}}' >"\$arg" ;;
  esac
  prev="\$arg"
done
exit $exit_code
EOF
  chmod +x "$WORK/k6"
}

run_case() {
  local exit_code="$1"
  make_fake_k6 "$exit_code"
  rm -rf "$WORK/summaries"
  mkdir -p "$WORK/summaries"
  STATUS=0
  K6_BIN="$WORK/k6" \
    LOADTEST_K6_SUMMARY_DIR="$WORK/summaries" \
    LOADTEST_BASE_URL="http://127.0.0.1:18080" \
    K6_SUMMARY_TREND_STATS="avg,p(95)" \
    "$RUNNER" api-baseline load/k6/scenarios/api-baseline.js >"$WORK/out.txt" 2>&1 || STATUS=$?
}

echo "loadtest-k6-run:"

# A clean run measured the fleet: keep the export, succeed.
run_case 0
assert_eq "clean run exits 0" "0" "$STATUS"
if [ -f "$WORK/summaries/api-baseline.json" ]; then
  pass "clean run keeps the summary export"
else
  fail "clean run keeps the summary export"
fi

# A threshold failure is a measurement — a slow fleet is exactly what the trend
# exists to record — so the row survives. Whether the breach fails the run is a
# decision the profile's gates make against the stored rows, not one k6's exit
# code makes on its own, so the scenario reports success and says what breached.
run_case 99
assert_eq "threshold failure is reported as a measurement" "0" "$STATUS"
if [ -f "$WORK/summaries/api-baseline.json" ]; then
  pass "threshold failure keeps the summary export"
else
  fail "threshold failure keeps the summary export"
fi
if grep -q "threshold" "$WORK/out.txt"; then
  pass "threshold failure is announced rather than swallowed"
else
  fail "threshold failure is announced rather than swallowed"
fi
if [ -f "$WORK/summaries/api-baseline.thresholds" ]; then
  pass "threshold failure is recorded for the gate to read"
else
  fail "threshold failure is recorded for the gate to read"
fi

# A script exception aborts before the workload runs. Its export holds the two
# or three requests setup managed, and trending those numbers drags the window
# median down for every later run, so the export is discarded.
run_case 107
assert_eq "script exception propagates 107" "107" "$STATUS"
if [ -f "$WORK/summaries/api-baseline.json" ]; then
  fail "script exception discards the summary export"
else
  pass "script exception discards the summary export"
fi
if grep -q "api-baseline" "$WORK/out.txt"; then
  pass "aborted run names the scenario it discarded"
else
  fail "aborted run names the scenario it discarded"
fi

# A clean run records no threshold breach, so the gate can tell a scenario that
# cleared its marks from one that was never asked.
run_case 0
if [ -f "$WORK/summaries/api-baseline.thresholds" ]; then
  fail "a clean run records no threshold breach"
else
  pass "a clean run records no threshold breach"
fi

# An export that never appeared after a clean run means the runner and the
# workflow disagree about where summaries land — a silent no-row otherwise.
cat >"$WORK/k6" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$WORK/k6"
rm -rf "$WORK/summaries"
mkdir -p "$WORK/summaries"
STATUS=0
K6_BIN="$WORK/k6" \
  LOADTEST_K6_SUMMARY_DIR="$WORK/summaries" \
  LOADTEST_BASE_URL="http://127.0.0.1:18080" \
  K6_SUMMARY_TREND_STATS="avg,p(95)" \
  "$RUNNER" api-baseline load/k6/scenarios/api-baseline.js >"$WORK/out.txt" 2>&1 || STATUS=$?
if [ "$STATUS" -ne 0 ]; then
  pass "a clean run that wrote no export fails"
else
  fail "a clean run that wrote no export fails"
fi

# The workflow must drive every k6 scenario through the runner, or a crashed
# scenario goes on trending its setup requests.
WORKFLOW="$REPO_ROOT/.github/workflows/load-test.yml"
direct="$(grep -cE '^\s+/tmp/k6 run' "$WORKFLOW" || true)"
assert_eq "load-test.yml invokes k6 only through the runner" "0" "$direct"
wrapped="$(grep -cE 'scripts/loadtest-k6-run\.sh' "$WORKFLOW" || true)"
assert_eq "load-test.yml runs three k6 scenarios through the runner" "3" "$wrapped"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
