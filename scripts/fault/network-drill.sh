#!/usr/bin/env bash
# One scenario of the nightly QUIC network drill.
#
# The drill asks four questions a support desk asks: when a machine goes dark
# and comes back, does it reconnect on its own; does the hole its absence left
# in the customer's charts fill in; does a site catching up over a thin uplink
# stay watchable while it does; and does a machine that returns on a new address
# keep its session. Each scenario below is one of those, driven by commanding
# the link shaper the machines' traffic runs through.
#
# Three phases every time — baseline, fault, recovery — and one rule that
# outranks the measurements: a scenario that could not observe the system emits
# NOTHING. Rows of zeroes pull a window median down and one bad night quietly
# costs two, which is the rule scripts/loadtest-quic-run.sh already writes down
# for its own half of the nightly.
#
# Environment:
#   NAMESPACE          must be opengate-staging; anything else is refused
#   PROBE_POD          in-cluster pod with curl, used for every request
#   SHAPER_POD         the shaper's pod, named in the evidence
#   SHAPER_URL         the shaper's cluster-internal control endpoint
#   SERVER_URL         the server's in-cluster API base
#   DEVICE_ID          the real machine this scenario measures
#   API_TOKEN          bearer token for the reads above
#   EVIDENCE_DIR       where the per-phase counters and readings are kept
#   MEASUREMENTS_FILE  one JSON row per measurement, appended
#
# Phase durations are the scenario's own parameters and are overridable so a
# calibration run can shorten them; the nightly leaves them alone.
#
# Usage:  NAMESPACE=opengate-staging … scripts/fault/network-drill.sh s1
set -euo pipefail

ALLOWED_NAMESPACE="opengate-staging"
NAMESPACE="${NAMESPACE:-$ALLOWED_NAMESPACE}"

# What the process returns, and what each code means to the workflow reading it.
# Two outcomes need two codes: a scenario that measured the product and found it
# wanting is a measurement the trend keeps, while a scenario that could not
# observe the product at all has nothing to say about it.
EXIT_INCONCLUSIVE=2

# The phase clock. Every one of these is calibrated in the drill's own
# specification against the 90 s idle timeout and the reconnect backoff.
BASELINE_SECONDS="${NETDRILL_BASELINE_SECONDS:-60}"
FAULT_SECONDS="${NETDRILL_FAULT_SECONDS:-180}"
RECOVERY_SECONDS="${NETDRILL_RECOVERY_SECONDS:-180}"
POLL_SECONDS="${NETDRILL_POLL_SECONDS:-5}"

# How much of the chart window has to come back for the gap to count as filled.
GAP_FILL_TARGET="${NETDRILL_GAP_FILL_TARGET:-0.95}"

SCENARIO="${1:-}"

die() {
  echo "network-drill: $1" >&2
  exit "${2:-1}"
}

# A scenario that could not observe the system says so and leaves the file
# alone. Nothing partial is kept: a run that emitted two of its four rows before
# the shaper went quiet would put an incomplete night into the trend under the
# same labels as a complete one.
inconclusive() {
  echo "network-drill: inconclusive — $1" >&2
  rm -f "$PENDING_ROWS"
  exit "$EXIT_INCONCLUSIVE"
}

[ "$NAMESPACE" = "$ALLOWED_NAMESPACE" ] \
  || die "refusing to run network faults outside the '$ALLOWED_NAMESPACE' namespace (got '$NAMESPACE')"
command -v kubectl >/dev/null 2>&1 || die "kubectl not found on PATH"
command -v jq >/dev/null 2>&1 || die "jq not found on PATH"

: "${PROBE_POD:?PROBE_POD is required — every request goes through an in-cluster client}"
: "${SHAPER_URL:?SHAPER_URL is required}"
: "${SERVER_URL:?SERVER_URL is required}"
: "${DEVICE_ID:?DEVICE_ID is required — a drill with no machine to measure measures nothing}"

SHAPER_POD="${SHAPER_POD:-unknown}"
EVIDENCE_DIR="${EVIDENCE_DIR:-/tmp/opengate-fault-state/network-drill}"
MEASUREMENTS_FILE="${MEASUREMENTS_FILE:-$EVIDENCE_DIR/measurements.jsonl}"
mkdir -p "$EVIDENCE_DIR" "$(dirname "$MEASUREMENTS_FILE")"

# Rows are held here until the scenario finishes, then appended in one go. That
# is what makes "emits nothing" true of a scenario that fell over halfway: a
# file appended to as it went would leave its first half behind.
PENDING_ROWS="$(mktemp)"

COMMIT_SHA="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"

