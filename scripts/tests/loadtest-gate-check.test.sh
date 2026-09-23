#!/usr/bin/env bash
# Tests for scripts/loadtest-gate-check.sh — the profile's own numbers deciding
# whether a night was acceptable.
#
# Every profile declares limits, keyed by the same source/scenario/phase triple
# the canonical rows carry. Nothing read them: the schema checked they were
# well-formed and then no code consumed one, so every limit in all seven
# profiles was decoration, including the ones marked as failing the run.
#
# They are read here, in the step where both halves of the night have been
# joined — the browser-side rows and the machine-side rows in one file. Inside
# the harness they could not be: it holds phases and machines and no
# browser-side row at all, so a limit naming one could never have been reached
# from there.
#
# A limit on a measurement that never arrives is the case worth pinning. It
# reads as a passing limit forever, which is the same false green a step that
# reports success for refused work produces.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECK="$REPO_ROOT/scripts/loadtest-gate-check.sh"
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
assert_eq() {
  local name="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then pass "$name"; else fail "$name (want=[$want] got=[$got])"; fi
}
assert_contains() {
  local name="$1" needle="$2" hay="$3"
  case "$hay" in
    *"$needle"*) pass "$name" ;;
    *) fail "$name (missing [$needle] in [$hay])" ;;
  esac
}

assert_lacks() {
  local name="$1" needle="$2" hay="$3"
  case "$hay" in
    *"$needle"*) fail "$name (found [$needle] in [$hay])" ;;
    *) pass "$name" ;;
  esac
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# A profile carrying one limit that fails the night and one mark that only
# reports, on the same measurement — which is the arrangement the two files of
# numbers were reconciled into.
write_profile() {
  cat >"$WORK/profile.yaml" <<'YAML'
schema_version: 1
name: normal
family: normal
environment: staging
fixture: small
phases:
  - name: steady
    duration: 2m
    connected_agents: 500
safety:
  max_node_cpu_percent: 85
  max_node_memory_percent: 90
  max_error_rate: 0.01
gates:
  - series: k6/api-baseline/http
    metric: latency_p95_ms
    max: 200
    blocking: true
  - series: k6/api-baseline/http
    metric: latency_p95_ms
    max: 100
    blocking: false
  - series: quic/quic-agents/aggregate
    metric: rps
    min: 10
    blocking: true
YAML
}

# Canonical rows in the shape the summarizer writes them.
write_rows() {
  local api_p95="$1" quic_rps="$2"
  jq -n --argjson p95 "$api_p95" --argjson rps "$quic_rps" '[
    { source: "k6", scenario: "api-baseline", phase: "http",
      latency_p95_ms: $p95, error_rate: 0 },
    { source: "quic", scenario: "quic-agents", phase: "aggregate",
      rps: $rps, error_rate: 0 }
  ]' >"$WORK/summary.json"
}

run_check() {
  STATUS=0
  "$CHECK" "$WORK/profile.yaml" "$WORK/summary.json" "$WORK/breaches.json" \
    >"$WORK/out.txt" 2>"$WORK/err.txt" || STATUS=$?
  OUT="$(cat "$WORK/out.txt" "$WORK/err.txt")"
}

echo "loadtest-gate-check:"

# A night inside every limit.
write_profile
write_rows 80 25
run_check
assert_eq "a night inside every limit passes" "0" "$STATUS"
assert_eq "and records no breach" "0" "$(jq 'length' "$WORK/breaches.json")"

# Past the mark that only reports, inside the one that fails. The mark is
# reported so the target it represents is being watched; the night still passes.
write_rows 150 25
run_check
assert_eq "past a reporting mark alone, the night passes" "0" "$STATUS"
assert_eq "and the mark is recorded" "1" "$(jq 'length' "$WORK/breaches.json")"
assert_contains "the mark says it only reports" "reported" "$OUT"

# Past the limit that fails the night. Both it and the mark below it are past,
# so both are recorded and the night fails.
write_rows 260 25
run_check
assert_eq "past an enforced limit, the night fails" "1" "$STATUS"
assert_eq "both the limit and the mark are recorded" "2" "$(jq 'length' "$WORK/breaches.json")"
assert_contains "the breach names the measurement" "k6/api-baseline/http" "$OUT"
assert_contains "the breach names the number it passed" "200" "$OUT"

