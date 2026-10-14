#!/usr/bin/env bash
# Tests scripts/fuzz-summarize.sh over recorded libFuzzer logs: a finished run and a crash.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SUMMARIZE="$REPO_ROOT/scripts/fuzz-summarize.sh"
LOGS="$SCRIPT_DIR/fixtures/run-logs"
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
assert_eq() {
  if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (want=[$2] got=[$3])"; fi
}
contains() {
  if grep -qF -- "$2" <<<"$3"; then pass "$1"; else fail "$1 (missing [$2] in [$3])"; fi
}
lacks() {
  if grep -qF -- "$2" <<<"$3"; then fail "$1 (found [$2] in [$3])"; else pass "$1"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

PURPOSE='Feeds the agent millions of generated, malformed messages to find one that crashes it.'
SECOND_PARAGRAPH=3

# A crate whose two targets sit among sections cargo-fuzz writes around them.
crate() {
  rm -rf "$WORK/crate"
  mkdir -p "$WORK/crate/logs"
  cat >"$WORK/crate/Cargo.toml" <<'TOML'
[package]
name = "example-fuzz"

[dependencies]
libfuzzer-sys = "0.4"

[[bin]]
name = "decode"
path = "fuzz_targets/decode.rs"
test = false

[[bin]]
name = "link_pieces"
path = "fuzz_targets/link_pieces.rs"
test = false

[workspace]
TOML
}

summary() {
  rc=0
  out="$("$SUMMARIZE" "$WORK/crate")" || rc=$?
  assert_eq "the summary exits 0 ($1)" 0 "$rc"
  assert_eq "the purpose comes first ($1)" "$PURPOSE" "$(sed -n "${SECOND_PARAGRAPH}p" <<<"$out")"
}

echo "▶ both targets finish"
crate
cp "$LOGS/fuzz-decode-done.log" "$WORK/crate/logs/decode.log"
cp "$LOGS/fuzz-decode-done.log" "$WORK/crate/logs/link_pieces.log"
summary "both finish"
contains "a clean night passes" '**Result:** pass: no target crashed.' "$out"
contains "the row carries runs, time, paths, inputs and crashes" '| decode | 5000000 | 104 s | 8726 | 4471 | none |' "$out"
contains "the second target has its own row" '| link_pieces | 5000000 | 104 s | 8726 | 4471 | none |' "$out"

echo "▶ the second target never ran"
rm "$WORK/crate/logs/link_pieces.log"
summary "one missing"
contains "a target with no log did not run" '| link_pieces | did not run | | | | |' "$out"
contains "a target that did not run fails the night" '**Result:** FAIL: link_pieces did not run.' "$out"

echo "▶ a target stopped before it finished"
grep -v '^Done ' "$LOGS/fuzz-decode-done.log" >"$WORK/crate/logs/link_pieces.log"
summary "cut short"
contains "a cut-short run reports its runs so far" '| link_pieces | 5000000, stopped | — | 8726 | 4471 | none |' "$out"
contains "a cut-short run fails the night" '**Result:** FAIL: link_pieces stopped before it finished.' "$out"

echo "▶ a target crashed"
cp "$LOGS/fuzz-crash.log" "$WORK/crate/logs/decode.log"
cp "$LOGS/fuzz-decode-done.log" "$WORK/crate/logs/link_pieces.log"
summary "crash"
crash_file="$(sed -n 's|.*Test unit written to .*/\(crash-[0-9a-f]*\)$|\1|p' "$LOGS/fuzz-crash.log")"
if [ -n "$crash_file" ]; then pass "the recorded crash names its input"; else fail "the recorded crash names its input"; fi
contains "the crash names its input" "| 1: $crash_file |" "$out"
contains "the crash fails the night and says where its input is" \
  "**Result:** FAIL: decode crashed; its input is artifacts/decode/$crash_file in the run's artifacts." "$out"
lacks "a crash is never a pass" '**Result:** pass' "$out"

echo "▶ a run that produced nothing"
crate
rm -rf "$WORK/crate/logs"
summary "no logs"
contains "it says there was nothing to read" '**Result:** the run produced nothing to read: no target wrote a log.' "$out"

echo "▶ the targets are the crate's own"
crate
cp "$REPO_ROOT/agent/fuzz/Cargo.toml" "$WORK/crate/Cargo.toml"
agent_targets="$(sed -n 's/^name = "\(.*\)"$/\1/p' "$REPO_ROOT/agent/fuzz/Cargo.toml" | grep -vx 'mesh-protocol-fuzz')"
while read -r target; do
  cp "$LOGS/fuzz-decode-done.log" "$WORK/crate/logs/$target.log"
done <<<"$agent_targets"
summary "the agent's crate"
targets=0
while read -r target; do
  targets=$((targets + 1))
  contains "the agent's target $target has a row" "| $target | 5000000 | 104 s |" "$out"
done <<<"$agent_targets"
if [ "$targets" -ge 2 ]; then pass "the agent's crate names its targets"; else fail "the agent's crate names $targets targets"; fi

echo
echo "fuzz-summarize: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