# The link is handed back clear whatever happened, so the scenario after this
# one — or whatever else takes the namespace next — starts on an unimpaired
# path rather than inheriting this one's outage.
clear_the_link() {
  probe_curl -X POST --data '{}' "$SHAPER_URL/impair" >/dev/null 2>&1 || true
  rm -f "$PENDING_ROWS"
}
trap clear_the_link EXIT

# --- talking to the cluster ---------------------------------------------------

# Every request goes through one in-cluster client, because both the shaper's
# control endpoint and the server's API are cluster-internal and an agent speaks
# QUIC over UDP, which kubectl port-forward does not carry.
probe_curl() {
  kubectl -n "$NAMESPACE" exec "$PROBE_POD" -- \
    curl -sS --fail-with-body --max-time 20 "$@"
}

shaper_healthy() {
  probe_curl "$SHAPER_URL/healthz" >/dev/null 2>&1
}

# impair puts one instruction in force. A refusal is inconclusive rather than a
# product failure: the shaper declining an impairment says nothing about how a
# machine copes with one.
impair() {
  local instruction="$1"
  probe_curl -X POST -H 'Content-Type: application/json' --data "$instruction" \
    "$SHAPER_URL/impair" >/dev/null \
    || inconclusive "the shaper refused the instruction $instruction"
}

rebind() {
  probe_curl -X POST "$SHAPER_URL/rebind" >/dev/null \
    || inconclusive "the shaper could not move to a new server-facing address"
}

# counters records what the shaper has done with the datagrams it handled, at
# one phase boundary, and prints it. The runner reads these to know a scenario
# ran at all.
counters() {
  local phase="$1" body
  body="$(probe_curl "$SHAPER_URL/counters")" \
    || inconclusive "the shaper stopped answering at the $phase boundary"
  printf '%s\n' "$body" >"$EVIDENCE_DIR/${SCENARIO}-counters-${phase}.json"
  printf '%s\n' "$body"
}

api_get() {
  probe_curl -H "Authorization: Bearer ${API_TOKEN:-}" "$SERVER_URL$1"
}

# --- reading the product ------------------------------------------------------

# The machine's own row, as a technician's device list shows it.
device_row() {
  local body
  body="$(api_get "/api/v1/devices")" || return 1
  jq -e --arg id "$DEVICE_ID" '.[] | select(.id == $id)' <<<"$body"
}

device_status() {
  device_row | jq -r '.status' 2>/dev/null || printf 'unknown\n'
}

device_last_seen_epoch() {
  local seen
  seen="$(device_row | jq -r '.last_seen' 2>/dev/null)" || return 1
  [ -n "$seen" ] && [ "$seen" != "null" ] || return 1
  date -u -d "$seen" +%s 2>/dev/null
}

# The share of the chart's buckets the machine has reported for, over the window
# the outage covered. This is read from the endpoint a technician's chart reads,
# so what it measures is what a customer would see: every bucket of the window
# is present and one the machine did not report is null.
gap_fill_ratio() {
  local from="$1" to="$2" body
  body="$(api_get "/api/v1/devices/$DEVICE_ID/metrics?from=$from&to=$to&max_points=200")" || return 1
  jq -r '
    [ .series[]?.avg[]? ] as $points
    | if ($points | length) == 0 then 0
      else ([ $points[] | select(. != null) ] | length) / ($points | length)
      end
  ' <<<"$body"
}

# --- what the scenario produces ----------------------------------------------

# One measurement. The labels are the ones the trend is sliced by; the value is
# a number, and a measurement with no number is not emitted rather than emitted
# as zero.
emit() {
  local metric="$1" victim="$2" value="$3"
  [ -n "$value" ] || return 0
  jq -cn \
    --arg metric "$metric" --arg scenario "$SCENARIO" --arg victim "$victim" \
    --arg commit "$COMMIT_SHA" --argjson value "$value" \
    '{metric: $metric, scenario: $scenario, victim: $victim, commit: $commit, env: "ci", value: $value}' \
    >>"$PENDING_ROWS"
}

# The shaper's own account of the phase, carried into the trend so a night whose
# numbers look odd can be read against what the link actually did.
emit_shaper_counters() {
  local body="$1"
  emit netdrill_shaper_dropped_to_server_total link "$(jq -r '.to_server.dropped' <<<"$body")"
  emit netdrill_shaper_dropped_to_machine_total link "$(jq -r '.to_machine.dropped' <<<"$body")"
}

# A scenario whose drop count does not match its instruction did not run. This
# is the check that separates "the machine coped with an outage" from "the
# outage never happened and the machine had nothing to cope with".
require_dropped() {
  local body="$1" dropped
  dropped="$(jq -r '.to_server.dropped // 0' <<<"$body")"
  [ "${dropped:-0}" -gt 0 ] \
    || inconclusive "the shaper's counters record no dropped datagram, so the fault it was told to apply did not reach the link"
}

