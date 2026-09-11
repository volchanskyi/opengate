#!/usr/bin/env bash
# The reference walk, held to the one thing that makes it worth running.
#
# Everything it produces is a report, and every failure mode it has produces an
# empty one: a container that is not there, a binary whose debugging information
# was stripped by the release build, a core the debugger never wrote, a core
# viewcore cannot open. Each of those ends with a directory of short files and a
# step that exited zero, which is exactly the shape ci-cd-determinism exists to
# refuse — the work was refused, the run is green, and the only way anybody finds
# out is by opening the artifact months later.
#
# So the script is driven here against stub tools: one arrangement where the walk
# works, and one for each way it cannot. It has to write the path from a root on
# the first and fail loudly on every other.
#
# Run: ./scripts/tests/loadtest-reference-walk.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WALK="$REPO_ROOT/scripts/loadtest-reference-walk.sh"

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
# shellcheck disable=SC2329 # invoked by the EXIT trap below
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

echo "loadtest-reference-walk:"

# --- the stub tools -----------------------------------------------------------
#
# One directory of them per case, so a case can remove or break exactly one tool
# and leave the rest sound.

make_stubs() { # dir
  local dir="$1"
  mkdir -p "$dir"

  cat >"$dir/sudo" <<'STUB'
#!/usr/bin/env bash
exec "$@"
STUB

  cat >"$dir/docker" <<'STUB'
#!/usr/bin/env bash
case "$1" in
  inspect) echo "${STUB_CONTAINER_PID:-4242}" ;;
  cp)      printf 'a binary\n' >"$3" ;;
  *)       exit 1 ;;
esac
STUB

  cat >"$dir/readelf" <<'STUB'
#!/usr/bin/env bash
echo "There are 30 section headers:"
echo "  [26] .symtab           SYMTAB"
if [ "${STUB_STRIPPED:-0}" != "1" ]; then
  echo "  [28] .debug_info      PROGBITS"
fi
STUB

  cat >"$dir/gcore" <<'STUB'
#!/usr/bin/env bash
# gcore -o <prefix> <pid>
prefix="$2"
pid="$3"
[ "${STUB_GCORE_WRITES:-1}" = "1" ] && printf 'core bytes\n' >"$prefix.$pid"
STUB

  cat >"$dir/viewcore" <<'STUB'
#!/usr/bin/env bash
# viewcore <core> --exe <exe> <command> [args]
command=""
for arg in "$@"; do
  case "$arg" in
    overview | breakdown | goroutines | histogram | objects | reachable) command="$arg"; break ;;
  esac
done
case "$command" in
  overview)
    [ "${STUB_CORE_UNREADABLE:-0}" = "1" ] && { echo "cannot read core" >&2; exit 1; }
    echo "arch amd64"
    echo "runtime go1.26.7"
    ;;
  breakdown)  echo "all 402653184" ;;
  goroutines) echo "goroutine 1 running" ;;
  histogram)
    if [ "${STUB_EMPTY_HEAP:-0}" = "1" ]; then
      echo "count size bytes  type"
    else
      echo "count size bytes  type"
      echo " 7148   96 686208  github.com/volchanskyi/opengate/server/internal/relay.session"
      echo "   12   64    768  main.holder"
    fi
    ;;
  objects)
    echo "        c000102000 github.com/volchanskyi/opengate/server/internal/relay.session"
    echo "        c000204000 main.holder"
    ;;
  reachable)
    address="${!#}"
    [ "$address" = "c000204000" ] && { echo "panic: can't find a root that can reach the object"; exit 2; }
    echo "main.main"
    echo "opengate/server/internal/relay.(*Relay).Run.sessions →"
    echo " map[string]*session[0] → c000102000 relay.session"
    ;;
  *) exit 1 ;;
