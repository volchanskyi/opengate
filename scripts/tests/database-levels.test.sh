#!/usr/bin/env bash
# Tests for scripts/database-levels.sh — how much of its own caps the database
# used through a run's measured phase.
#
# On the runner's compose stack the reading is the container's own accounting,
# read from its cgroup while the run is on; on staging it is the cluster's
# container readings over the phase's window. Either way the answer is a share
# of the database's own cap, and a reading that could not be taken is null —
# which a summary prints as "not read", never as 0.
#
# Run: ./scripts/tests/database-levels.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LEVELS="$REPO_ROOT/scripts/database-levels.sh"

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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "database-levels:"

# --- the container's own accounting --------------------------------------------
#
# A cgroup as the kernel lays it out: processor time used so far, the program's
# own memory, and the two caps.
CG="$WORK/cgroup"
mkdir -p "$CG"
printf 'usage_usec 5000000\nuser_usec 4000000\nsystem_usec 1000000\n' >"$CG/cpu.stat"
printf 'anon 104857600\nfile 900000000\n' >"$CG/memory.stat"
printf '100000 100000\n' >"$CG/cpu.max"
printf '1073741824\n' >"$CG/memory.max"

DB_LEVELS_SAMPLES=2 DB_LEVELS_INTERVAL=0 "$LEVELS" sample "$CG" "$WORK/samples.tsv"
assert_eq "the sampler writes one line a reading" "2" "$(grep -c . "$WORK/samples.tsv")"
if awk -F'\t' '$2 == 5000000 && $3 == 104857600 && $4 == 100000 && $5 == 100000 && $6 == 1073741824 { ok++ } END { exit !(ok == 2) }' "$WORK/samples.tsv"; then
  pass "each line carries the processor used, the program's memory and the two caps"
else
  fail "each line carries the processor used, the program's memory and the two caps (got=[$(cat "$WORK/samples.tsv")])"
fi

# Ten seconds of the measured phase, half a processor used against a cap of
# one, the program holding a tenth of its gigabyte and then three tenths.
t0="$(date -u -d '2026-09-29T13:45:35Z' +%s)"
{
  printf '%s\t1000000\t107374182\t100000\t100000\t1073741824\n' "$((t0 - 5))"
  printf '%s\t2000000\t107374182\t100000\t100000\t1073741824\n' "$t0"
  printf '%s\t7000000\t322122547\t100000\t100000\t1073741824\n' "$((t0 + 10))"
  printf '%s\t9000000\t999999999\t100000\t100000\t1073741824\n' "$((t0 + 20))"
} >"$WORK/run.tsv"
out="$("$LEVELS" average "$WORK/run.tsv" 2026-09-29T13:45:35Z 2026-09-29T13:45:45Z)"
assert_eq "processor is the share of its cap used across the window" "50" "$(jq -r '.cpu_percent' <<<"$out")"
assert_eq "memory is the program's own, averaged, against its cap" "20" "$(jq -r '.memory_percent | round' <<<"$out")"
assert_eq "and the caps are named" "1 processor 1 GiB" "$(jq -r '"\(.cpu_cap) \(.memory_cap)"' <<<"$out")"

# A window with fewer than two readings in it has no processor figure: a
# difference needs two ends. It is null, never nought.
out="$("$LEVELS" average "$WORK/run.tsv" 2026-09-29T13:45:40Z 2026-09-29T13:45:44Z)"
assert_eq "a window holding no reading is null throughout" "null null" "$(jq -r '"\(.cpu_percent) \(.memory_percent)"' <<<"$out")"
out="$("$LEVELS" average "$WORK/no-such.tsv" 2026-09-29T13:45:35Z 2026-09-29T13:45:45Z)"
assert_eq "no samples at all is null, not a failure" "null null" "$(jq -r '"\(.cpu_percent) \(.memory_percent)"' <<<"$out")"

# --- the cluster's container readings -------------------------------------------
mkdir -p "$WORK/bin"
cat >"$WORK/bin/kubectl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$KUBECTL_ARGS"
query="$(python3 -c 'import sys, urllib.parse; q = urllib.parse.parse_qs(urllib.parse.urlsplit(sys.argv[1]).query); print(q.get("query", [""])[0])' "${3:-}")"
value() { printf '{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[0,"%s"]}]}}\n' "$1"; }
case "$query" in
  *container_cpu_usage_seconds_total*) [ -n "${NO_CPU:-}" ] && echo '{"status":"success","data":{"result":[]}}' || value 150 ;;
  *container_spec_cpu_quota*) value 100000 ;;
  *container_spec_cpu_period*) value 100000 ;;
  *container_memory_rss*) value 80530636.8 ;;
  *container_spec_memory_limit_bytes*) value 402653184 ;;
  *) echo "unexpected query: $query" >&2; exit 1 ;;
esac
STUB
chmod +x "$WORK/bin/kubectl"
: >"$WORK/args"
out="$(PATH="$WORK/bin:$PATH" KUBECTL_ARGS="$WORK/args" "$LEVELS" cluster opengate-staging 2026-09-29T11:00:00Z 2026-09-29T11:05:00Z)"
assert_eq "processor used over the window against the database's cap" "50" "$(jq -r '.cpu_percent' <<<"$out")"
assert_eq "memory held against the database's limit" "20" "$(jq -r '.memory_percent | round' <<<"$out")"
assert_eq "and the caps are named" "1 processor 384 MiB" "$(jq -r '"\(.cpu_cap) \(.memory_cap)"' <<<"$out")"
if grep -qF 'namespace%3D%22opengate-staging%22' "$WORK/args" && grep -qF 'container%3D%22postgres%22' "$WORK/args"; then
  pass "the readings are the staging database container's own"
else
  fail "the readings are the staging database container's own (args=[$(cat "$WORK/args")])"
fi
out="$(NO_CPU=1 PATH="$WORK/bin:$PATH" KUBECTL_ARGS="$WORK/args" "$LEVELS" cluster opengate-staging 2026-09-29T11:00:00Z 2026-09-29T11:05:00Z)"
assert_eq "a reading the store does not hold is null" "null" "$(jq -r '.cpu_percent' <<<"$out")"

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