# publish is the only writer of the measurements file, and it runs once, at the
# end, after every reading the scenario needed has been taken.
publish() {
  [ -s "$PENDING_ROWS" ] || inconclusive "the scenario produced no measurement"
  cat "$PENDING_ROWS" >>"$MEASUREMENTS_FILE"
  rm -f "$PENDING_ROWS"
  echo "network-drill: $SCENARIO measured $(wc -l <"$MEASUREMENTS_FILE") row(s) in total"
}

hold() {
  local seconds="$1"
  [ "$seconds" -gt 0 ] 2>/dev/null && sleep "$seconds"
  return 0
}

# --- the impairments, as the scenarios name them ------------------------------

PASS_THROUGH='{}'
BLACKHOLE='{"blackhole":true}'
# A fifth of what the machine sends, and nothing at all in the direction it
# receives: a customer's upload is the half that degrades, and a symmetric fault
# would hide which side the recovery machinery is coping with.
ONE_WAY_LOSS='{"loss_to_server":0.2}'
# A third of a second each way, which is what a survey office on satellite has
# all day.
SATELLITE='{"delay_each_way_ms":300}'
# One 2 Mbit/s link shared by every machine, buffering a second before it drops
# — which is what a small site's shared uplink is.
THIN_UPLINK='{"rate_bits_per_sec":2000000,"max_queue_ms":1000}'

# --- the scenarios ------------------------------------------------------------

# S1 — the site goes dark for three minutes and comes back on a healthy link.
# Three minutes exceeds the 90 s idle timeout, so the connection genuinely dies
# rather than stalling: a stalled connection never exercises reconnect at all.
run_s1() {
  local dark_from dark_to restored online_at ratio filled_at

  impair "$PASS_THROUGH"
  counters baseline >/dev/null
  hold "$BASELINE_SECONDS"

  dark_from="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  impair "$BLACKHOLE"
  hold "$FAULT_SECONDS"
  require_dropped "$(counters fault)"
  dark_to="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  impair "$PASS_THROUGH"
  restored="$(date +%s)"

  online_at="$(wait_until_online "$restored" "$RECOVERY_SECONDS")"
  emit netdrill_reconnect_seconds real "$online_at"

  filled_at="$(wait_until_filled "$restored" "$dark_from" "$dark_to" "$RECOVERY_SECONDS")"
  emit netdrill_backfill_complete_seconds real "$filled_at"

  ratio="$(gap_fill_ratio "$dark_from" "$dark_to" || echo 0)"
  emit netdrill_gap_fill_ratio real "$ratio"

  emit_shaper_counters "$(counters recovery)"
  publish
}

# S2 — the same outage, recovered over a thin uplink shared by the whole site.
# The number that bites is the staleness of the live readings: it is the only
# one that measures a catch-up batch sitting ahead of the next heartbeat on the
# one ordered stream the machine sends everything over.
run_s2() {
  local restored back staleness transitions

  impair "$PASS_THROUGH"
  counters baseline >/dev/null

  impair "$BLACKHOLE"
  hold "$FAULT_SECONDS"
  require_dropped "$(counters fault)"

  impair "$THIN_UPLINK"
  restored="$(date +%s)"

  # The machine has to be back before its staleness means anything. Measured
  # from the restore, the number would be the outage's own three minutes on
  # every night, whatever the uplink did afterwards — and the question this
  # scenario asks is whether live monitoring stays usable *while the site
  # catches up*.
  back="$(wait_until_online "$restored" "$RECOVERY_SECONDS")"
  emit netdrill_reconnect_seconds real "$back"
  [ -n "$back" ] || inconclusive "the machine never came back over the thin uplink, so there was no catch-up to watch"

  read -r staleness transitions <<<"$(watch_liveness "$((RECOVERY_SECONDS - back))")"
  emit netdrill_live_staleness_max_seconds real "$staleness"
  emit netdrill_offline_transitions real "$transitions"

  emit_shaper_counters "$(counters recovery)"
  impair "$PASS_THROUGH"
  publish
}

# S3 — the connection is up but bad. A machine on saturated rural broadband
# keeps its connection and loses a fifth of what it sends; the question is
# whether it holds the connection or churns.
run_s3() {
  local staleness transitions

  impair "$PASS_THROUGH"
  counters baseline >/dev/null
  hold "$BASELINE_SECONDS"

  impair "$ONE_WAY_LOSS"
  read -r staleness transitions <<<"$(watch_liveness "$FAULT_SECONDS")"
  require_dropped "$(counters fault)"
  emit netdrill_offline_transitions real "$transitions"
  emit netdrill_live_staleness_max_seconds real "$staleness"

  impair "$PASS_THROUGH"
  hold "$RECOVERY_SECONDS"
  emit_shaper_counters "$(counters recovery)"
  publish
}

