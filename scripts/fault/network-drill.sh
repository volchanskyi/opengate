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
#   MACHINE_POD        that machine's pod (required) — its own log is where the
#                      reconnect actually happened, and a five-second poll of a
#                      status the server writes is not a reading of it
#   FLEET_PREFIX       the name this run's simulated machines carry (required),
#                      so a scenario can count its own herd rather than whatever
#                      else is in the tenant
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

# How far the first scenario's outage moves either side of the declared length,
# drawn from the run's own seed.
#
# A fixed three minutes is two whole idle timeouts, so the machine met the
# restored link at the same point in its own cycle on every night and the figure
# that decides read the same number twice — 13 one night, 18 the next, against a
# floor of 120 it could not reach. Moving the length moves where in that cycle
# the link returns, which is the only thing that gives the figure a spread.
FAULT_SPREAD_SECONDS="${NETDRILL_FAULT_SPREAD_SECONDS:-90}"
SHAPER_SEED="${SHAPER_SEED:-0}"

# How much of the herd has to be behind the link before a catch-up is a site
# catching up. The server admits four drains per customer, so eight is four
# draining with four more waiting — the smallest fleet that produces the queue
# the thin-uplink scenario is about.
FLEET_MINIMUM="${NETDRILL_FLEET_MINIMUM:-8}"

# What the machine writes as it fails to get its link back, and as it gets it.
ATTEMPT_FAILED_MARK="connection attempt failed"
RECONNECTED_MARK="reconnected successfully"

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
: "${MACHINE_POD:?MACHINE_POD is required — the reconnect figure is the account that machine itself keeps of when it came back}"
: "${FLEET_PREFIX:?FLEET_PREFIX is required — a scenario that cannot name its herd cannot tell whether it was there}"

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
# ran at all. A boundary it could not read is refused rather than answered.
counters() {
  local phase="$1" body
  body="$(probe_curl "$SHAPER_URL/counters")" || return 1
  printf '%s\n' "$body" >"$EVIDENCE_DIR/${SCENARIO}-counters-${phase}.json"
  printf '%s\n' "$body"
}

# The reading at one phase boundary, left in COUNTERS for the scenario to use.
#
# It is assigned here, in the scenario's own shell, rather than inside a command
# substitution at the point of use. A substitution runs in a subshell, and
# ending the scenario from inside one ends only that subshell: the run would
# carry on with an empty reading and decide the phase on it.
COUNTERS=""
read_counters() {
  local phase="$1"
  COUNTERS="$(counters "$phase")" \
    || inconclusive "the shaper stopped answering at the $phase boundary"
}

# The shaper counts for the life of its process and its control endpoint offers
# no reset, so every reading it gives is a running total across every scenario
# that ran before this one. What a scenario has to say about the link is what
# changed while it held it, so each scenario opens by recording where the totals
# stood and measures everything it publishes from there.
OPENING_DROPPED_TO_SERVER=0
OPENING_DROPPED_TO_MACHINE=0

open_counters() {
  read_counters baseline
  OPENING_DROPPED_TO_SERVER="$(jq -r '.to_server.dropped // 0' <<<"$COUNTERS")"
  OPENING_DROPPED_TO_MACHINE="$(jq -r '.to_machine.dropped // 0' <<<"$COUNTERS")"
}

# What the shaper dropped in one direction while this scenario held the link.
dropped_since_opening() {
  local body="$1" direction="$2" opening now
  case "$direction" in
    to_server) opening="$OPENING_DROPPED_TO_SERVER" ;;
    *) opening="$OPENING_DROPPED_TO_MACHINE" ;;
  esac
  now="$(jq -r ".${direction}.dropped // 0" <<<"$body")"
  printf '%s\n' "$((${now:-0} - ${opening:-0}))"
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

# What a status reading is when the drill could not take one. No machine is
# ever in this state: it says the drill failed to observe, which is a different
# thing from the machine having moved, and every caller treats it that way.
UNREADABLE_STATUS="unreadable"

