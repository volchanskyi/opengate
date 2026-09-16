#!/usr/bin/env bash
# The limits a bundle-judged profile declares reach a measurement, and every
# measurement the reader produces reaches a limit.
#
# scripts/loadtest-gate-check.sh is the one thing that reads a limit, and what
# it reads is canonical rows. Those exist only where a browser-side export has
# been joined to the machine-side output, which is the staging night and nothing
# else — so the seven profiles that run on a throwaway machine declared limits
# nobody evaluated. scripts/loadtest-bundle-rows.sh turns a run's own evidence
# into those rows, and this holds the two vocabularies level.
#
# Both directions matter and they fail differently. A limit naming a series the
# reader does not produce fails the night it is first read, loudly and for the
# wrong reason. A series the reader produces that no profile names is a row
# nobody reads, which is the decoration this work exists to remove and which
# nothing would ever report.
#
# The reader itself is exercised against a bundle the harness actually wrote —
# built through the same validator a night's evidence passes — in
# server/tests/loadtest/bundle_rows_test.go. What is here is the reader's
# refusals and the sweep.
#
# Run: ./scripts/tests/loadtest-bundle-rows.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ROWS="$REPO_ROOT/scripts/loadtest-bundle-rows.sh"
WORKFLOW_DIR="$REPO_ROOT/.github/workflows"
PROFILE_DIR="$REPO_ROOT/load/profiles"
# shellcheck source=scripts/lib/loadtest-profile.sh
. "$REPO_ROOT/scripts/lib/loadtest-profile.sh"

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

echo "loadtest bundle rows:"

[ -x "$ROWS" ] || {
  fail "$ROWS is not executable"
  printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
  exit 1
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# bundle_observing writes a bundle carrying the named series, in the shape the
# harness writes one.
bundle_observing() {
  local out="$1"
  shift
  local series=()
  while [ "$#" -gt 0 ]; do
    series+=("$(jq -nc --arg s "$1" --argjson v "$2" \
      '{at: "2026-09-13T02:30:00Z", series: $s, value: $v}')")
    shift 2
  done
  jq -n --argjson o "$(printf '%s\n' "${series[@]+"${series[@]}"}" | jq -sc '.')" '{
    schema_version: 11,
    run: {
      id: "quic-agents-1",
      commit: "0b5d1f2c3a4e5d6f7089abcdef0123456789abcd",
      environment: "runner",
      finished_at: "2026-09-13T02:30:00Z"
    },
    observations: $o
  }' >"$out"
}

run_rows() {
  STATUS=0
  OUT="$("$ROWS" "$@" 2>"$WORK/err.txt")" || STATUS=$?
}

# --- what the reader produces ------------------------------------------------

bundle_observing "$WORK/full.json" \
  aggregate_error_rate 0.004 connect_p95_ms 41 handshake_p95_ms 12 register_p95_ms 23.45
run_rows "$WORK/full.json"
assert_eq "a bundle carrying every series is read" "0" "$STATUS"

EMITTED="$(jq -r '
  .[] | . as $row
  | ["latency_p50_ms", "latency_p95_ms", "latency_p99_ms", "rps", "error_rate"][]
  | select($row[.] != null)
  | "\($row.source)/\($row.scenario)/\($row.phase)|\(.)"' <<<"$OUT" | sort -u)"

assert_eq "the aggregate error rate is read off the bundle" "0.004" \
  "$(jq -r '.[] | select(.phase == "aggregate") | .error_rate' <<<"$OUT")"
assert_eq "registration is read off the bundle" "23.45" \
  "$(jq -r '.[] | select(.phase == "register") | .latency_p95_ms' <<<"$OUT")"
assert_eq "every row names the machine side" "quic quic-agents" \
  "$(jq -r '[.[] | .source] + [.[] | .scenario] | unique | join(" ")' <<<"$OUT")"

# Registration is the server's own figure and a run the server did not answer
# has none. An absent row is what lets the limits on it fail the night; a row
# carrying nought would pass every ceiling ever written.
bundle_observing "$WORK/no-register.json" aggregate_error_rate 0 connect_p95_ms 41
run_rows "$WORK/no-register.json"
assert_eq "a run the server did not answer still reads" "0" "$STATUS"
assert_eq "and publishes no registration row" "0" \
  "$(jq '[.[] | select(.phase == "register")] | length' <<<"$OUT")"

# --- what it refuses ---------------------------------------------------------
#
# Each of these would otherwise arrive as a night where every limit passed for
# want of anything to compare against, which a caller cannot tell from a clean
# one.

run_rows "$WORK/absent.json"
assert_eq "a bundle that is not there refuses" "2" "$STATUS"

bundle_observing "$WORK/no-aggregate.json" connect_p95_ms 41 register_p95_ms 23
run_rows "$WORK/no-aggregate.json"
assert_eq "a bundle stating no aggregate error rate refuses" "2" "$STATUS"
if grep -q 'aggregate' "$WORK/err.txt"; then
  pass "and says which reading it wanted"