# A floor, not a ceiling: throughput below what the profile asked for.
write_rows 80 4
run_check
assert_eq "below a floor, the night fails" "1" "$STATUS"
assert_contains "the breach names the floor" "quic/quic-agents/aggregate" "$OUT"

# The case this whole check exists for. A limit on a measurement that never
# arrives reads as a passing limit forever — the same false green as a step
# that reports success for work it was refused.
jq -n '[
  { source: "quic", scenario: "quic-agents", phase: "aggregate", rps: 25, error_rate: 0 }
]' >"$WORK/summary.json"
run_check
assert_eq "a limit whose measurement never arrived fails the night" "1" "$STATUS"
# The sentence a reader is left with has to say which of the two happened. A
# double negative reads as the opposite of the finding, and a night that failed
# for a missing row spent eight lines saying "no row for it never arrived".
assert_contains "and says the measurement was absent" "no row for it ever arrived" "$OUT"
assert_lacks "without saying the opposite of the finding" "never arrived" "$OUT"

# A row that arrived without the number the limit is about is the same absence
# wearing a row's clothing.
jq -n '[
  { source: "k6", scenario: "api-baseline", phase: "http", error_rate: 0 },
  { source: "quic", scenario: "quic-agents", phase: "aggregate", rps: 25, error_rate: 0 }
]' >"$WORK/summary.json"
run_check
assert_eq "a row carrying no such number fails the night" "1" "$STATUS"
# The row is present; it is the number inside it that is missing, and a message
# calling that row absent sends a reader looking for the wrong thing.
assert_contains "and says the row arrived without the number" "row that arrived carries no such number" "$OUT"

# A profile with no limits at all is a profile that decides nothing, which must
# not read as a night that cleared everything.
cat >"$WORK/profile.yaml" <<'YAML'
schema_version: 1
name: bare
family: normal
environment: staging
fixture: small
phases:
  - name: steady
    duration: 2m
    connected_agents: 500
safety:
  max_node_cpu_percent: 85
  max_node_memory_percent: 90
  max_error_rate: 0.01
YAML
write_rows 80 25
run_check
assert_eq "a profile that declares no limits is refused" "2" "$STATUS"

# Inputs that are not there fail rather than passing quietly.
STATUS=0
"$CHECK" "$WORK/nope.yaml" "$WORK/summary.json" >/dev/null 2>&1 || STATUS=$?
assert_eq "an absent profile fails" "2" "$STATUS"

write_profile
STATUS=0
"$CHECK" "$WORK/profile.yaml" "$WORK/nope.json" >/dev/null 2>&1 || STATUS=$?
assert_eq "an absent summary fails" "2" "$STATUS"

# --- the shipped scaling profile, at the rung it deliberately saturates -------
#
# The scaling family holds the load and the data constant and varies the
# processors, so the answer is a shape rather than a point. Its bottom rung runs
# the server on a quarter of a processor at 95.6% busy, and that rung is the only
# place in the repository deliberately driven past saturation while still judged
# on whether its machines arrived.
#
# Held at nought, it failed two nights running for one machine in two thousand
# and then for two: 1989 and 1985 of 2000 arrived, one enrolment timing out and
# one refused with a request timeout. The rung's own curve says why — 6.5 s to
# list the fleet at a quarter of a processor against 17 ms at a half — and a
# machine that gave up there is the measurement rather than a defect. Both nights
# were discarded, which is the one outcome the profile's own comment warns
# against: an unmeasured leg silently flattens the curve it is part of.
#
# What the limit is stated against is the run's own write-off line. The same
# figure decides whether a phase measured the target at all, so a verdict
# stricter than it can fail a leg the run has already certified as a valid
# measurement — and the whole band between the two reads that way.
SCALING="$REPO_ROOT/load/profiles/scaling.yaml"
[ -f "$SCALING" ] || {
  echo "FAIL: $SCALING missing" >&2
  exit 1
}

# scaling_rows renders the machine-side rows a scaling leg publishes, at a given
# share of machines that did not arrive.
scaling_rows() {
  jq -n --argjson rate "$1" '[
    { source: "quic", scenario: "quic-agents", phase: "aggregate",
      error_rate: $rate, latency_p95_ms: 55, rps: 25 },
    { source: "quic", scenario: "quic-agents", phase: "connect",
      latency_p95_ms: 55, error_rate: $rate }
  ]' >"$WORK/summary.json"
}

