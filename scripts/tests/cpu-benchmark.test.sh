#!/usr/bin/env bash
# Tests scripts/cpu-benchmark.sh against stand-ins for docker and kubectl that run the built
# benchmark directly: each mode holds it to a quarter processor and writes its reading.
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
# kubectl keeps the copied binary in a stand-in pod directory and answers its run with the reading
# an arm64 node prints, since this machine may not run that binary.
cat >"$WORK/bin/kubectl" <<'KUBECTL'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_LOG"
args=("$@")
[ "${args[0]}" = "-n" ] && args=("${args[@]:2}")
case "${args[0]}" in
  run | delete | wait) exit 0 ;;
  cp) cp "${args[1]}" "$FAKE_POD/cpubench" ;;
  exec)
    shift_to=0
    for i in "${!args[@]}"; do [ "${args[$i]}" = "--" ] && shift_to=$((i + 1)); done
    cmd=("${args[@]:$shift_to}")
    case "${cmd[0]}" in
      chmod) exit 0 ;;
      *) printf '{"rounds":600,"seconds":60,"rounds_per_second":10,"arch":"arm64","go":"go1.27.3"}\n' ;;
    esac
    ;;
  *) exit 64 ;;
esac
KUBECTL
chmod +x "$WORK/bin/"*
mkdir -p "$WORK/pod"

run_mode() {
  rm -f "$WORK/log"
  env PATH="$WORK/bin:$PATH" FAKE_LOG="$WORK/log" FAKE_POD="$WORK/pod" GOFLAGS=-buildvcs=false \
    "$ROOT/scripts/cpu-benchmark.sh" "$@" >"$WORK/out" 2>&1
}

echo "cpu benchmark:"

status=0
run_mode runner "$WORK/runner.json" || status=$?
if [ "$status" -eq 0 ] && jq -e '.rounds_per_second > 0 and .rounds > 0' "$WORK/runner.json" >/dev/null; then
  pass "the runner mode writes a reading of rounds a second"
else
  fail "the runner mode must write its reading (status=$status out=[$(cat "$WORK/out")])"
fi
if grep -qE -- '--cpus 0\.25' "$WORK/log"; then
  pass "the runner mode holds the benchmark to a quarter processor"
else
  fail "the runner mode must hold the benchmark to a quarter processor (calls=[$(cat "$WORK/log")])"
fi

status=0
run_mode cluster "$WORK/cluster.json" || status=$?
if [ "$status" -eq 0 ] && jq -e '.rounds_per_second > 0' "$WORK/cluster.json" >/dev/null \
  && grep -qF '"cpu":"250m"' "$WORK/log" && grep -qE '^-n opengate-staging delete pod ' "$WORK/log"; then
  pass "the cluster mode runs a quarter-processor pod in the staging namespace and removes it"
else
  fail "the cluster mode must run a quarter-processor pod and remove it (status=$status out=[$(cat "$WORK/out")] calls=[$(cat "$WORK/log")])"
fi
# An ELF file names its processor at byte 18: 183 is 64-bit ARM.
machine="$(python3 -c 'import sys; print(open(sys.argv[1], "rb").read(20)[18])' "$WORK/pod/cpubench" 2>/dev/null || echo none)"
if [ "$machine" = "183" ]; then
  pass "the cluster mode builds for the production node's processor"
else
  fail "the cluster mode must build for arm64 (out=[$(cat "$WORK/out")])"
fi

status=0
run_mode elsewhere "$WORK/x.json" || status=$?
if [ "$status" -eq 2 ] && grep -qF 'Usage' "$WORK/out"; then
  pass "an unknown mode prints the usage"
else
  fail "an unknown mode must exit 2 with the usage (status=$status)"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