# The machine's status as a technician's device list shows it, or
# UNREADABLE_STATUS when the drill could not read it. A request that did not
# land, a machine missing from the list, and a reply with nothing in it are all
# the drill failing to observe the machine. None of them is a machine that went
# offline, and every comparison here is against "online", so answering with any
# of them as though it were a status reports a failed read as a failed product.
device_status() {
  local body status
  body="$(device_row)" || {
    printf '%s\n' "$UNREADABLE_STATUS"
    return 0
  }
  status="$(jq -r '.status // empty' <<<"$body" 2>/dev/null)" || status=""
  [ -n "$status" ] || status="$UNREADABLE_STATUS"
  printf '%s\n' "$status"
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

# The machine's own clock, so both ends of a reconnect figure are read off one
# clock. The link is restored by a runner outside the cluster and the machine
# writes its log inside it, and seconds of skew between the two would land
# straight in a figure measured in seconds.
machine_clock_now() {
  kubectl -n "$NAMESPACE" exec "$MACHINE_POD" -- date -u +%s.%N 2>/dev/null
}

# What the machine wrote from the given moment onward.
machine_log_since() {
  local since="$1"
  kubectl -n "$NAMESPACE" logs "$MACHINE_POD" --timestamps --since-time="$since" 2>/dev/null
}

# The moment one log line was written, as seconds. It is the stamp kubectl puts
# in front of the line rather than the one the agent's own formatter writes:
# both are the node's clock, and the stamped one needs no assumption about a
# formatter that wraps its timestamp in terminal escapes.
log_epoch() {
  local stamp
  stamp="${1%% *}"
  [ -n "$stamp" ] || return 1
  date -u -d "$stamp" +%s.%N 2>/dev/null
}

at_or_after() {
  awk -v a="$1" -v b="$2" 'BEGIN { exit !(a + 0 >= b + 0) }'
}

# Two figures about one reconnect, or nothing at all.
#
# The first is what a site waits through: the link is back at this moment and
# the machine is on it that many seconds later. The second is what the machine
# itself spent: from its last failed attempt to being back.
#
# They differ by where in its own cycle the machine met the restored link, and
# that difference is most of the figure. On the night this was written the site
# waited 17.7 seconds and the reconnect took 0.315 of one — the machine was
# sitting inside a ninety-second attempt that could not finish, and the link came
# back part way through it.
#
# Nothing at all, rather than a zero, when the log could not be read or carries
# no return after the link came back. A log the drill could not read is the
# drill failing to observe; it is not a machine that failed to come back, and
# the reading that answers that question is taken from the status poll.
# What the machine's own log says about coming back: how long the site waited,
# and — where the machine actually failed an attempt — what that attempt cost.
# The second is empty for a machine whose first try worked, which is a machine
# that spent nothing on a failed attempt rather than one that was quick.
reconnect_from_machine_log() {
  local since="$1" restored="$2"
  local log line stamp back="" last_fail=""

  [ -n "$restored" ] || return 1
  log="$(machine_log_since "$since")" || return 1
  [ -n "$log" ] || return 1

  while IFS= read -r line; do
    [ -n "$line" ] || continue
    stamp="$(log_epoch "$line")" || continue
    [ -n "$stamp" ] || continue
    if [ -n "$back" ]; then
      continue
    fi
    case "$line" in
      *"$RECONNECTED_MARK"*)
        if at_or_after "$stamp" "$restored"; then
          back="$stamp"
        fi
        ;;
      *"$ATTEMPT_FAILED_MARK"*) last_fail="$stamp" ;;
    esac
  done <<<"$log"

  [ -n "$back" ] || return 1

  local waited spent=""
  waited="$(awk -v b="$back" -v r="$restored" 'BEGIN { printf "%.3f", b - r }')"
  if [ -n "$last_fail" ]; then
    spent="$(awk -v b="$back" -v f="$last_fail" 'BEGIN { printf "%.3f", b - f }')"
  fi
  printf '%s %s\n' "$waited" "$spent"
}