else
  fail "and says which reading it wanted"
fi

bundle_observing "$WORK/empty.json"
run_rows "$WORK/empty.json"
assert_eq "a bundle observing nothing refuses" "2" "$STATUS"

run_rows
assert_eq "a call naming no bundle refuses" "2" "$STATUS"

# The rows it produces are rows the evaluator reads: the two scripts join here
# or they join nowhere.
run_rows "$WORK/full.json" "$WORK/rows.json"
assert_eq "rows can be written for the evaluator" "0" "$STATUS"
GATE_STATUS=0
"$REPO_ROOT/scripts/loadtest-gate-check.sh" "$PROFILE_DIR/peak.yaml" "$WORK/rows.json" \
  >"$WORK/gate.txt" 2>&1 || GATE_STATUS=$?
assert_eq "the evaluator reads them without complaint" "0" "$GATE_STATUS"
if grep -qE 'gates: [0-9]+ read, 0 breached' "$WORK/gate.txt"; then
  pass "and reports how many limits it read"
else
  fail "and reports how many limits it read: $(cat "$WORK/gate.txt")"
fi

# --- the sweep ---------------------------------------------------------------

# bundle_judged_profiles — every profile whose limits are read off a bundle: the
# profiles named by a workflow that runs this reader. The workflows call it
# through scripts/perf-bundle-limits.sh, which pairs it with the one evaluator
# and holds an invalid run's numbers apart from a valid one's, so that is the
# name a workflow is searched for.
bundle_judged_profiles() {
  local workflow
  while IFS= read -r workflow; do
    [ -n "$workflow" ] || continue
    grep -oE 'load/profiles/[a-z0-9-]+\.yaml' "$workflow" || true
  done < <(grep -rlE 'perf-bundle-limits\.sh' "$WORKFLOW_DIR" 2>/dev/null || true) | sort -u
}

# A leg that writes an evidence bundle and reads no limits is the state this
# closes, so a workflow whose run produces a bundle and no browser-side export
# to join it to has to run the reader.
#
# The bundle is what makes a workflow one of these, rather than the harness
# binary: the network drill runs the same binary as a site — no profile, no
# limits, no bundle — so there is nothing there for a reader to read, and a
# sweep that selected on the binary would demand one.
machine_side_workflows() {
  local workflow
  while IFS= read -r workflow; do
    grep -qE 'loadtest-summarize\.sh' "$workflow" && continue
    echo "$workflow"
  done < <(grep -rlE -- '-bundle=' "$WORKFLOW_DIR" 2>/dev/null || true)
}

swept=0
while IFS= read -r workflow; do
  [ -n "$workflow" ] || continue
  swept=$((swept + 1))
  if grep -qE 'perf-bundle-limits\.sh' "$workflow"; then
    pass "$(basename "$workflow") reads its limits off the run's own evidence"
  else
    fail "$(basename "$workflow") writes a bundle and reads no limit at all"
  fi
done < <(machine_side_workflows)

if [ "$swept" -ge 2 ]; then
  pass "reached $swept workflow(s) whose only output is a bundle"
else
  fail "reached only $swept such workflow(s) — the naming changed shape"
fi

profiles=0
limits=0
gated=""
while IFS= read -r relative; do
  [ -n "$relative" ] || continue
  profile="$REPO_ROOT/$relative"
  if [ ! -s "$profile" ]; then
    fail "a workflow names $relative and there is no such profile"
    continue
  fi
  profiles=$((profiles + 1))

  unreadable=""
  while IFS= read -r measurement; do
    [ -n "$measurement" ] || continue
    limits=$((limits + 1))
    gated="$gated$measurement"$'\n'
    grep -qxF "$measurement" <<<"$EMITTED" || unreadable="$unreadable $measurement"
  done < <(profile_gates "$profile" | jq -r '.[] | "\(.series)|\(.metric)"')

  if [ -z "$unreadable" ]; then
    pass "$(basename "$relative") limits only what a bundle states"
  else
    fail "$(basename "$relative") limits measurements no bundle carries:$unreadable"
  fi
done < <(bundle_judged_profiles)

# And back the other way. A row the reader produces that no profile names is a
# measurement nobody decided about, which reads from the outside exactly like
# one deliberately left alone.
unread=""
while IFS= read -r measurement; do
  [ -n "$measurement" ] || continue
  grep -qxF "$measurement" <<<"$gated" || unread="$unread $measurement"
done <<<"$EMITTED"

if [ -z "$unread" ]; then
  pass "every measurement the reader produces is one some profile holds"
else
  fail "the reader produces measurements no bundle-judged profile names:$unread"
fi

# A sweep that reached nothing proves nothing, which is the same shape as the
# limits it exists to catch.
if [ "$profiles" -ge 5 ] && [ "$limits" -ge 8 ]; then
  pass "read $limits limit(s) across $profiles profile(s)"
else
  fail "read only $limits limit(s) across $profiles profile(s) — did the workflows stop naming profiles?"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
