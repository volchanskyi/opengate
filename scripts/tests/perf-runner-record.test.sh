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

assert_eq "a server that ran to the end is recorded as running" "false" \
  "$(jq -r '.server_stopped' "$WORK/runner.json")"

# The server's cgroup goes with its container, so the second reading of a stopped server has none;
# Docker keeps the container's state, as `docker inspect -f '{{json .State}}'` prints it.
reading 1100 100 3000 610 2050000
cat >"$WORK/state.json" <<'STATE'
{"Status":"exited","Running":false,"Paused":false,"Restarting":false,"OOMKilled":true,"Dead":false,"Pid":0,"ExitCode":137,"Error":"","StartedAt":"2026-11-23T19:17:01.104Z","FinishedAt":"2026-11-23T19:20:41.512Z"}
STATE
"$RECORD" snapshot --server-stopped "$WORK/state.json" "$WORK/stopped.json"
"$RECORD" delta "$WORK/before.json" "$WORK/stopped.json" >"$WORK/stopped-runner.json"
assert_eq "a stopped server leaves the runner's processor model" "AMD EPYC 7763 64-Core Processor" \
  "$(jq -r '.processor_model' "$WORK/stopped-runner.json")"
assert_eq "and the runner's steal" "9.09" "$(jq -r '.steal_percent' "$WORK/stopped-runner.json")"
assert_eq "and says the server stopped" "true" "$(jq -r '.server_stopped' "$WORK/stopped-runner.json")"
assert_eq "with no throttling claimed for it" "null null null null" \
  "$(jq -r '"\(.server_periods) \(.server_throttled_periods) \(.server_throttled_share) \(.server_throttled_ms)"' \
    "$WORK/stopped-runner.json")"

assert_eq "and why: the exit code it stopped with" "137" "$(jq -r '.server_exit_code' "$WORK/stopped-runner.json")"
assert_eq "whether the kernel killed it for memory" "true" "$(jq -r '.server_oom_killed' "$WORK/stopped-runner.json")"
assert_eq "and when it stopped" "2026-11-23T19:20:41.512Z" "$(jq -r '.server_finished_at' "$WORK/stopped-runner.json")"
assert_eq "a server that ran to the end names no exit" "null null null" \
  "$(jq -r '"\(.server_exit_code) \(.server_oom_killed) \(.server_finished_at)"' "$WORK/runner.json")"

rc=0
"$RECORD" snapshot --server-stopped "$WORK/state.json" "$WORK/x.json" extra 2>/dev/null || rc=$?
assert_eq "a stopped-server reading takes the container's state and one output path" "2" "$rc"
rc=0
"$RECORD" snapshot --server-stopped "$WORK/x.json" 2>/dev/null || rc=$?
assert_eq "a stopped-server reading without the container's state is refused" "2" "$rc"
rc=0
"$RECORD" snapshot --server-stopped "$WORK/no-such-state.json" "$WORK/x.json" 2>/dev/null || rc=$?
assert_eq "a container state that cannot be read is refused" "1" "$rc"

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