# S4 — a slow link, and a machine that returns on a new address. Two impairments
# in one window because both are cheap and neither needs a fresh outage.
#
# The re-addressing has a silent failure mode: if the session does not migrate,
# the server keeps writing to a port the shaper has closed and the connection
# dies at the idle timeout looking exactly like an ordinary outage. So survival
# and reconnection are both recorded, and a failure reads as "the migration did
# not happen" rather than "the link broke".
run_s4() {
  local before after survived reconnected transitions

  impair "$PASS_THROUGH"
  counters baseline >/dev/null
  hold "$BASELINE_SECONDS"

  impair "$SATELLITE"
  hold "$FAULT_SECONDS"
  counters fault >/dev/null

  before="$(device_status)"
  rebind
  # The window is watched rather than waited out. A machine that dropped and
  # reconnected inside it is online at both ends of a wait, which is exactly
  # the reading that would report a failed migration as a successful one.
  read -r _ transitions <<<"$(watch_liveness "$RECOVERY_SECONDS")"
  after="$(device_status)"

  # The session survived if the machine never left. It reconnected if it did
  # leave and came back inside the window.
  survived=0
  { [ "$before" = "online" ] && [ "$after" = "online" ] && [ "$transitions" -eq 0 ]; } && survived=1
  reconnected=0
  { [ "$survived" -eq 0 ] && [ "$after" = "online" ]; } && reconnected=1

  emit netdrill_session_survived real "$survived"
  emit netdrill_reconnected_after_rebind real "$reconnected"

  impair "$PASS_THROUGH"
  emit_shaper_counters "$(counters recovery)"
  publish
}

# --- the polls the scenarios share -------------------------------------------

# How long after the link was restored the machine was back, or nothing at all
# if it did not come back inside the budget. Nothing, rather than the budget:
# a machine that never returned did not take exactly as long as the drill was
# willing to wait.
wait_until_online() {
  local from="$1" budget="$2" deadline now
  deadline=$((from + budget))
  while :; do
    now="$(date +%s)"
    if [ "$(device_status)" = "online" ]; then
      printf '%s\n' "$((now - from))"
      return 0
    fi
    [ "$now" -ge "$deadline" ] && return 0
    hold "$POLL_SECONDS"
  done
}

# How long after the link was restored the hole in the machine's charts was
# filled, or nothing if it was not filled inside the budget.
wait_until_filled() {
  local from="$1" window_from="$2" window_to="$3" budget="$4" deadline now ratio
  deadline=$((from + budget))
  while :; do
    now="$(date +%s)"
    ratio="$(gap_fill_ratio "$window_from" "$window_to" || echo 0)"
    if awk -v r="$ratio" -v t="$GAP_FILL_TARGET" 'BEGIN { exit !(r + 0 >= t + 0) }'; then
      printf '%s\n' "$((now - from))"
      return 0
    fi
    [ "$now" -ge "$deadline" ] && return 0
    hold "$POLL_SECONDS"
  done
}

# Watches the machine for a window and reports the worst staleness of its live
# readings and how many times it crossed the offline line, as two numbers.
#
# Staleness is measured against the machine's own last_seen rather than against
# a scrape: it is the age of the newest thing the server has heard from that
# machine, which is exactly what a technician watching the site is looking at.
watch_liveness() {
  local budget="$1" deadline now seen worst=0 transitions=0 previous="online" status age
  deadline=$(($(date +%s) + budget))
  while :; do
    now="$(date +%s)"
    status="$(device_status)"
    if [ "$status" != "online" ]; then
      [ "$previous" = "online" ] && transitions=$((transitions + 1))
    fi
    previous="$status"

    if seen="$(device_last_seen_epoch)"; then
      age=$((now - seen))
      [ "$age" -gt "$worst" ] && worst="$age"
    fi

    [ "$now" -ge "$deadline" ] && break
    hold "$POLL_SECONDS"
  done
  printf '%s %s\n' "$worst" "$transitions"
}

# --- what to run --------------------------------------------------------------

shaper_healthy || inconclusive "the shaper at $SHAPER_URL (pod $SHAPER_POD) is not answering"

case "$SCENARIO" in
  s1) run_s1 ;;
  s2) run_s2 ;;
  s3) run_s3 ;;
  s4) run_s4 ;;
  *) die "no such scenario: '$SCENARIO' — the drill runs s1, s2, s3 and s4" ;;
esac
