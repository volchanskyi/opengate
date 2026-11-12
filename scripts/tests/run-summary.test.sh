#!/usr/bin/env bash
# The summary opens with its verdict; each table reads Measurement, Expected, Actual, Result.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SUMMARY="$REPO_ROOT/scripts/run-summary.sh"
# shellcheck source=scripts/lib/summary-table.sh
. "$REPO_ROOT/scripts/lib/summary-table.sh"

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
has_line() {
  if grep -qxF -- "$2" <<<"$OUT"; then pass "$1"; else fail "$1 (missing [$2] in [$OUT])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

cat >"$WORK/profile.yaml" <<'YAML'
name: peak
version: 1
phases:
  - name: ramp
    duration: 1m
    connected_agents: 500
  - name: peak
    duration: 5m
    connected_agents: 2000
    measured: true
  - name: drain
    duration: 1m
    connected_agents: 0
gates:
  - series: quic/quic-agents/register
    metric: latency_p95_ms
    max: 500
    blocking: true
  - series: quic/quic-agents/connect
    metric: latency_p95_ms
    max: 100
    blocking: false
  - series: quic/quic-agents/aggregate
    metric: error_rate
    max: 0.02
    blocking: true
  - series: k6/api-baseline/http
    metric: latency_p95_ms
    max: 200
    blocking: true
  - series: catchup/gaps/drain
    metric: machines_caught_up
    min: 5000
    blocking: true
  - series: catchup/probe-one/probe
    metric: readings_per_second
    min: 10000
    blocking: true
  - series: service/server/measured
    metric: cpu_percent
    max: 80
    blocking: false
  - series: service/server/measured
    metric: memory_percent
    max: 80
    blocking: false
  - series: service/database/measured
    metric: cpu_percent
    max: 80
    blocking: false
  - series: service/database/measured
    metric: memory_percent
    max: 80
    blocking: false
YAML

cat >"$WORK/rows.json" <<'JSON'
[
  {"source": "quic", "scenario": "quic-agents", "phase": "aggregate", "error_rate": 0.005},
  {"source": "quic", "scenario": "quic-agents", "phase": "connect", "latency_p95_ms": 150},
  {"source": "quic", "scenario": "quic-agents", "phase": "register", "latency_p95_ms": 1257, "latency_p50_ms": 8.9},
  {"source": "k6", "scenario": "api-baseline", "phase": "http", "latency_p95_ms": 120.4, "latency_p99_ms": 180, "error_rate": 0},
  {"source": "catchup", "scenario": "gaps", "phase": "drain", "machines_caught_up": 3951},
  {"source": "catchup", "scenario": "probe-one", "phase": "probe", "readings_per_second": 259118.2},
  {"source": "service", "scenario": "server", "phase": "measured", "cpu_percent": 34, "memory_percent": 12,
   "cpu_cap": 0.25, "memory_bytes": 402653184},
  {"source": "service", "scenario": "database", "phase": "measured", "cpu_percent": 8.2, "memory_percent": 20.5,
   "cpu_cap": "1 processor", "memory_cap": "384 MiB"}
]
JSON

cat >"$WORK/bundle.json" <<'JSON'
{
  "verdict": {"result": "invalid", "reasons": ["the target stopped answering before the end of the run"]},
  "target": {"cpus": 0.25, "memory_bytes": 402653184},
  "phases": [
    {"name": "ramp", "started_at": "2026-09-29T13:44:35Z", "finished_at": "2026-09-29T13:45:35Z",
     "target_busy_percent": 90, "target_resident_bytes": 100000000},
    {"name": "peak", "started_at": "2026-09-29T13:45:35Z", "finished_at": "2026-09-29T13:50:35Z",
     "target_busy_percent": 34, "target_resident_bytes": 48318382},
    {"name": "drain", "started_at": "2026-09-29T13:50:35Z", "finished_at": "2026-09-29T13:51:35Z",
     "target_busy_percent": 5}
  ]
}
JSON

echo "run-summary:"

OUT="$("$SUMMARY" render --title "Busy morning (peak)" --profile "$WORK/profile.yaml" \
  --rows "$WORK/rows.json" --bundle "$WORK/bundle.json")"

# Each heading and metric word carries its explanation as hover text.
p50="$(summary_term p50)" p95="$(summary_term p95)" error_rate="$(summary_term "error rate")"
level="$(summary_term "Service Level")" not_read="$(summary_term "not read")"
has_line "the table has the four columns, each explained on hover" "$(summary_table_header | sed -n 1p)"
has_line "the summary opens with whether the run counted, and why not" \
  "**Did not count:** the target stopped answering before the end of the run."
has_line "a limit that fails the run, crossed, is FAIL" \
  "| quic/quic-agents/register $p95 | ≤ 500 ms | 1257 ms | FAIL |"
has_line "a watched limit, crossed, is over" \
  "| quic/quic-agents/connect $p95 | ≤ 100 ms (pass/over) | 150 ms | over |"
has_line "a reading inside its limit passes" \
  "| k6/api-baseline/http $p95 | ≤ 200 ms | 120.4 ms | pass |"
has_line "a measurement with no limit fails, since every measurement is held to one" \
  "| quic/quic-agents/register $p50 | no limit | 8.9 ms | FAIL |"
has_line "the error rate is a percentage" \
  "| quic/quic-agents/aggregate $error_rate | ≤ 2 % | 0.5 % | pass |"
has_line "a promise counted in machines reads as a count" \
  "| catchup/gaps/drain machines caught up | ≥ 5000 | 3951 | FAIL |"
has_line "the store probe has a table of its own" "#### Store probe"
has_line "the store probe's rate reads as readings a second" \
  "| catchup/probe-one/probe readings a second | ≥ 10000 | 259118.2 | pass |"
has_line "the server and database have a table of their own" "#### Server and database"
has_line "the server's processor, as a share of its own cap, over the measured phase, against its mark" \
  "| $level Avg CPU % (server) | ≤ 80 % (pass/over) | 34 % of 0.25 processors | pass |"
has_line "the server's memory, the same" \
  "| $level Avg Mem % (server) | ≤ 80 % (pass/over) | 12 % of 384 MiB | pass |"
has_line "the database's processor, as a share of its own cap" \
  "| $level Avg CPU % (database) | ≤ 80 % (pass/over) | 8.2 % of 1 processor | pass |"
has_line "the database's memory, the same" \
  "| $level Avg Mem % (database) | ≤ 80 % (pass/over) | 20.5 % of 384 MiB | pass |"
for word in "Measurement" "Expected" "Actual" "Result" "p50" "p95" "p99" "error rate" "throughput" \
  "Service Level" "not read"; do
  if grep -qE "^<span title=\"[^\"]{20,}\">$word</span>$" <<<"$(summary_term "$word")"; then
    pass "$word carries its explanation as hover text"
  else
    fail "$word carries its explanation as hover text (got=[$(summary_term "$word")])"
  fi
done
if grep -q '^- \*\*' <<<"$OUT"; then
  fail "no legend follows the table (out=[$OUT])"
else
  pass "no legend follows the table"
fi
beyond_table="$(grep -vE '^(### |#### |\*\*Did not count:\*\*|\*\*Counted\*\*|\||$)' <<<"$OUT" || true)"
if [ -n "$beyond_table" ]; then
  fail "the summary is its title, its verdict and its tables (out=[$OUT])"
else
  pass "the summary is its title, its verdict and its tables"
fi

jq 'map(if .source == "service" then del(.memory_percent) | (if .scenario == "database" then del(.cpu_percent) else . end) else . end)' \
  "$WORK/rows.json" >"$WORK/rows-unread.json"
OUT="$("$SUMMARY" render --title "Busy morning (peak)" --profile "$WORK/profile.yaml" \
  --rows "$WORK/rows-unread.json" --bundle "$WORK/bundle.json")"
has_line "a server reading the run could not take is not read, and is over its mark unread" \
  "| $level Avg Mem % (server) | ≤ 80 % (pass/over) | $not_read | over |"
has_line "and a database nobody read is not read" \
  "| $level Avg CPU % (database) | ≤ 80 % (pass/over) | $not_read | over |"
has_line "and the database's memory, unread, is not read either" \
  "| $level Avg Mem % (database) | ≤ 80 % (pass/over) | $not_read | over |"
jq '.verdict = {"result": "valid", "reasons": []}' "$WORK/bundle.json" >"$WORK/bundle-valid.json"
OUT="$("$SUMMARY" render --title "t" --profile "$WORK/profile.yaml" \
  --rows "$WORK/rows.json" --bundle "$WORK/bundle-valid.json")"
has_line "a run that measured the system says it counted" "**Counted:** the run measured the system."

if [ "$("$SUMMARY" window "$WORK/profile.yaml" "$WORK/bundle.json")" = "2026-09-29T13:45:35Z 2026-09-29T13:50:35Z" ]; then
  pass "the window is the measured phase's own"
else
  fail "the window is the measured phase's own (got=[$("$SUMMARY" window "$WORK/profile.yaml" "$WORK/bundle.json")])"
fi

grep -v 'measured: true' "$WORK/profile.yaml" >"$WORK/profile-all.yaml"
if [ "$("$SUMMARY" window "$WORK/profile-all.yaml" "$WORK/bundle.json")" = "2026-09-29T13:44:35Z 2026-09-29T13:51:35Z" ]; then
  pass "a profile that marks no phase is measured over all of them"
else
  fail "a profile that marks no phase is measured over all of them"
fi

cat >"$WORK/profile-follows.yaml" <<'YAML'
name: catch-up
phases:
  - name: drain
    duration: 1m
    connected_agents: 10
gates:
  - series: catchup/gaps/drain
    metric: fill_seconds
    max: 960
    blocking: true
  - series: catchup/batches/drain
    metric: deferred
    follows: catchup/gaps/drain|fill_seconds
    blocking: false
  - series: quic/quic-agents/register
    metric: latency_p95_ms
    max: 500
    blocking: true
    blocking_from_cpus: 1
YAML
jq -n '[{source: "catchup", scenario: "gaps", phase: "drain", fill_seconds: 1575},
        {source: "catchup", scenario: "batches", phase: "drain", deferred: 12001},
        {source: "quic", scenario: "quic-agents", phase: "register", target_cpus: 0.25, latency_p95_ms: 28420}]' \
  >"$WORK/rows-follows.json"
OUT="$("$SUMMARY" render --title "t" --profile "$WORK/profile-follows.yaml" --rows "$WORK/rows-follows.json")"
has_line "a measurement that follows another limit reads with it" \
  "| catchup/batches/drain told to wait | with catchup/gaps/drain fill_seconds (pass/over) | 12001 | over |"
has_line "a rung starved on purpose reads a failing limit as a watched mark" \
  "| quic/quic-agents/register $p95 | ≤ 500 ms (pass/over) | 28420 ms | over |"

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
