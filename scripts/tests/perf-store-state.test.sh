#!/usr/bin/env bash
# Tests scripts/perf-store-state.sh against a stand-in for docker: the store's state is one line,
# and its last log lines follow whenever the store or a probe reading says something went wrong.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
STATE="$ROOT/scripts/perf-store-state.sh"

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

# docker inspect renders the state the test names; docker logs hands the container's own error
# stream to stderr, which is where the store writes its log.
cat >"$WORK/bin/docker" <<'DOCKER'
#!/usr/bin/env bash
case "$1" in
  inspect)
    if [ -n "${FAKE_MISSING:-}" ]; then
      echo "Error: No such object: ${*: -1}" >&2
      exit 1
    fi
    printf '%s\n' "$FAKE_STATE"
    ;;
  logs)
    printf '%s\n' "$*" >>"$FAKE_LOG"
    echo "store log: cannot allocate memory for the in-memory parts" >&2
    ;;
  *) exit 64 ;;
esac
DOCKER
chmod +x "$WORK/bin/docker"

fine='{"senders":1,"store":{"stopped_answering":false}}'

# show STATE READING... runs the script with the stand-in reporting STATE.
show() {
  local state="$1"
  shift
  local i=0 files=()
  for reading in "$@"; do
    i=$((i + 1))
    printf '%s\n' "$reading" >"$WORK/probe-$i.json"
    files+=("$WORK/probe-$i.json")
  done
  : >"$WORK/log"
  status=0
  env PATH="$WORK/bin:$PATH" FAKE_STATE="$state" FAKE_LOG="$WORK/log" FAKE_MISSING="${MISSING:-}" \
    "$STATE" opengate-perf-metrics "${files[@]}" >"$WORK/out" 2>"$WORK/err" || status=$?
}
out_has() { grep -qF -- "$1" "$WORK/out"; }

echo "store state:"

show 'running false 0 0' "$fine" "$fine"
if [ "$status" -eq 0 ] && out_has 'opengate-perf-metrics: running, killed out of memory: false, exit code 0, restarts 0' \
  && ! out_has 'store log:'; then
  pass "a running store whose readings are whole is one line, with no log"
else
  fail "a healthy store must be one line (status=$status out=[$(cat "$WORK/out")])"
fi

show 'running false 0 0' "$fine" '{"senders":2,"store":{"stopped_answering":true}}'
if [ "$status" -eq 0 ] && out_has 'store log: cannot allocate memory' && out_has 'last 100 log lines' \
  && grep -qF -- '--tail 100' "$WORK/log"; then
  pass "a reading that says the store stopped answering prints its last 100 log lines"
else
  fail "a stopped store's log must print (status=$status out=[$(cat "$WORK/out")] log=[$(cat "$WORK/log")])"
fi

show 'running false 0 0' "$fine" '{}'
if [ "$status" -eq 0 ] && out_has 'store log:'; then
  pass "a probe step that wrote no reading prints the store's log"
else
  fail "a missing reading must print the log (status=$status out=[$(cat "$WORK/out")])"
fi

show 'exited true 137 0' "$fine"
if [ "$status" -eq 0 ] && out_has 'exited, killed out of memory: true, exit code 137' && out_has 'store log:'; then
  pass "a store killed out of memory says so and prints its log"
else
  fail "an out-of-memory kill must be named with its log (status=$status out=[$(cat "$WORK/out")])"
fi

MISSING=1 show 'running false 0 0' "$fine"
if [ "$status" -eq 1 ] && grep -qF 'no container' "$WORK/err"; then
  pass "a store with no container is an error, never a healthy line"
else
  fail "a missing container must exit 1 (status=$status err=[$(cat "$WORK/err")])"
fi

status=0
"$STATE" >"$WORK/out" 2>&1 || status=$?
if [ "$status" -eq 2 ] && out_has 'Usage'; then
  pass "a call naming no container prints its usage"
else
  fail "a call with no arguments must exit 2 (status=$status)"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
