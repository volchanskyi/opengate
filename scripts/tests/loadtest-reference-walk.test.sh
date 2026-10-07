#!/usr/bin/env bash
# The reference walk writes its reports against stub tools and fails loudly on every missing input.
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

for tool in age age-keygen zstd; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "FAIL: $tool is not installed; run scripts/install-dump-tools.sh" >&2
    exit 1
  }
done
age-keygen -o "$WORK/test.key" 2>/dev/null
RECIPIENT="$(age-keygen -y "$WORK/test.key")"
export SOAK_DUMP_AGE_RECIPIENT="$RECIPIENT"
export RUNNER_TEMP="$WORK/runner-temp"
mkdir -p "$RUNNER_TEMP"

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
[ -n "${STUB_GCORE_LOG:-}" ] && printf 'gcore %s\n' "$*" >>"$STUB_GCORE_LOG"
[ "${STUB_GCORE_WRITES:-1}" = "1" ] && printf 'core bytes' >"$prefix.$pid"
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

run_walk() { # stubdir out [dump] -> exit code, output in $LAST_OUTPUT
  local dir="$1" out="$2" dump="${3:-$2-dump}" rc=0
  LAST_OUTPUT="$(PATH="$dir:$PATH" "$WALK" opengate-perf-server /usr/local/bin/meshserver "$out" "$dump" 2>&1)" || rc=$?
  return "$rc"
}

plain_cores() {
  find "$@" -name 'core.*' ! -name '*.age' -print 2>/dev/null || true
}

encrypted_dumps() {
  find "$1" -name 'core.*.zst.age' -print 2>/dev/null || true
}

opens() {
  age -d -i "$WORK/test.key" "$1" | zstd -d -q
}

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

if grep -qF -- 'no root reaches' <<<"$walked"; then
  pass "an unreachable object is reported as one"
else
  fail "an object no root reaches left a silent gap in the report"
fi

if [ -z "$(plain_cores "$OUT" "$RUNNER_TEMP")" ]; then
  pass "no plain core is left under the bundle or where it was taken"
else
  fail "a plain core survived the walk: $(plain_cores "$OUT" "$RUNNER_TEMP")"
fi

dump="$(encrypted_dumps "$OUT-dump")"
if [ "$(grep -c . <<<"$dump")" = "1" ] && [ "$(opens "$dump")" = "core bytes" ]; then
  pass "the dump is kept encrypted, and the maintainer's key and zstd open it back"
else
  fail "the dump is kept encrypted, and the maintainer's key and zstd open it back (dumps=[$dump])"
fi
if [ -z "$(encrypted_dumps "$OUT")" ]; then
  pass "the encrypted dump is kept apart from the bundle"
else
  fail "the encrypted dump was written into the bundle"
fi

if [ -s "$OUT/target-binary" ]; then
  pass "the copy of the target binary travels with the reports"
else
  fail "the copy of the target binary is gone, so the dump cannot be read"
fi

STUBS="$WORK/unreadable"
make_stubs "$STUBS"
OUT="$WORK/out-unreadable"
if STUB_CORE_UNREADABLE=1 run_walk "$STUBS" "$OUT"; then
  fail "a core the reader cannot open fails the walk"
else
  pass "a core the reader cannot open fails the walk"
fi
if [ -z "$(plain_cores "$OUT" "$RUNNER_TEMP")" ]; then
  pass "and leaves no plain core behind"
else
  fail "and leaves no plain core behind: $(plain_cores "$OUT" "$RUNNER_TEMP")"
fi
dump="$(encrypted_dumps "$OUT-dump")"
if [ -n "$dump" ] && [ "$(opens "$dump")" = "core bytes" ]; then
  pass "and the encrypted dump is still kept"
else
  fail "and the encrypted dump is still kept (dumps=[$dump])"
fi
if [ -s "$OUT/target-binary" ]; then
  pass "and the program copy is still there to read it with"
else
  fail "and the program copy is still there to read it with"
fi

STUBS="$WORK/no-encryption"
make_stubs "$STUBS"
cat >"$STUBS/age" <<'STUB'
#!/usr/bin/env bash
cat >/dev/null
echo "age: error: failed to write the header" >&2
exit 1
STUB
chmod +x "$STUBS/age"
OUT="$WORK/out-no-encryption"
if run_walk "$STUBS" "$OUT"; then
  fail "a dump that could not be encrypted fails the walk"
