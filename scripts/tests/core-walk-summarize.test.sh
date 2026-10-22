#!/usr/bin/env bash
# Tests scripts/core-walk-summarize.sh over a recorded check log and the check's refusals.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SUMMARIZE="$REPO_ROOT/scripts/core-walk-summarize.sh"
CHECK="$REPO_ROOT/scripts/core-walk-check.sh"
RECORDED="$SCRIPT_DIR/fixtures/run-logs/core-walk-pass.log"
[ -x "$SUMMARIZE" ] || {
  echo "FAIL: $SUMMARIZE not executable" >&2
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
contains() {
  if grep -qF -- "$2" <<<"$3"; then pass "$1"; else fail "$1 (missing [$2] in [$3])"; fi
}
assert_eq() {
  if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (want=[$2] got=[$3])"; fi
}
lacks() {
  if grep -qF -- "$2" <<<"$3"; then fail "$1 (found [$2] in [$3])"; else pass "$1"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
printf 'module example\n\ngo 1.27\n\ntoolchain go1.27.1\n' >"$WORK/go.mod"

PURPOSE="Checks our memory-leak tracer still reads the current Go version's memory dumps. The weekly long test uses it to find what holds leaked memory."
SECOND_PARAGRAPH=3

summary() {
  "$SUMMARIZE" "$1" "$WORK/go.mod"
}

echo "▶ a recorded passing run"
rc=0
out="$(summary "$RECORDED")" || rc=$?
assert_eq "a pass exits 0" 0 "$rc"
assert_eq "the purpose comes first" "$PURPOSE" "$(sed -n "${SECOND_PARAGRAPH}p" <<<"$out")"
contains "the result follows the purpose" '**Result:** pass' "$out"
contains "the Go version comes from go.mod" '| Go version | go1.27.1 |' "$out"
contains "the pinned reader walked" '| The pinned reader walked the dump | yes |' "$out"
contains "upstream cannot yet" '| The unpatched upstream reader walks it | not yet, so the patch stays |' "$out"

echo "▶ the upstream reader walks unpatched"
{
  head -n 1 "$RECORDED"
  echo '::error::the newest upstream reader walks a main.leaf back to main.wideRoot unpatched: upstream follows pointer maps built on demand now, so delete scripts/patches/viewcore-gcmask-on-demand.patch'
} >"$WORK/patch-can-go.log"
out="$(summary "$WORK/patch-can-go.log")"
contains "the patch can go" '| The unpatched upstream reader walks it | yes, so the patch can go |' "$out"
contains "the refusal is the result" '**Result:** FAIL: the newest upstream reader walks a main.leaf' "$out"

echo "▶ the pinned reader fails the walk"
echo '::error::the reader did not walk a main.node back to main.root on a core go1.27.1 wrote: ' >"$WORK/reader-failed.log"
out="$(summary "$WORK/reader-failed.log")"
contains "the pinned reader did not walk" '| The pinned reader walked the dump | no |' "$out"
contains "upstream was never asked" '| The unpatched upstream reader walks it | not reached |' "$out"
contains "the refusal names the type" '**Result:** FAIL: the reader did not walk a main.node' "$out"

echo "▶ the check stops before any walk"
echo '::error::gcore could not dump the probe (pid 4242); its log is /tmp/core-walk/gcore.log' >"$WORK/no-core.log"
out="$(summary "$WORK/no-core.log")"
contains "no walk was reached" '| The pinned reader walked the dump | not reached |' "$out"
contains "the earlier refusal is the result" '**Result:** FAIL: gcore could not dump the probe' "$out"
lacks "a refusal is never a pass" '**Result:** pass' "$out"

echo "▶ the check printed its first line and then stopped"
head -n 1 "$RECORDED" >"$WORK/cut-short.log"
out="$(summary "$WORK/cut-short.log")"
contains "a cut-short check says so" '**Result:** FAIL: the check stopped before it finished' "$out"

echo "▶ a run that produced nothing"
: >"$WORK/empty.log"
for log in "$WORK/empty.log" "$WORK/absent.log"; do
  rc=0
  out="$(summary "$log")" || rc=$?
  assert_eq "nothing to read still writes a summary ($(basename "$log"))" 0 "$rc"
  assert_eq "the purpose still comes first ($(basename "$log"))" "$PURPOSE" "$(sed -n "${SECOND_PARAGRAPH}p" <<<"$out")"
  contains "it says there was nothing to read ($(basename "$log"))" '**Result:** the run produced nothing to read' "$out"
done

echo "▶ every line the summary reads is one the check prints"
for phrase in 'the pinned reader walks a core' 'the newest upstream reader still cannot walk it' \
  'refuse "the reader did not walk' 'refuse "the newest upstream reader walks'; do
  contains "the check prints [$phrase]" "$phrase" "$(<"$CHECK")"
done

echo
echo "core-walk-summarize: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