esac
STUB

  chmod +x "$dir"/*
}

# run_walk drives the script and reports its exit code without errexit ending
# the test at the first refusal — the status is taken on the failing command's
# own line, which is the form errexit does not fire on.
run_walk() { # stubdir out -> exit code, output in $LAST_OUTPUT
  local dir="$1" out="$2" rc=0
  LAST_OUTPUT="$(PATH="$dir:$PATH" "$WALK" opengate-perf-server /usr/local/bin/meshserver "$out" 2>&1)" || rc=$?
  return "$rc"
}

# --- the walk works -----------------------------------------------------------

STUBS="$WORK/good"
make_stubs "$STUBS"
OUT="$WORK/out-good"
if run_walk "$STUBS" "$OUT"; then
  pass "a live container with a debuggable binary produces a walk"
else
  fail "the walk refused an arrangement that has everything it needs: $LAST_OUTPUT"
fi

for report in overview.txt memory-breakdown.txt goroutines.txt type-histogram.txt reference-walk.txt; do
  if [ -s "$OUT/$report" ]; then
    pass "it wrote $report"
  else
    fail "it wrote no $report"
  fi
done

walked="$(cat "$OUT/reference-walk.txt" 2>/dev/null || true)"
if grep -qF -- 'relay.(*Relay).Run.sessions' <<<"$walked"; then
  pass "the walk names the root that holds the heaviest type"
else
  fail "the walk names no root, so it answers the question the heap profile already answered"
fi

# An object no root reaches is recorded as that, rather than as a blank entry
# that reads like a type nobody asked about.
if grep -qF -- 'no root reaches' <<<"$walked"; then
  pass "an unreachable object is reported as one"
else
  fail "an object no root reaches left a silent gap in the report"
fi

# The core is most of the target's address space and holds every row of the
# fixture. It is analysed where it is taken and never carried out.
if [ -z "$(find "$OUT" -name 'core.*' -print -quit)" ]; then
  pass "the core is removed once it has been read"
else
  fail "the core survived the walk, so the fixture's own data is in the artifact"
fi

if [ ! -e "$OUT/target-binary" ]; then
  pass "the copy of the target binary is removed with it"
else
  fail "the copied binary survived the walk and goes into the artifact"
fi

# --- every way it cannot work fails loudly ------------------------------------

refuses() { # description, env assignment..., then the check
  local description="$1"
  shift
  local dir="$WORK/stubs-$FAIL-$PASS"
  make_stubs "$dir"
  local out="$WORK/out-$FAIL-$PASS" rc=0
  LAST_OUTPUT="$(env "$@" PATH="$dir:$PATH" "$WALK" opengate-perf-server /usr/local/bin/meshserver "$out" 2>&1)" || rc=$?
  if [ "$rc" -ne 0 ]; then
    pass "$description"
  else
    fail "$description — it exited zero instead"
  fi
}

refuses "a container that is not running is refused" STUB_CONTAINER_PID=0
refuses "a stripped binary is refused" STUB_STRIPPED=1
refuses "a core the debugger never wrote is refused" STUB_GCORE_WRITES=0
refuses "a core viewcore cannot open is refused" STUB_CORE_UNREADABLE=1
refuses "a core with no live heap in it is refused" STUB_EMPTY_HEAP=1

# A stripped binary is the failure somebody will actually hit, so its refusal
# has to say what to change rather than only that something is wrong.
STRIPPED_STUBS="$WORK/stripped"
make_stubs "$STRIPPED_STUBS"
stripped_message="$(env STUB_STRIPPED=1 PATH="$STRIPPED_STUBS:$PATH" \
  "$WALK" opengate-perf-server /usr/local/bin/meshserver "$WORK/out-stripped" 2>&1 || true)"
if grep -qF -- 'GO_LDFLAGS' <<<"$stripped_message"; then
  pass "the refusal names the build setting that fixes it"
else
  fail "the refusal does not say how to get a debuggable target"
fi

# A tool that is not installed is the same class: the walk cannot happen, and
# saying so is the whole job.
NOVIEW="$WORK/no-viewcore"
make_stubs "$NOVIEW"
rm "$NOVIEW/viewcore"
noviewcore_rc=0
PATH="$NOVIEW:$PATH" "$WALK" opengate-perf-server /usr/local/bin/meshserver "$WORK/out-noviewcore" \
  >/dev/null 2>&1 || noviewcore_rc=$?
if [ "$noviewcore_rc" -ne 0 ]; then
  pass "a missing viewcore is refused rather than skipped"
else
  fail "it took a core that nothing could read and reported success"
fi

# --- the verdict --------------------------------------------------------------

echo
echo "loadtest-reference-walk: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