elif grep -qi 'encrypt' <<<"$LAST_OUTPUT"; then
  pass "a dump that could not be encrypted fails the walk, and says so"
else
  fail "a dump that could not be encrypted fails the walk, and says so (got=[$LAST_OUTPUT])"
fi
if [ -z "$(plain_cores "$OUT" "$OUT-dump" "$RUNNER_TEMP")" ]; then
  pass "and leaves no plain core anywhere"
else
  fail "and leaves no plain core anywhere: $(plain_cores "$OUT" "$OUT-dump" "$RUNNER_TEMP")"
fi
if [ -s "$OUT/target-binary" ]; then
  pass "and the program copy is still there"
else
  fail "and the program copy is still there"
fi

# A refusal prints none of the value, since one refused shape is a private key.
PRIVATE_KEY="$(grep '^AGE-SECRET-KEY-' "$WORK/test.key")"
recipient_refused() { # description, value
  local description="$1" value="$2" dir out rc=0
  dir="$WORK/recipient-$PASS-$FAIL"
  out="$WORK/out-recipient-$PASS-$FAIL"
  make_stubs "$dir"
  LAST_OUTPUT="$(SOAK_DUMP_AGE_RECIPIENT="$value" PATH="$dir:$PATH" STUB_GCORE_LOG="$out.gcore" \
    "$WALK" opengate-perf-server /usr/local/bin/meshserver "$out" "$out-dump" 2>&1)" || rc=$?
  if [ "$rc" -eq 0 ]; then
    fail "$description — it exited zero"
  elif [ -e "$out.gcore" ]; then
    fail "$description — a core was taken first"
  elif [ -n "$value" ] && grep -qF -- "$value" <<<"$LAST_OUTPUT"; then
    fail "$description — the refusal printed the value"
  else
    pass "$description"
  fi
}
recipient_refused "a missing recipient is refused" ""
recipient_refused "an SSH key is refused as a recipient" \
  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl ivan@laptop"
recipient_refused "a private key is refused as a recipient, without being printed" "$PRIVATE_KEY"
recipient_refused "a value that is not a native age recipient is refused" "age1notarecipient"

if SOAK_DUMP_AGE_RECIPIENT="$RECIPIENT" "$WALK" --check-recipient >/dev/null 2>&1; then
  pass "a native age recipient passes the early check"
else
  fail "a native age recipient passes the early check"
fi
if SOAK_DUMP_AGE_RECIPIENT="$PRIVATE_KEY" "$WALK" --check-recipient >/dev/null 2>&1; then
  fail "a private key fails the early check"
else
  pass "a private key fails the early check"
fi

refuses() { # description, env assignment..., then the check
  local description="$1"
  shift
  local dir="$WORK/stubs-$FAIL-$PASS"
  make_stubs "$dir"
  local out="$WORK/out-$FAIL-$PASS" rc=0
  LAST_OUTPUT="$(env "$@" PATH="$dir:$PATH" "$WALK" opengate-perf-server /usr/local/bin/meshserver "$out" "$out-dump" 2>&1)" || rc=$?
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

STRIPPED_STUBS="$WORK/stripped"
make_stubs "$STRIPPED_STUBS"
stripped_message="$(env STUB_STRIPPED=1 PATH="$STRIPPED_STUBS:$PATH" \
  "$WALK" opengate-perf-server /usr/local/bin/meshserver "$WORK/out-stripped" "$WORK/dump-stripped" 2>&1 || true)"
if grep -qF -- 'GO_LDFLAGS' <<<"$stripped_message"; then
  pass "the refusal names the build setting that fixes it"
else
  fail "the refusal does not say how to get a debuggable target"
fi

NOVIEW="$WORK/no-viewcore"
make_stubs "$NOVIEW"
rm "$NOVIEW/viewcore"
noviewcore_rc=0
PATH="$NOVIEW:$PATH" "$WALK" opengate-perf-server /usr/local/bin/meshserver "$WORK/out-noviewcore" \
  "$WORK/dump-noviewcore" >/dev/null 2>&1 || noviewcore_rc=$?
if [ "$noviewcore_rc" -ne 0 ]; then
  pass "a missing viewcore is refused rather than skipped"
else
  fail "it took a core that nothing could read and reported success"
fi

echo
echo "loadtest-reference-walk: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