# How many of this run's own simulated machines the server currently has online.
#
# Read from the product's device list rather than from the shaper, whose
# machines field is an idle-mapping expiry: it went on reading 21 for ten
# minutes after the herd had left, and first read 1 twenty minutes after.
fleet_online() {
  local body
  body="$(api_get "/api/v1/devices")" || return 1
  jq -r --arg p "${FLEET_PREFIX}-" \
    '[ .[] | select((.hostname // "") | startswith($p)) | select(.status == "online") ] | length' \
    <<<"$body" 2>/dev/null
}

# The first scenario's outage length, moved off the machine's own timeouts by
# the run's seed so the figure it decides is not the same number every night.
# A collapsed phase clock stays collapsed, so a calibration run is unaffected.
outage_seconds() {
  if [ "$FAULT_SECONDS" -le 0 ] || [ "$FAULT_SPREAD_SECONDS" -le 0 ]; then
    printf '%s\n' "$FAULT_SECONDS"
    return 0
  fi
  printf '%s\n' "$((FAULT_SECONDS - FAULT_SPREAD_SECONDS / 2 + SHAPER_SEED % FAULT_SPREAD_SECONDS))"
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

# The shaper's own account of what this scenario did to the link, carried into
# the trend so a night whose numbers look odd can be read against it.
emit_shaper_counters() {
  local body="$1"
  emit netdrill_shaper_dropped_to_server link "$(dropped_since_opening "$body" to_server)"
  emit netdrill_shaper_dropped_to_machine link "$(dropped_since_opening "$body" to_machine)"
}

# The two reconnect figures, when the machine's own log has them. A scenario
# whose log could not be read publishes neither, and still publishes everything
# else it measured.
#
# The attempt figure is published only by a machine that made a failed attempt.
# A machine whose first try worked spent nothing on one, and timing from the
# moment it noticed the loss instead times the outage — which is the luck this
# figure exists to hold apart and which the figure beside it already carries.
emit_reconnect_figures() {
  local since="$1" restored_epoch="$2" figures waited spent
  figures="$(reconnect_from_machine_log "$since" "$restored_epoch")" || return 0
  read -r waited spent <<<"$figures"
  emit netdrill_reconnect_seconds real "$waited"
  [ -n "$spent" ] && emit netdrill_reconnect_attempt_seconds real "$spent"
  return 0
}

# Whether the machine came back at all, which is a different question from how
# long it took and is answered by a different reading. The status poll answers
# empty for a machine that never returned inside the budget, and refuses
# outright when it could not read the status at all — so an empty answer here is
# the machine, not the drill.
emit_reconnected() {
  local online_at="$1"
  if [ -n "$online_at" ]; then
    emit netdrill_reconnected real 1
  else
    emit netdrill_reconnected real 0
  fi
}

# A scenario whose drop count does not match its instruction did not run. This
# is the check that separates "the machine coped with an outage" from "the
# outage never happened and the machine had nothing to cope with", so what it
# asks about is what this scenario dropped: a total left on the clock by an
# earlier scenario's outage is not evidence that this one's fault reached the
# link.
require_dropped() {
  local body="$1"
  [ "$(dropped_since_opening "$body" to_server)" -gt 0 ] \
    || inconclusive "the shaper dropped nothing toward the server while this scenario held the link, so the fault it was told to apply did not reach it"
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
  local dark_from dark_to restored restored_epoch online_at ratio filled_at outage

  impair "$PASS_THROUGH"
  open_counters
  hold "$BASELINE_SECONDS"

  outage="$(outage_seconds)"
  dark_from="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  impair "$BLACKHOLE"
  hold "$outage"
  read_counters fault
  require_dropped "$COUNTERS"
  dark_to="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  impair "$PASS_THROUGH"
  restored="$(date +%s)"
  # Read before anything else in the recovery, because it is the moment the
  # figures below are measured from.
  restored_epoch="$(machine_clock_now || true)"
  # The length this night actually ran, published so the figures it decides can
  # be read against it rather than assumed to share a window.
  emit netdrill_outage_seconds link "$outage"

  online_at="$(wait_until_online "$restored" "$RECOVERY_SECONDS")" \
    || inconclusive "the drill never read the machine's status while waiting for it to come back"
  emit_reconnected "$online_at"
  emit_reconnect_figures "$dark_from" "$restored_epoch"

  filled_at="$(wait_until_filled "$restored" "$dark_from" "$dark_to" "$RECOVERY_SECONDS")"
  emit netdrill_backfill_complete_seconds real "$filled_at"

  ratio="$(gap_fill_ratio "$dark_from" "$dark_to" || echo 0)"
  emit netdrill_gap_fill_ratio real "$ratio"

  read_counters recovery
  emit_shaper_counters "$COUNTERS"
  publish
}

# S2 — the same outage, recovered over a thin uplink shared by the whole site.
# The number that bites is the staleness of the live readings: it is the only
# one that measures a catch-up batch sitting ahead of the next heartbeat on the
# one ordered stream the machine sends everything over.
run_s2() {
  local dark_from restored restored_epoch back herd staleness transitions watched

  impair "$PASS_THROUGH"
  open_counters

  dark_from="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  impair "$BLACKHOLE"
  hold "$FAULT_SECONDS"
  read_counters fault
  require_dropped "$COUNTERS"

  impair "$THIN_UPLINK"
  restored="$(date +%s)"
  restored_epoch="$(machine_clock_now || true)"

  # The machine has to be back before its staleness means anything. Measured
  # from the restore, the number would be the outage's own three minutes on
  # every night, whatever the uplink did afterwards — and the question this
  # scenario asks is whether live monitoring stays usable *while the site
  # catches up*.
  back="$(wait_until_online "$restored" "$RECOVERY_SECONDS")" \
    || inconclusive "the drill never read the machine's status while waiting for it to come back over the thin uplink"
  emit_reconnected "$back"
  emit_reconnect_figures "$dark_from" "$restored_epoch"
  [ -n "$back" ] || inconclusive "the machine never came back over the thin uplink, so there was no catch-up to watch"

  # And the herd has to be back, because a site is what this scenario measures.
  # One machine catching up over a two-megabit link uses under half of it; four
  # of them, which is what the server admits per customer, ask for nearly twice
  # what the link has. Without the herd the figure below is a reading of an
  # uncontended link — and it becomes the baseline that a night with a real herd
  # is then measured against, so the night that fixes the fleet reads as the
  # regression.
  herd="$(fleet_online)" \
    || inconclusive "the drill could not read how much of its herd was behind the link"
  emit netdrill_fleet_online fleet "$herd"
  [ "${herd:-0}" -ge "$FLEET_MINIMUM" ] \
    || inconclusive "only ${herd:-0} of the herd was behind the link, and this scenario measures a site catching up rather than one machine on an empty one"

  watched="$(watch_liveness "$((RECOVERY_SECONDS - back))")" \
    || inconclusive "the drill never read the machine's status while the site caught up"
  read -r staleness transitions <<<"$watched"
  emit netdrill_live_staleness_max_seconds real "$staleness"
  emit netdrill_offline_transitions real "$transitions"

  read_counters recovery
  emit_shaper_counters "$COUNTERS"
  impair "$PASS_THROUGH"
  publish
}

# S3 — the connection is up but bad. A machine on saturated rural broadband
# keeps its connection and loses a fifth of what it sends; the question is
# whether it holds the connection or churns.
run_s3() {
  local staleness transitions watched

  impair "$PASS_THROUGH"
  open_counters
  hold "$BASELINE_SECONDS"

  impair "$ONE_WAY_LOSS"
  watched="$(watch_liveness "$FAULT_SECONDS")" \
    || inconclusive "the drill never read the machine's status while the link was lossy"
  read -r staleness transitions <<<"$watched"
  read_counters fault
  require_dropped "$COUNTERS"
  emit netdrill_offline_transitions real "$transitions"
  emit netdrill_live_staleness_max_seconds real "$staleness"

  impair "$PASS_THROUGH"
  hold "$RECOVERY_SECONDS"
  read_counters recovery
  emit_shaper_counters "$COUNTERS"
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
  local before after survived reconnected transitions watched

  impair "$PASS_THROUGH"
  open_counters
  hold "$BASELINE_SECONDS"

  impair "$SATELLITE"
  hold "$FAULT_SECONDS"
  read_counters fault

  # Both verdicts below are decided by comparing these two readings against
  # "online", so a reading the drill could not take would publish a migration
  # that failed on the strength of a request that did not arrive.
  before="$(device_status)"
  [ "$before" != "$UNREADABLE_STATUS" ] \
    || inconclusive "the drill could not read the machine's status before the address change"
  rebind
  # The window is watched rather than waited out. A machine that dropped and
  # reconnected inside it is online at both ends of a wait, which is exactly
  # the reading that would report a failed migration as a successful one.
  watched="$(watch_liveness "$RECOVERY_SECONDS")" \
    || inconclusive "the drill never read the machine's status after the address change"
  read -r _ transitions <<<"$watched"
  after="$(device_status)"
  [ "$after" != "$UNREADABLE_STATUS" ] \
    || inconclusive "the drill could not read the machine's status after the address change"

  # The session survived if the machine never left. It reconnected if it did
  # leave and came back inside the window.
  survived=0
  { [ "$before" = "online" ] && [ "$after" = "online" ] && [ "$transitions" -eq 0 ]; } && survived=1
  reconnected=0
  { [ "$survived" -eq 0 ] && [ "$after" = "online" ]; } && reconnected=1

  emit netdrill_session_survived real "$survived"
  emit netdrill_reconnected_after_rebind real "$reconnected"

  impair "$PASS_THROUGH"
  read_counters recovery
  emit_shaper_counters "$COUNTERS"
  publish
}

# --- the polls the scenarios share -------------------------------------------

# How long after the link was restored the machine was back, or nothing at all
# if it did not come back inside the budget. Nothing, rather than the budget:
# a machine that never returned did not take exactly as long as the drill was
# willing to wait.
#
# Refuses — rather than answering "not back" — when it never once read the
# machine's status. A poll that could not see the machine has found nothing out
# about whether it returned, and answering would report the drill's own blind
# spot as the machine failing to come back.
wait_until_online() {
  local from="$1" budget="$2" deadline now status readings=0
  deadline=$((from + budget))
  while :; do
    now="$(date +%s)"
    status="$(device_status)"
    [ "$status" = "$UNREADABLE_STATUS" ] || readings=$((readings + 1))
    if [ "$status" = "online" ]; then
      printf '%s\n' "$((now - from))"
      return 0
    fi
    [ "$now" -ge "$deadline" ] && break
    hold "$POLL_SECONDS"
  done
  [ "$readings" -gt 0 ]
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
#
# Refuses, as the poll above does, when it never read the machine's status:
# a window of readings nobody could take reports no staleness and no crossing
# of the offline line, which is indistinguishable from a machine that behaved.
watch_liveness() {
  local budget="$1" deadline now seen worst=0 transitions=0 previous="online" status age readings=0
  deadline=$(($(date +%s) + budget))
  while :; do
    now="$(date +%s)"
    status="$(device_status)"
    # A reading the drill could not take places the machine nowhere: it neither
    # crosses the offline line nor clears it, so it is not a sample and the last
    # real reading still stands.
    if [ "$status" != "$UNREADABLE_STATUS" ]; then
      readings=$((readings + 1))
      if [ "$status" != "online" ] && [ "$previous" = "online" ]; then
        transitions=$((transitions + 1))
      fi
      previous="$status"
    fi

    if seen="$(device_last_seen_epoch)"; then
      age=$((now - seen))
      [ "$age" -gt "$worst" ] && worst="$age"
    fi

    [ "$now" -ge "$deadline" ] && break
    hold "$POLL_SECONDS"
  done
  printf '%s %s\n' "$worst" "$transitions"
  [ "$readings" -gt 0 ]
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
