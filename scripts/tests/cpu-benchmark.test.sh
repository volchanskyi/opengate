#!/usr/bin/env bash
# Tests scripts/cpu-benchmark.sh against a stand-in for docker that runs the built benchmark
# directly: the run is held to a quarter processor and writes its reading.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"

# docker run records its arguments and runs the mounted benchmark with a short window.
cat >"$WORK/bin/docker" <<'DOCKER'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_LOG"
[ "$1" = "run" ] || exit 64
mount=""
args=("$@")
for i in "${!args[@]}"; do
  [ "${args[$i]}" = "-v" ] && mount="${args[$((i + 1))]%%:*}"
done
exec "$mount/cpubench" -window=50ms
DOCKER
chmod +x "$WORK/bin/"*

run_mode() {
  rm -f "$WORK/log"
  env PATH="$WORK/bin:$PATH" FAKE_LOG="$WORK/log" GOFLAGS=-buildvcs=false \
    "$ROOT/scripts/cpu-benchmark.sh" "$@" >"$WORK/out" 2>&1
}

echo "cpu benchmark:"

status=0
run_mode "$WORK/runner.json" || status=$?
if [ "$status" -eq 0 ] && jq -e '.rounds_per_second > 0 and .rounds > 0' "$WORK/runner.json" >/dev/null; then
  pass "the benchmark writes a reading of rounds a second"
else
  fail "the benchmark must write its reading (status=$status out=[$(cat "$WORK/out")])"
fi
if grep -qE -- '--cpus 0\.25' "$WORK/log"; then
  pass "the benchmark is held to a quarter processor"
else
  fail "the benchmark must be held to a quarter processor (calls=[$(cat "$WORK/log")])"
fi

status=0
run_mode runner "$WORK/x.json" || status=$?
if [ "$status" -eq 2 ] && grep -qF 'Usage' "$WORK/out" && [ ! -e "$WORK/x.json" ]; then
  pass "a second argument prints the usage and writes nothing"
else
  fail "a second argument must exit 2 with the usage (status=$status)"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
