#!/usr/bin/env bash
# The summary table has Measurement, Expected, Actual and Result columns; unread shows "not read".
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
YAML

cat >"$WORK/rows.json" <<'JSON'
[
  {"source": "quic", "scenario": "quic-agents", "phase": "aggregate", "error_rate": 0.005},
  {"source": "quic", "scenario": "quic-agents", "phase": "connect", "latency_p95_ms": 150},
  {"source": "quic", "scenario": "quic-agents", "phase": "register", "latency_p95_ms": 1257, "latency_p50_ms": 8.9},
  {"source": "k6", "scenario": "api-baseline", "phase": "http", "latency_p95_ms": 120.4, "latency_p99_ms": 180, "error_rate": 0}
]
JSON

cat >"$WORK/bundle.json" <<'JSON'
{
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

cat >"$WORK/database.json" <<'JSON'
{"cpu_percent": 8.2, "cpu_cap": "1 processor", "memory_percent": 20.5, "memory_cap": "384 MiB"}
JSON

echo "run-summary:"

OUT="$("$SUMMARY" render --title "Busy morning (peak)" --profile "$WORK/profile.yaml" \
  --rows "$WORK/rows.json" --bundle "$WORK/bundle.json" --database "$WORK/database.json")"

# Each heading and metric word carries its explanation as hover text.
p50="$(summary_term p50)" p95="$(summary_term p95)" error_rate="$(summary_term "error rate")"
level="$(summary_term "Service Level")" not_read="$(summary_term "not read")"
has_line "the table has the four columns, each explained on hover" "$(summary_table_header | sed -n 1p)"
has_line "a limit that fails the run, crossed, is FAIL" \
  "| quic/quic-agents/register $p95 | ≤ 500 ms | 1257 ms | FAIL |"
has_line "a limit that is only reported, crossed, is over" \
  "| quic/quic-agents/connect $p95 | ≤ 100 ms (reported only) | 150 ms | over |"
has_line "a reading inside its limit passes" \
  "| k6/api-baseline/http $p95 | ≤ 200 ms | 120.4 ms | pass |"
has_line "a measurement with no limit says so" \
  "| quic/quic-agents/register $p50 | no limit | 8.9 ms | — |"
has_line "the error rate is a percentage" \
  "| quic/quic-agents/aggregate $error_rate | ≤ 2 % | 0.5 % | pass |"
has_line "the server's processor, as a share of its own cap, over the measured phase" \
  "| $level Avg CPU % (server) | — | 34 % of 0.25 processors | — |"
has_line "the server's memory, the same" \
  "| $level Avg Mem % (server) | — | 12 % of 384 MiB | — |"
has_line "the database's processor, as a share of its own cap" \
  "| $level Avg CPU % (database) | — | 8.2 % of 1 processor | — |"
has_line "the database's memory, the same" \
  "| $level Avg Mem % (database) | — | 20.5 % of 384 MiB | — |"
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
beyond_table="$(grep -vE '^(### |\||$)' <<<"$OUT" || true)"
if [ -n "$beyond_table" ]; then
  fail "the summary is its title and its table (out=[$OUT])"
else
  pass "the summary is its title and its table"
fi

jq 'del(.phases[1].target_resident_bytes)' "$WORK/bundle.json" >"$WORK/bundle-unread.json"
OUT="$("$SUMMARY" render --title "Busy morning (peak)" --profile "$WORK/profile.yaml" \
  --rows "$WORK/rows.json" --bundle "$WORK/bundle-unread.json")"
has_line "a server reading the run could not take is not read" \
  "| $level Avg Mem % (server) | — | $not_read | — |"
has_line "and a database nobody read is not read" \
  "| $level Avg CPU % (database) | — | $not_read | — |"
echo '{"cpu_percent": null, "memory_percent": 3}' >"$WORK/database-half.json"
OUT="$("$SUMMARY" render --title "t" --profile "$WORK/profile.yaml" \
  --rows "$WORK/rows.json" --bundle "$WORK/bundle.json" --database "$WORK/database-half.json")"
has_line "a database reading that is missing is not read, beside one that is there" \
  "| $level Avg CPU % (database) | — | $not_read | — |"

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

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
