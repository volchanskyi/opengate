#!/usr/bin/env bash
# Tests for scripts/perf-runner-record.sh, which reads what the runner and the server's allowance
# did over one leg.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RECORD="$REPO_ROOT/scripts/perf-runner-record.sh"
[ -x "$RECORD" ] || {
  echo "FAIL: $RECORD not executable" >&2
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
  if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (want=[$2] got=[$3])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin" "$WORK/cgroup"
cat >"$WORK/bin/lscpu" <<'EOF'
#!/usr/bin/env bash
printf 'Architecture:  x86_64\nModel name:    AMD EPYC 7763 64-Core Processor\nCPU(s):  4\n'
EOF
chmod +x "$WORK/bin/lscpu"
export PATH="$WORK/bin:$PATH"
export PERF_PROC_STAT="$WORK/stat"

# reading USER STEAL PERIODS THROTTLED THROTTLED_USEC writes the counters one snapshot reads.
reading() {
  printf 'cpu  %s 0 100 800 0 0 0 %s 0 0\ncpu0 1 0 1 1 0 0 0 0 0 0\n' "$1" "$2" >"$WORK/stat"
  printf 'usage_usec 1\nnr_periods %s\nnr_throttled %s\nthrottled_usec %s\n' "$3" "$4" "$5" \
    >"$WORK/cgroup/cpu.stat"
}

echo "perf-runner-record:"

reading 100 0 1000 10 50000
"$RECORD" snapshot "$WORK/cgroup" "$WORK/before.json"
reading 1100 100 3000 610 2050000
"$RECORD" snapshot "$WORK/cgroup" "$WORK/after.json"
"$RECORD" delta "$WORK/before.json" "$WORK/after.json" >"$WORK/runner.json"

assert_eq "the processor model is named" "AMD EPYC 7763 64-Core Processor" \
  "$(jq -r '.processor_model' "$WORK/runner.json")"
# Between the readings: 1000 user and 100 stolen of 1100 ticks.
assert_eq "steal is the share of the leg's processor time the host took" "9.09" \
  "$(jq -r '.steal_percent' "$WORK/runner.json")"
assert_eq "the server's throttled periods over the leg" "600" \
  "$(jq -r '.server_throttled_periods' "$WORK/runner.json")"
assert_eq "as a share of its periods" "0.3" "$(jq -r '.server_throttled_share' "$WORK/runner.json")"
assert_eq "and the time it was held back, in milliseconds" "2000" \
  "$(jq -r '.server_throttled_ms' "$WORK/runner.json")"

rc=0
"$RECORD" snapshot "$WORK/no-such-cgroup" "$WORK/x.json" 2>/dev/null || rc=$?
assert_eq "a cgroup with no processor counters is refused" "1" "$rc"

rc=0
"$RECORD" delta "$WORK/before.json" "$WORK/missing.json" 2>/dev/null || rc=$?
assert_eq "a missing snapshot is refused" "2" "$rc"

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
