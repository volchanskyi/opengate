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
# The source it was asked to build is kept, so a test can read what the probe
# holds.
printf 'go %s toolchain=%s\n' "$*" "${GOTOOLCHAIN:-}" >>"$STUB_LOG"
[ "$1" = build ] || exit 1
cp main.go "$STUB_PROBE_SOURCE"
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
# One reader script, two installs: the patched one on PATH and the newest
# upstream one the check is handed. STUB_READER names which is answering, so a
# case can break one without the other.
cat >"$WORK/reader" <<'STUB_VIEWCORE'
#!/usr/bin/env bash
reader="$STUB_READER"
command="" address=""
for arg in "$@"; do
  if [ "$command" = reachable ]; then address="$arg"; break; fi
  case "$arg" in
    overview | breakdown | goroutines | histogram | objects | reachable) command="$arg" ;;
  esac
done
printf 'viewcore(%s) %s\n' "$reader" "$command" >>"$STUB_LOG"
# The failure the newest upstream reader has on a core holding a type whose
# pointer map the runtime built on demand: it reads the map's slot as the map.
if [ "$reader" = upstream ] && [ "${STUB_UPSTREAM_READS:-0}" != "1" ]; then
  echo "panic: address 4d2000 is not mapped in the core file" >&2
  exit 2
fi
case "$command" in
  overview)
    [ "${STUB_UNREADABLE:-0}" = "1" ] && { echo "unrecognized runtime version" >&2; exit 1; }
    echo "runtime go1.27.1"
    ;;
  breakdown)  echo "all 4194304" ;;
  goroutines) echo "goroutine 1 sleeping" ;;
  histogram)
    echo "count size bytes  type"
    echo "    1 8388608 8388608  main.wide"
    echo "    1 1048576 1048576  main.leaf"
    echo "  512 80 40960  main.node"
    ;;
  objects)
    echo "        c000400000 main.wide"
    echo "        c001000000 main.leaf"
    echo "        c000180000 main.node"
    ;;
  reachable)
    case "$address" in
      c000180000)
        if [ "${STUB_NO_ROOT:-0}" = "1" ]; then
          echo "panic: can't find a root that can reach the object"
          exit 2
        fi
        echo "main.root → c000180000 main.node"
        ;;
      c001000000)
        # Reached only through the last slot of main.wide, whose pointer map is
        # the one built on demand. A reader that misreads it loses the edge.
        if [ "${STUB_NO_WIDE_EDGE:-0}" = "1" ]; then
          echo "panic: can't find a root that can reach the object"
          exit 2
        fi
        echo "main.wideRoot → c000400000 main.wide .slots[1048575] → c001000000 main.leaf"
        ;;
      *) echo "main.wideRoot → c000400000 main.wide" ;;
    esac
    ;;
  *) exit 1 ;;
esac
STUB_VIEWCORE
printf '#!/usr/bin/env bash\nSTUB_READER=patched exec "%s" "$@"\n' "$WORK/reader" >"$STUB/viewcore"
printf '#!/usr/bin/env bash\nSTUB_READER=upstream exec "%s" "$@"\n' "$WORK/reader" >"$WORK/viewcore-upstream"
chmod +x "$STUB"/* "$WORK/reader" "$WORK/viewcore-upstream"
export STUB_PROBE_SOURCE="$WORK/probe-main.go"

run_check() {
  STATUS=0
  : >"$STUB_LOG"
  rm -rf "$WORK/out"
  PATH="$STUB:$PATH" VIEWCORE_UPSTREAM="$WORK/viewcore-upstream" \
    "$CHECK" "$WORK/out" >"$WORK/out.txt" 2>&1 || STATUS=$?
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
  if grep -qx "viewcore(patched) $command" "$STUB_LOG"; then
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

# --- a type whose pointer map the runtime builds on demand ---------------------
#
# Go describes the pointers of a type with more than 128 pointer words through
# one more indirection, built by the runtime the first time it needs it. The
# reader the soak pinned read the slot as the map and walked off the end of the
# binary's memory on the first dump that held one; the probe held nothing that
# large, so this check passed while every soak failed.
if grep -qE '\[1 << 20\]\*leaf' "$STUB_PROBE_SOURCE" && grep -qF 'runtime.GC()' "$STUB_PROBE_SOURCE"; then
  pass "the probe holds a type whose pointer map is built on demand, and has it built"
else
  fail "the probe holds a type whose pointer map is built on demand, and has it built (source=[$(cat "$STUB_PROBE_SOURCE" 2>/dev/null)])"
fi

STUB_NO_WIDE_EDGE=1 run_check
if [ "$STATUS" -ne 0 ] && grep -qF 'main.wideRoot' "$WORK/out.txt"; then
  pass "a walk that loses the edge only that pointer map describes fails the check"
else
  fail "a walk that loses the edge only that pointer map describes fails the check (status=$STATUS: $(cat "$WORK/out.txt"))"
fi

# --- the patch retires itself --------------------------------------------------
#
# The same core goes through the newest upstream reader, unpatched. While it
# still fails, the patch is still earning its place and the check says so.
run_check
if [ "$STATUS" -eq 0 ] && grep -qx "viewcore(upstream) overview" "$STUB_LOG" \
  && grep -qi 'patch is still needed' "$WORK/out.txt"; then
  pass "an upstream reader that still cannot walk the core keeps the patch"
else
  fail "an upstream reader that still cannot walk the core keeps the patch (status=$STATUS: $(cat "$WORK/out.txt"))"
fi

# The night upstream reads it unpatched, the job goes red and says what to swap.
STUB_UPSTREAM_READS=1 run_check
if [ "$STATUS" -ne 0 ] && grep -qF 'viewcore-gcmask-on-demand.patch' "$WORK/out.txt"; then
  pass "an upstream reader that walks the core fails the check and names the patch to drop"
else
  fail "an upstream reader that walks the core fails the check and names the patch to drop (status=$STATUS: $(cat "$WORK/out.txt"))"
fi

# Without the upstream reader there is no way to know whether the patch is
# still needed, so the check refuses to run rather than passing on half of it.
STATUS=0
PATH="$STUB:$PATH" "$CHECK" "$WORK/out" >"$WORK/out.txt" 2>&1 || STATUS=$?
if [ "$STATUS" -ne 0 ] && grep -qF 'VIEWCORE_UPSTREAM' "$WORK/out.txt"; then
  pass "a check handed no upstream reader refuses"
else
  fail "a check handed no upstream reader refuses (status=$STATUS: $(cat "$WORK/out.txt"))"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
