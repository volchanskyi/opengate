#!/usr/bin/env bash
# Tests for scripts/benchmark-anchor.sh: the rounds it runs in turns against stand-ins for go and
# cargo, and the judgement it makes with the pinned benchstat over fixed sample files.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ANCHOR="$SCRIPT_DIR/../benchmark-anchor.sh"
[ -x "$ANCHOR" ] || {
  echo "FAIL: $ANCHOR not executable" >&2
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
  local name="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then pass "$name"; else fail "$name (want=[$want] got=[$got])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
export BENCHMARK_ANCHOR_SUMMARY="$WORK/anchor.md"

# The stand-in go prints what go test prints around its results: goos, goarch, pkg and cpu lines
# before them, and PASS and ok after; each tree's readings come from its own file.
mkdir -p "$WORK/bin"
cat >"$WORK/bin/go" <<'STUB'
#!/usr/bin/env bash
tree="$(cd .. && pwd)"
printf '%s\n' "go $(basename "$tree") $*" >>"$ROUNDS_LOG"
printf 'goos: linux\ngoarch: amd64\npkg: github.com/volchanskyi/opengate/server/internal/protocol\n'
printf 'cpu: AMD EPYC 7763 64-Core Processor\n'
while read -r name ns; do
  printf '%s-4 \t 1000000\t %s ns/op\t 64 B/op\t 2 allocs/op\n' "$name" "$ns"
done <"$tree/readings"
printf 'PASS\nok  \tgithub.com/volchanskyi/opengate/server/internal/protocol\t12.345s\n'
STUB
# The stand-in cargo writes what Criterion writes: each benchmark's estimates under its own folder
# of target/criterion, a group's benchmarks one folder deeper.
cat >"$WORK/bin/cargo" <<'STUB'
#!/usr/bin/env bash
tree="$(cd .. && pwd)"
printf '%s\n' "cargo $(basename "$tree") $*" >>"$ROUNDS_LOG"
while read -r name ns; do
  dir="target/criterion/$name/new"
  mkdir -p "$dir"
  printf '{"mean":{"point_estimate":%s},"median":{"point_estimate":%s}}\n' "$ns" "$ns" >"$dir/estimates.json"
  echo "$name    time:   [$ns ns $ns ns $ns ns]"
done <"$tree/readings"
STUB
chmod +x "$WORK/bin/go" "$WORK/bin/cargo"
export ROUNDS_LOG="$WORK/rounds.log"

# tree NAME LINE… makes a work tree whose suites read the lines given, one benchmark a line.
tree() {
  local name="$1"
  shift
  rm -rf "${WORK:?}/$name"
  mkdir -p "$WORK/$name/server" "$WORK/$name/agent"
  printf '%s\n' "$@" >"$WORK/$name/readings"
}

echo "benchmark-anchor rounds:"

tree anchor "BenchmarkCodec_ReadFrame 100" "BenchmarkRelayForward_Keystroke 2000"
tree tonight "BenchmarkCodec_ReadFrame 130" "BenchmarkRelayForward_Keystroke 2000"
: >"$ROUNDS_LOG"
rc=0
PATH="$WORK/bin:$PATH" "$ANCHOR" rounds go "$WORK/anchor" "$WORK/tonight" "$WORK/out-go" >"$WORK/log" 2>&1 || rc=$?
assert_eq "the Go rounds complete" "0" "$rc"
assert_eq "each side's suite runs ten times" "10 10" \
  "$(grep -c '^go anchor ' "$ROUNDS_LOG") $(grep -c '^go tonight ' "$ROUNDS_LOG")"
assert_eq "the two sides take turns, the anchor first" "anchor tonight anchor tonight" \
  "$(head -4 "$ROUNDS_LOG" | cut -d' ' -f2 | paste -sd' ' -)"
assert_eq "every run is the nightly's whole suite at four processors" \
  "test -bench=. -benchmem -count=1 -cpu 4 -run=^\$ ./internal/..." \
  "$(sed -n 1p "$ROUNDS_LOG" | cut -d' ' -f3-)"
assert_eq "each side keeps ten readings of each benchmark" "10 10" \
  "$(grep -c '^BenchmarkCodec_ReadFrame ' "$WORK/out-go/anchor.txt") $(grep -c '^BenchmarkCodec_ReadFrame ' "$WORK/out-go/tonight.txt")"
if grep -qE '^(goos|pkg|cpu|PASS|ok)|-4 ' "$WORK/out-go/anchor.txt" "$WORK/out-go/tonight.txt"; then
  fail "only result lines, without the processor suffix, reach the comparison"
else
  pass "only result lines, without the processor suffix, reach the comparison"
fi
assert_eq "tonight's first run is kept whole for the trend and the allocation check" "1" \
  "$(grep -c '^cpu: ' "$WORK/out-go/tonight-first.txt")"

tree anchor "frame_decode_control 583" "edge_sentinel_sampler/edge_sentinel_sysinfo_sample 900"
tree tonight "frame_decode_control 590" "edge_sentinel_sampler/edge_sentinel_sysinfo_sample 905"
mkdir -p "$WORK/tonight/agent/target/criterion/removed_last_week/new"
printf '{"mean":{"point_estimate":1}}\n' >"$WORK/tonight/agent/target/criterion/removed_last_week/new/estimates.json"
: >"$ROUNDS_LOG"
rc=0
PATH="$WORK/bin:$PATH" "$ANCHOR" rounds rust "$WORK/anchor" "$WORK/tonight" "$WORK/out-rust" >"$WORK/log" 2>&1 || rc=$?
assert_eq "the Rust rounds complete" "0" "$rc"
assert_eq "each side's suites run ten times" "10 10" \
  "$(grep -c '^cargo anchor ' "$ROUNDS_LOG") $(grep -c '^cargo tonight ' "$ROUNDS_LOG")"
assert_eq "every run is the nightly's three suites" \
  "bench -p mesh-protocol -p mesh-agent-core -p edge-tsdb --bench *" \
  "$(sed -n 1p "$ROUNDS_LOG" | cut -d' ' -f3-)"
assert_eq "each round's estimate becomes a reading under the benchmark's own name" \
  "Benchmarkedge_sentinel_sysinfo_sample 1 905 ns/op" \
  "$(grep -m1 'sysinfo' "$WORK/out-rust/tonight.txt")"
assert_eq "ten readings a side" "10 10" \
  "$(grep -c '^Benchmarkframe_decode_control ' "$WORK/out-rust/anchor.txt") $(grep -c '^Benchmarkframe_decode_control ' "$WORK/out-rust/tonight.txt")"
if grep -q 'removed_last_week' "$WORK/out-rust/tonight.txt"; then
  fail "an estimate left from an earlier build is never read as tonight's"
else
  pass "an estimate left from an earlier build is never read as tonight's"
fi

echo "benchmark-anchor judge:"

# sides NAME ANCHOR-READINGS -- TONIGHT-READINGS writes one benchmark's ten readings a side.
sides() {
  local name="$1" v side=anchor
  shift
  for v in "$@"; do
    if [ "$v" = "--" ]; then
      side=tonight
      continue
    fi
    printf '%s 1 %s ns/op\n' "$name" "$v" >>"$WORK/cmp/$side.txt"
  done
}
reset() {
  rm -rf "${WORK:?}/cmp"
  mkdir -p "$WORK/cmp"
  : >"$WORK/cmp/anchor.txt"
  : >"$WORK/cmp/tonight.txt"
  : >"$BENCHMARK_ANCHOR_SUMMARY"
}
# baseline LANG NAME… names the benchmarks the baseline holds.
baseline() {
  local lang="$1"
  shift
  printf '%s\n' "$@" | jq -Rn --arg lang "$lang" \
    '{version: 1, anchor: "a1", benchmarks: [inputs | {name: ., lang: $lang}]}' >"$WORK/baseline.json"
}
judge() {
  "$ANCHOR" judge "$1" "$WORK/cmp" "$WORK/baseline.json" "${2:-a1}" "${3:-t2}"
}
STEADY=(100 101 99 100 102 98 100 101 99 100)

reset
baseline go BenchmarkSlower
sides BenchmarkSlower "${STEADY[@]}" -- 129 131 130 132 128 130 131 129 130 130
rc=0
judge go >"$WORK/out" 2>&1 || rc=$?
assert_eq "a consistent 30% slowdown against the anchor fails" "1" "$rc"
if grep -q '^REGRESSION_ALERT:.*go/BenchmarkSlower' "$WORK/out"; then
  pass "the failure names the language and the benchmark"
else
  fail "the failure names the language and the benchmark (got: $(cat "$WORK/out"))"
fi

reset
baseline go BenchmarkSmall
sides BenchmarkSmall "${STEADY[@]}" -- 105 106 104 105 107 103 105 106 104 105
rc=0
judge go >"$WORK/out" 2>&1 || rc=$?
assert_eq "a significant slowdown of 5% stays inside the 10% limit" "0" "$rc"

reset
baseline go BenchmarkPast
sides BenchmarkPast "${STEADY[@]}" -- 115 116 114 115 117 113 115 116 114 115
rc=0
judge go >"$WORK/out" 2>&1 || rc=$?
assert_eq "a significant slowdown of 15% is past the limit and fails" "1" "$rc"

reset
baseline go BenchmarkSteady
sides BenchmarkSteady "${STEADY[@]}" -- 99 101 100 102 98 100 101 99 100 300
rc=0
judge go >"$WORK/out" 2>&1 || rc=$?
assert_eq "one outlier among ten passes" "0" "$rc"
if grep -qF '~' "$BENCHMARK_ANCHOR_SUMMARY"; then
  pass "the summary quotes benchstat's ~"
else
  fail "the summary quotes benchstat's ~ (got: $(cat "$BENCHMARK_ANCHOR_SUMMARY"))"
fi

reset
baseline go BenchmarkFaster
sides BenchmarkFaster "${STEADY[@]}" -- 70 71 69 70 72 68 70 71 69 70
rc=0
judge go >"$WORK/out" 2>&1 || rc=$?
assert_eq "a speed-up passes" "0" "$rc"

reset
baseline go BenchmarkKept BenchmarkGone
sides BenchmarkKept "${STEADY[@]}" -- "${STEADY[@]}"
sides BenchmarkGone "${STEADY[@]}"
rc=0
judge go >"$WORK/out" 2>&1 || rc=$?
assert_eq "a benchmark the baseline names that tonight did not run fails" "1" "$rc"
if grep -q '^REGRESSION_ALERT:.*go/BenchmarkGone.*missing' "$WORK/out"; then
  pass "the failure says the named benchmark is missing"
else
  fail "the failure says the named benchmark is missing (got: $(cat "$WORK/out"))"
fi

reset
baseline go BenchmarkKept BenchmarkNew
sides BenchmarkKept "${STEADY[@]}" -- "${STEADY[@]}"
sides BenchmarkNew -- 300 301 299 300 302 298 300 301 299 300
rc=0
judge go >"$WORK/out" 2>&1 || rc=$?
assert_eq "a benchmark absent at the anchor is reported, not judged" "0" "$rc"
new_row="$(grep -F 'go/BenchmarkNew' "$BENCHMARK_ANCHOR_SUMMARY" || true)"
if grep -qF 'until the anchor moves' <<<"$new_row"; then
  pass "the summary says it waits for the anchor to move"
else
  fail "the summary says it waits for the anchor to move (got: $(cat "$BENCHMARK_ANCHOR_SUMMARY"))"
fi

reset
baseline go BenchmarkSlower
sides BenchmarkSlower "${STEADY[@]}" -- 129 131 130 132 128 130 131 129 130 130
rc=0
judge go a1 a1 >"$WORK/out" 2>&1 || rc=$?
assert_eq "the same commit on both sides is recorded, not judged" "0" "$rc"
if grep -qF 'same commit' "$BENCHMARK_ANCHOR_SUMMARY"; then
  pass "the summary says both sides ran one commit"
else
  fail "the summary says both sides ran one commit (got: $(cat "$BENCHMARK_ANCHOR_SUMMARY"))"
fi

reset
baseline rust frame_decode_control
sides Benchmarkframe_decode_control "${STEADY[@]}" -- 129 131 130 132 128 130 131 129 130 130
rc=0
judge rust >"$WORK/out" 2>&1 || rc=$?
assert_eq "a Rust slowdown is judged the same way" "1" "$rc"
if grep -q '^REGRESSION_ALERT:.*rust/frame_decode_control' "$WORK/out"; then
  pass "under the name the baseline gives it"
else
  fail "under the name the baseline gives it (got: $(cat "$WORK/out"))"
fi

reset
baseline go BenchmarkSteady
sides BenchmarkSteady "${STEADY[@]}" -- "${STEADY[@]}"
rm "$WORK/cmp/tonight.txt"
rc=0
judge go >"$WORK/out" 2>&1 || rc=$?
assert_eq "a side with no readings at all exits 2" "2" "$rc"

# A benchstat that cannot say its version stands in for one at another pin.
mkdir -p "$WORK/stale"
printf '#!/usr/bin/env bash\nexit 0\n' >"$WORK/stale/benchstat"
chmod +x "$WORK/stale/benchstat"
reset
baseline go BenchmarkSteady
sides BenchmarkSteady "${STEADY[@]}" -- "${STEADY[@]}"
rc=0
PATH="$WORK/stale:$PATH" judge go >"$WORK/out" 2>&1 || rc=$?
if [ "$rc" -eq 2 ] && grep -qF 'install-benchstat.sh' "$WORK/out"; then
  pass "a benchstat at another version exits 2 with the install command"
else
  fail "a benchstat at another version exits 2 with the install command (rc=$rc: $(cat "$WORK/out"))"
fi

echo "benchmark-anchor anchor:"

baseline go BenchmarkSteady
got="$(env -u BENCHMARK_ANCHOR "$ANCHOR" anchor "$WORK/baseline.json" 2>&1)" || true
assert_eq "the night compares with the anchor the baseline names" "a1" "$got"

COMMIT=23e44072779ae33c988d1ecae1f936210ccfcad7
got="$(BENCHMARK_ANCHOR="$COMMIT" "$ANCHOR" anchor "$WORK/baseline.json" 2>&1)" || true
assert_eq "a run by hand compares with the commit it is given" "$COMMIT" "$got"

for value in main --orphan "$COMMIT;id" 123456; do
  rc=0
  BENCHMARK_ANCHOR="$value" "$ANCHOR" anchor "$WORK/baseline.json" >"$WORK/out" 2>&1 || rc=$?
  assert_eq "a given anchor that is no commit hash exits 2 ($value)" "2" "$rc"
done

jq -n '{version: 1, benchmarks: []}' >"$WORK/no-anchor.json"
rc=0
env -u BENCHMARK_ANCHOR "$ANCHOR" anchor "$WORK/no-anchor.json" >"$WORK/out" 2>&1 || rc=$?
if [ "$rc" -eq 2 ] && grep -qF 'names no anchor' "$WORK/out"; then
  pass "a baseline naming no anchor exits 2 and says so"
else
  fail "a baseline naming no anchor exits 2 and says so (rc=$rc: $(cat "$WORK/out"))"
fi

# The workflow is read as structure: every step that picks the anchor asks the script, and hands
# it the dispatch input through its own env.
WORKFLOW="$SCRIPT_DIR/../../.github/workflows/benchmark.yml"
got="$(
  python3 - "$WORKFLOW" <<'PY'
import sys, yaml
doc = yaml.safe_load(open(sys.argv[1]))
dispatch = (doc.get(True) or doc.get("on"))["workflow_dispatch"]["inputs"]
anchor = dispatch.get("anchor") or {}
print("input", anchor.get("type"), anchor.get("required"))
for job in doc["jobs"].values():
    for step in job.get("steps", []):
        run = step.get("run", "")
        if ".anchor" not in run and "benchmark-anchor.sh anchor" not in run:
            continue
        env = (step.get("env") or {}).get("BENCHMARK_ANCHOR")
        asks = "benchmark-anchor.sh anchor benchmarks/baseline.json" in run
        print("step", step.get("name"), asks, env)
PY
)"
assert_eq "the run by hand takes an optional anchor and both steps ask the script with it" \
  "input string False
step Check out the anchor beside tonight's commit True \${{ inputs.anchor }}
step Judge both languages against the anchor True \${{ inputs.anchor }}" "$got"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