run_scaling() {
  STATUS=0
  "$CHECK" "$SCALING" "$WORK/summary.json" "$WORK/breaches.json" \
    >"$WORK/out.txt" 2>"$WORK/err.txt" || STATUS=$?
  OUT="$(cat "$WORK/out.txt" "$WORK/err.txt")"
}

# Every machine in: the leg passes, which it always did.
scaling_rows 0
run_scaling
assert_eq "scaling: a leg every machine reached passes" "0" "$STATUS"

# The two nights that were thrown away. 1989 of 2000 is 0.00055; 1985 of 2000
# with two failures is 0.001006, which is the figure the gate printed.
scaling_rows 0.00055
run_scaling
assert_eq "scaling: the 09-18 reading (1 machine in 2000) passes" "0" "$STATUS"
scaling_rows 0.0010065425264217413
run_scaling
assert_eq "scaling: the 09-19 reading (2 machines in 2000) passes" "0" "$STATUS"

# And a leg that stopped working still fails. Five percent of a fleet not
# arriving is not a saturated rung reporting its saturation.
scaling_rows 0.05
run_scaling
assert_eq "scaling: a leg that stopped working fails" "1" "$STATUS"
assert_contains "scaling: the breach names the arrivals" "quic/quic-agents/aggregate" "$OUT"

# The limit is stated as a share of the run's own write-off line rather than
# independently of it, so a leg the run certified as valid can never be failed
# by the verdict beside it. A rung nothing judges is what the profile's comment
# warns against, so the limit is not simply removed either.
SCALING_READ="$(python3 "$SCRIPT_DIR/fixtures/scaling-error-gate.py" "$SCALING")"
SCALING_GATE="$(cut -f1 <<<"$SCALING_READ")"
SCALING_SAFETY="$(cut -f2 <<<"$SCALING_READ")"
if [ -n "$SCALING_GATE" ] && awk -v g="$SCALING_GATE" -v s="$SCALING_SAFETY" 'BEGIN { exit !(g > 0 && g <= s) }'; then
  pass "scaling: the verdict sits inside the run's own write-off line ($SCALING_GATE <= $SCALING_SAFETY)"
else
  fail "scaling: the verdict sits inside the run's own write-off line (gate=[$SCALING_GATE] safety=[$SCALING_SAFETY])"
fi

# --- the endurance run's connect ceiling, enforced rather than watched --------
#
# The ceiling was reported rather than enforced while the leg's load changed
# underneath it: it had been bracketed by nights with nothing beside the fleet
# at all, and the volume family making the same change saw its comparable figure
# go from about 400 ms to between 4,510 and 9,443. A ceiling carried across that
# would fail a night for the load the night was asked to apply.
#
# The night that settles it exists. A valid endurance run of 4h44m with the
# technician load beside the fleet completed 38,481 operations against 2,750
# before sessions were added, with no errors and no goroutine growth, and its
# machines connected in 2 ms. The technician load is not what moves the connect
# path, so the figure carries across unchanged and stops being a number nothing
# consults.
SOAK="$REPO_ROOT/load/profiles/soak.yaml"
[ -f "$SOAK" ] || {
  echo "FAIL: $SOAK missing" >&2
  exit 1
}

soak_rows() {
  jq -n --argjson p95 "$1" '[
    { source: "quic", scenario: "quic-agents", phase: "aggregate",
      error_rate: 0, latency_p95_ms: 20, rps: 25 },
    { source: "quic", scenario: "quic-agents", phase: "connect",
      latency_p95_ms: $p95, error_rate: 0 }
  ]' >"$WORK/summary.json"
}

run_soak() {
  STATUS=0
  "$CHECK" "$SOAK" "$WORK/summary.json" "$WORK/breaches.json" \
    >"$WORK/out.txt" 2>"$WORK/err.txt" || STATUS=$?
  OUT="$(cat "$WORK/out.txt" "$WORK/err.txt")"
}

# The night that established it: 2 ms, three orders of magnitude clear.
soak_rows 2
run_soak
assert_eq "soak: the night that established the ceiling passes" "0" "$STATUS"

# And a night past it fails rather than being written down. This is the whole
# change: the same reading used to be recorded and the run went green.
soak_rows 4000
run_soak
assert_eq "soak: a night past the ceiling fails the run" "1" "$STATUS"
assert_contains "soak: the breach names the connect path" "quic/quic-agents/connect" "$OUT"
assert_lacks "soak: and does not merely report it" "reported, not enforced" "$OUT"

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
