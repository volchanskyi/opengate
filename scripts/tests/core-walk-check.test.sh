#!/usr/bin/env bash
# Tests for scripts/core-walk-check.sh — the pinned core reader still reads a
# core the pinned Go toolchain writes, and walks a live object back to its root.
#
# go, gcore, viewcore and sudo are stand-ins on PATH. The go stand-in records
# the toolchain it was asked for and builds a probe that only says it is ready,
# so what is exercised is the script's own sequence and refusals; the real
# reader against a real core is what the workflow that calls it runs.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECK="$REPO_ROOT/scripts/core-walk-check.sh"
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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
STUB="$WORK/stub"
mkdir -p "$STUB"
export STUB_LOG="$WORK/calls.log"

cat >"$STUB/go" <<'STUB_GO'
#!/usr/bin/env bash
# go build -o <out> .   — the probe it leaves only says it is ready, then waits.
printf 'go %s toolchain=%s\n' "$*" "${GOTOOLCHAIN:-}" >>"$STUB_LOG"
[ "$1" = build ] || exit 1
out="$3"
printf '#!/usr/bin/env bash\necho ready\nexec sleep 60\n' >"$out"
chmod +x "$out"
STUB_GO
cat >"$STUB/sudo" <<'STUB_SUDO'
#!/usr/bin/env bash
"$@"
STUB_SUDO
cat >"$STUB/gcore" <<'STUB_GCORE'
#!/usr/bin/env bash
# gcore -o <prefix> <pid>
printf 'core bytes\n' >"$2.$3"
STUB_GCORE
cat >"$STUB/readelf" <<'STUB_READELF'
#!/usr/bin/env bash
echo "  [28] .debug_info      PROGBITS"
STUB_READELF
cat >"$STUB/viewcore" <<'STUB_VIEWCORE'
#!/usr/bin/env bash
command=""
for arg in "$@"; do
  case "$arg" in
    overview | breakdown | goroutines | histogram | objects | reachable) command="$arg"; break ;;
  esac
done
printf 'viewcore %s\n' "$command" >>"$STUB_LOG"
case "$command" in
  overview)
    [ "${STUB_UNREADABLE:-0}" = "1" ] && { echo "unrecognized runtime version" >&2; exit 1; }
    echo "runtime go1.27.1"
    ;;
  breakdown)  echo "all 4194304" ;;
  goroutines) echo "goroutine 1 sleeping" ;;
  histogram)
    echo "count size bytes  type"
    echo "  512 4104 2101248  main.node"
    ;;
  objects) echo "        c000180000 main.node" ;;
  reachable)
    if [ "${STUB_NO_ROOT:-0}" = "1" ]; then
      echo "panic: can't find a root that can reach the object"
      exit 2
    fi
    echo "main.root → c000180000 main.node"
    ;;
  *) exit 1 ;;
esac
STUB_VIEWCORE
chmod +x "$STUB"/*

run_check() {
  STATUS=0
  : >"$STUB_LOG"
  PATH="$STUB:$PATH" "$CHECK" "$WORK/out" >"$WORK/out.txt" 2>&1 || STATUS=$?
}

echo "core-walk-check:"

toolchain="$(awk '$1 == "toolchain" { print $2 }' "$REPO_ROOT/server/go.mod")"

run_check
if [ "$STATUS" -eq 0 ]; then
  pass "a core the reader walks back to its root passes"
else
  fail "a core the reader walks back to its root passes (status=$STATUS: $(cat "$WORK/out.txt"))"
fi
if grep -qF "toolchain=$toolchain" "$STUB_LOG"; then
  pass "the probe is built with the toolchain server/go.mod pins"
else
  fail "the probe must be built with $toolchain (calls=[$(cat "$STUB_LOG")])"
fi
for command in overview breakdown goroutines histogram objects reachable; do
  if grep -qx "viewcore $command" "$STUB_LOG"; then
    pass "the reader is asked for its $command"
  else
    fail "the reader is never asked for its $command"
  fi
done

STUB_UNREADABLE=1 run_check
if [ "$STATUS" -ne 0 ] && grep -qF 'could not read' "$WORK/out.txt"; then
  pass "a core the reader cannot open fails the check"
else
  fail "a core the reader cannot open fails the check (status=$STATUS: $(cat "$WORK/out.txt"))"
fi

# Reading the core is half of it. The walk back to a root is the half that
# reads the runtime's own heap structures, and it can fail on a core the
# overview reads without complaint.
STUB_NO_ROOT=1 run_check
if [ "$STATUS" -ne 0 ] && grep -qF 'main.root' "$WORK/out.txt"; then
  pass "a walk that does not reach the probe's root fails the check"
else
  fail "a walk that does not reach the probe's root fails the check (status=$STATUS: $(cat "$WORK/out.txt"))"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
