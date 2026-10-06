#!/usr/bin/env bash
# One scenario of the nightly QUIC network drill, driven by commanding the link shaper.
# A scenario that could not observe the system emits no rows, so a window median is never skewed.
#
# Environment:
#   NAMESPACE          must be opengate-staging; anything else is refused
#   PROBE_POD          in-cluster pod with curl, used for every request
#   SHAPER_POD         the shaper's pod, named in the evidence
#   SHAPER_URL         the shaper's cluster-internal control endpoint
#   SERVER_URL         the server's in-cluster API base
#   DEVICE_ID          the real machine this scenario measures
#   MACHINE_POD        that machine's pod (required), whose own log records the reconnect
#   FLEET_PREFIX       the name this run's simulated machines carry (required)
#   API_TOKEN          bearer token for the reads above
#   EVIDENCE_DIR       where the per-phase counters and readings are kept
#   MEASUREMENTS_FILE  one JSON row per measurement, appended
#
# Usage:  NAMESPACE=opengate-staging … scripts/fault/network-drill.sh s1
set -euo pipefail

ALLOWED_NAMESPACE="opengate-staging"
NAMESPACE="${NAMESPACE:-$ALLOWED_NAMESPACE}"

# A scenario that could not observe the product exits with this code and keeps no measurement.
EXIT_INCONCLUSIVE=2

# Phase durations, overridable for calibration runs.
BASELINE_SECONDS="${NETDRILL_BASELINE_SECONDS:-60}"
FAULT_SECONDS="${NETDRILL_FAULT_SECONDS:-180}"
RECOVERY_SECONDS="${NETDRILL_RECOVERY_SECONDS:-180}"
POLL_SECONDS="${NETDRILL_POLL_SECONDS:-5}"

# The share of the chart window that has to come back for the gap to count as filled.
GAP_FILL_TARGET="${NETDRILL_GAP_FILL_TARGET:-0.95}"

# The first scenario's outage moves either side of its declared length by a seed-drawn amount.
# The shift moves where in the machine's idle cycle the link returns, which spreads the figure.
FAULT_SPREAD_SECONDS="${NETDRILL_FAULT_SPREAD_SECONDS:-90}"
SHAPER_SEED="${SHAPER_SEED:-0}"

# The server admits four drains per customer, so eight is four draining and four queued.
FLEET_MINIMUM="${NETDRILL_FLEET_MINIMUM:-8}"

# The log lines the machine writes as it fails to get its link back, and as it gets it.
ATTEMPT_FAILED_MARK="connection attempt failed"
RECONNECTED_MARK="reconnected successfully"

SCENARIO="${1:-}"

die() {
  echo "network-drill: $1" >&2
  exit "${2:-1}"
}

# An inconclusive scenario discards its pending rows, so no partial night reaches the trend.
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

# Rows are held here until the scenario finishes, so a scenario that fails midway leaves none.
PENDING_ROWS="$(mktemp)"

COMMIT_SHA="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"

# The link is cleared on exit so the next scenario starts on an unimpaired path.
clear_the_link() {
  probe_curl -X POST --data '{}' "$SHAPER_URL/impair" >/dev/null 2>&1 || true
  rm -f "$PENDING_ROWS"
}
trap clear_the_link EXIT

# shellcheck source=../lib/kubectl-retry.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/kubectl-retry.sh"

# The shaper control endpoint and the server API are cluster-internal, and QUIC rides UDP that
# port-forward does not carry. A request is retried only when it never reached the probe pod.
probe_curl() {
  kubectl_retry --unstarted -n "$NAMESPACE" exec "$PROBE_POD" -- \
    curl -sS --fail-with-body --max-time 20 "$@"
}

# True when the probe failed because the cluster dropped the connection to the probe pod each time.
never_arrived() { [ "$1" -eq "$KUBECTL_RETRY_LOST" ]; }

NEVER_ARRIVED="the cluster dropped the connection to the probe pod on every attempt"

shaper_healthy() {
  probe_curl "$SHAPER_URL/healthz" >/dev/null 2>&1
}

# A refused impairment is inconclusive, and an instruction that never arrived is named apart.
impair() {
  local instruction="$1" status=0
  probe_curl -X POST -H 'Content-Type: application/json' --data "$instruction" \
    "$SHAPER_URL/impair" >/dev/null || status=$?
  [ "$status" -eq 0 ] && return 0
  if never_arrived "$status"; then
    inconclusive "the shaper never received the instruction $instruction: $NEVER_ARRIVED"
  fi
  inconclusive "the shaper refused the instruction $instruction"
}

rebind() {
  local status=0
  probe_curl -X POST "$SHAPER_URL/rebind" >/dev/null || status=$?
  [ "$status" -eq 0 ] && return 0
  if never_arrived "$status"; then
    inconclusive "the shaper never received the request to move to a new server-facing address: $NEVER_ARRIVED"
  fi
  inconclusive "the shaper could not move to a new server-facing address"
}

# Prints the shaper's counters at one phase boundary; a failed read returns the probe's status.
counters() {
  local phase="$1" body status=0
  body="$(probe_curl "$SHAPER_URL/counters")" || status=$?
  [ "$status" -eq 0 ] || return "$status"
  printf '%s\n' "$body" >"$EVIDENCE_DIR/${SCENARIO}-counters-${phase}.json"
  printf '%s\n' "$body"
}

# The reading is assigned in the scenario's shell, since an exit in a substitution ends a subshell.
COUNTERS=""
read_counters() {
  local phase="$1" status=0
  COUNTERS="$(counters "$phase")" || status=$?
  [ "$status" -eq 0 ] && return 0
  if never_arrived "$status"; then
    inconclusive "the shaper's counters at the $phase boundary were never asked for: $NEVER_ARRIVED"
  fi
  inconclusive "the shaper stopped answering at the $phase boundary"
}

# The shaper's totals run for its process life, so each scenario records its opening totals.
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

# The machine's own row, as a technician's device list shows it.
device_row() {
  local body
  body="$(api_get "/api/v1/devices")" || return 1
  jq -e --arg id "$DEVICE_ID" '.[] | select(.id == $id)' <<<"$body"
}

# The status reported when the drill could not take a reading; it is never a machine state.
UNREADABLE_STATUS="unreadable"

# The machine's status from the device list, or UNREADABLE_STATUS for a failed request,
# a missing machine or an empty reply.
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

# The share of the window's chart buckets the machine reported for, read from the chart endpoint.
# Every bucket of the window is present, and an unreported one is null.
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

# The machine's own clock puts both ends of a reconnect figure on one clock.
machine_clock_now() {
  kubectl -n "$NAMESPACE" exec "$MACHINE_POD" -- date -u +%s.%N 2>/dev/null
}

# What the machine wrote from the given moment onward.
machine_log_since() {
  local since="$1"
  kubectl -n "$NAMESPACE" logs "$MACHINE_POD" --timestamps --since-time="$since" 2>/dev/null
}

# The kubectl stamp on a log line as seconds; it needs no assumption about the agent's formatter.
log_epoch() {
  local stamp
  stamp="${1%% *}"
  [ -n "$stamp" ] || return 1
  date -u -d "$stamp" +%s.%N 2>/dev/null
}

at_or_after() {
  awk -v a="$1" -v b="$2" 'BEGIN { exit !(a + 0 >= b + 0) }'
}

# Prints "waited spent": seconds from the link's return and from the last failed attempt to return.
# Fails on an unreadable log or no return; spent is empty when the first try worked.
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

# How many of this run's simulated machines the server has online, read from the device list.
# The shaper's machines field is an idle-mapping expiry that lags the herd.
fleet_online() {
  local body
  body="$(api_get "/api/v1/devices")" || return 1
  jq -r --arg p "${FLEET_PREFIX}-" \
    '[ .[] | select((.hostname // "") | startswith($p)) | select(.status == "online") ] | length' \
    <<<"$body" 2>/dev/null
}

# The first scenario's outage length, shifted by the run's seed; a collapsed clock stays collapsed.
outage_seconds() {
  if [ "$FAULT_SECONDS" -le 0 ] || [ "$FAULT_SPREAD_SECONDS" -le 0 ]; then
    printf '%s\n' "$FAULT_SECONDS"
    return 0
  fi
  printf '%s\n' "$((FAULT_SECONDS - FAULT_SPREAD_SECONDS / 2 + SHAPER_SEED % FAULT_SPREAD_SECONDS))"
}

# A machine holds an alert raised while offline and offers it when the link returns.
# The rule is tuned against the machine's own disk reading, so the drill never guesses a line.

# The armed rule is one the product ships, so the alert it raises is one the product accepts.
ALERT_DRILL_RULE="disk-critical"
# The shortest hold the rule allows, so the firing lands inside the outage.
ALERT_DRILL_SUSTAIN=60
# The line sits this far under the machine's reading so a one-point drift does not clear it.
ALERT_DRILL_MARGIN=5
# The lowest line the rule may be tuned to; a machine whose disk is below it cannot be armed.
ALERT_DRILL_FLOOR=50
# The customer whose rule this run tuned, kept for the teardown that runs after the scenario.
ARMED_ORG=""

api_post() {
  probe_curl -X POST -H "Authorization: Bearer ${API_TOKEN:-}" \
    -H 'Content-Type: application/json' --data "$2" "$SERVER_URL$1"
}

api_put() {
  probe_curl -X PUT -H "Authorization: Bearer ${API_TOKEN:-}" \
    -H 'Content-Type: application/json' --data "$2" "$SERVER_URL$1"
}

# The fullest mount on the machine as a whole percentage, as its sampler compares to the rule.
machine_disk_percent() {
  kubectl -n "$NAMESPACE" exec "$MACHINE_POD" -- \
    sh -c "df -P | awk 'NR>1 {gsub(/%/,\"\",\$5); if (\$5+0 > m) m=\$5+0} END {print m+0}'" 2>/dev/null
}

# The customer the machine belongs to, whose rule is tuned.
machine_organization() {
  device_row | jq -r '.organization_id // empty' 2>/dev/null
}

# Tunes the rule to a line the machine is already past, held for the shortest span allowed.
# The change reaches the machine over its open connection, so nothing restarts.
arm_the_alert() {
  local org="$1" line="$2"
  ARMED_ORG="$org"
  api_put "/api/v1/rules/$ALERT_DRILL_RULE/bindings?organization_id=$org" \
    "$(jq -cn --arg org "$org" --argjson line "$line" --argjson hold "$ALERT_DRILL_SUSTAIN" \
      '{level: "organization", level_key: $org, params: {threshold: $line, sustain_secs: $hold}}')" \
    >/dev/null
}

# Restores the rule's shipped binding whatever the scenario found.
disarm_the_alert() {
  local org="$ARMED_ORG"
  [ -n "$org" ] || return 0
  ARMED_ORG=""
  probe_curl -X DELETE -H "Authorization: Bearer ${API_TOKEN:-}" \
    "$SERVER_URL/api/v1/rules/$ALERT_DRILL_RULE/bindings?organization_id=$org&level=organization&level_key=$org" \
    >/dev/null 2>&1 || true
}

# How many rooms this machine's alerts have opened for the armed rule.
rooms_for_the_armed_rule() {
  local body
  body="$(api_get "/api/v1/investigations?device_id=$DEVICE_ID&rule_id=$ALERT_DRILL_RULE")" || return 1
  jq -r '[.items[]?] | length' <<<"$body"
}

# Waits up to the recovery budget for the offline alert to arrive; prints seconds taken, or fails.
wait_until_the_alert_arrives() {
  local from="$1" budget="$2" deadline rooms
  deadline=$(($(date +%s) + budget))
  while [ "$(date +%s)" -le "$deadline" ]; do
    rooms="$(rooms_for_the_armed_rule)" || rooms=""
    if [ -n "$rooms" ] && [ "$rooms" -gt 0 ] 2>/dev/null; then
      printf '%s\n' "$(($(date +%s) - from))"
      return 0
    fi
    sleep "$POLL_SECONDS"
  done
  return 1
}

# Appends one measurement row; the trend is sliced by its labels, and an empty value is skipped.
emit() {
  local metric="$1" victim="$2" value="$3"
  [ -n "$value" ] || return 0
  jq -cn \
    --arg metric "$metric" --arg scenario "$SCENARIO" --arg victim "$victim" \
    --arg commit "$COMMIT_SHA" --argjson value "$value" \
    '{metric: $metric, scenario: $scenario, victim: $victim, commit: $commit, env: "ci", value: $value}' \
    >>"$PENDING_ROWS"
}

# The shaper's account of what the scenario did to the link, kept beside the trend.
emit_shaper_counters() {
  local body="$1"
  emit netdrill_shaper_dropped_to_server link "$(dropped_since_opening "$body" to_server)"
  emit netdrill_shaper_dropped_to_machine link "$(dropped_since_opening "$body" to_machine)"
}

# Publishes the two reconnect figures when the log has them; an unreadable log publishes neither.
# The attempt figure comes only from a machine that failed an attempt.
emit_reconnect_figures() {
  local since="$1" restored_epoch="$2" figures waited spent
  figures="$(reconnect_from_machine_log "$since" "$restored_epoch")" || return 0
  read -r waited spent <<<"$figures"
  emit netdrill_reconnect_seconds real "$waited"
  [ -n "$spent" ] && emit netdrill_reconnect_attempt_seconds real "$spent"
  return 0
}

# Whether the machine came back at all; an empty status poll answer means it never returned.
emit_reconnected() {
  local online_at="$1"
  if [ -n "$online_at" ]; then
    emit netdrill_reconnected real 1
  else
    emit netdrill_reconnected real 0
  fi
}

# A scenario whose drops since opening are zero did not run its fault.
require_dropped() {
  local body="$1"
  [ "$(dropped_since_opening "$body" to_server)" -gt 0 ] \
    || inconclusive "the shaper dropped nothing toward the server while this scenario held the link, so the fault it was told to apply did not reach it"
}

# The only writer of the measurements file, run once after every reading is taken.
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

PASS_THROUGH='{}'
BLACKHOLE='{"blackhole":true}'
# A fifth of what the machine sends is lost and nothing in the direction it receives.
ONE_WAY_LOSS='{"loss_to_server":0.2}'
# A third of a second each way.
SATELLITE='{"delay_each_way_ms":300}'
# One 2 Mbit/s link shared by every machine, buffering a second before it drops.
THIN_UPLINK='{"rate_bits_per_sec":2000000,"max_queue_ms":1000}'

# S1 is a three-minute outage then a healthy link; it exceeds the 90 s idle timeout, so the
# connection dies and exercises reconnect.
run_s1() {
  local dark_from dark_to restored restored_epoch online_at ratio filled_at outage
  local org disk line replayed_in

  impair "$PASS_THROUGH"
  open_counters
  hold "$BASELINE_SECONDS"

  # The rule is armed against this machine's own disk; below the floor the scenario is inconclusive.
  org="$(machine_organization)" \
    || inconclusive "the drill could not read which customer its machine belongs to"
  [ -n "$org" ] \
    || inconclusive "the drill's machine belongs to no customer, so no rule can be tuned for it"
  disk="$(machine_disk_percent)" \
    || inconclusive "the drill could not read its machine's own disk"
  line=$((disk - ALERT_DRILL_MARGIN))
  [ "$line" -ge "$ALERT_DRILL_FLOOR" ] 2>/dev/null \
    || inconclusive "this machine's fullest mount is at ${disk}%, and the rule cannot be tuned below ${ALERT_DRILL_FLOOR}% — there is no line here it is already past"
  trap 'disarm_the_alert; clear_the_link' EXIT
  arm_the_alert "$org" "$line" \
    || inconclusive "the drill could not tune the rule it measures the replay with"
  emit netdrill_alert_line real "$line"

  outage="$(outage_seconds)"
  dark_from="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  impair "$BLACKHOLE"
  hold "$outage"
  read_counters fault
  require_dropped "$COUNTERS"
  dark_to="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  impair "$PASS_THROUGH"
  restored="$(date +%s)"
  # The machine's clock is read first because the recovery figures are measured from it.
  restored_epoch="$(machine_clock_now || true)"
  # The outage length this night ran is published so its figures are read against it.
  emit netdrill_outage_seconds link "$outage"

  online_at="$(wait_until_online "$restored" "$RECOVERY_SECONDS")" \
    || inconclusive "the drill never read the machine's status while waiting for it to come back"
  emit_reconnected "$online_at"
  emit_reconnect_figures "$dark_from" "$restored_epoch"

  filled_at="$(wait_until_filled "$restored" "$dark_from" "$dark_to" "$RECOVERY_SECONDS")"
  emit netdrill_backfill_complete_seconds real "$filled_at"

  ratio="$(gap_fill_ratio "$dark_from" "$dark_to" || echo 0)"
  emit netdrill_gap_fill_ratio real "$ratio"

  # The alert is held through the outage and offered on return, so the measure is its survival.
  if replayed_in="$(wait_until_the_alert_arrives "$restored" "$RECOVERY_SECONDS")"; then
    emit netdrill_alerts_replayed real 1
    emit netdrill_alert_replay_seconds real "$replayed_in"
  else
    emit netdrill_alerts_replayed real 0
  fi

  read_counters recovery
  emit_shaper_counters "$COUNTERS"
  publish
}

# S2 is the same outage recovered over a thin shared uplink; live staleness measures a catch-up
# batch sitting ahead of the next heartbeat on the machine's one ordered stream.
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

  # Staleness is measured once the machine is back, so it reflects the catch-up and not the outage.
  back="$(wait_until_online "$restored" "$RECOVERY_SECONDS")" \
    || inconclusive "the drill never read the machine's status while waiting for it to come back over the thin uplink"
  emit_reconnected "$back"
  emit_reconnect_figures "$dark_from" "$restored_epoch"
  [ -n "$back" ] || inconclusive "the machine never came back over the thin uplink, so there was no catch-up to watch"

  # The herd must be back too; four machines draining ask for nearly twice the link's capacity.
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

# S3 keeps the connection up while it loses a fifth of what the machine sends.
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

# S4 is a slow link, then a machine returning on a new address; survival and reconnection are both
# recorded, since a failed migration looks like an ordinary outage after the idle timeout.
run_s4() {
  local before after survived reconnected transitions watched

  impair "$PASS_THROUGH"
  open_counters
  hold "$BASELINE_SECONDS"

  impair "$SATELLITE"
  hold "$FAULT_SECONDS"
  read_counters fault

  # Both verdicts compare these readings to "online", so an unreadable one is inconclusive.
  before="$(device_status)"
  [ "$before" != "$UNREADABLE_STATUS" ] \
    || inconclusive "the drill could not read the machine's status before the address change"
  rebind
  # Watching the window catches a drop and reconnect that two endpoint readings would miss.
  watched="$(watch_liveness "$RECOVERY_SECONDS")" \
    || inconclusive "the drill never read the machine's status after the address change"
  read -r _ transitions <<<"$watched"
  after="$(device_status)"
  [ "$after" != "$UNREADABLE_STATUS" ] \
    || inconclusive "the drill could not read the machine's status after the address change"

  # The session survived if the machine never left; it reconnected if it left and returned.
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

# Prints seconds from the link's restoration to the machine being online; empty at the budget.
# Fails when no status was ever readable, since a blind poll proves nothing about the return.
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

# Prints seconds from the link's restoration to the chart gap being filled; empty at the budget.
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

# Prints the worst last_seen age and the count of offline crossings over a window.
# Fails when no status was ever readable, since an unobserved window looks like good behaviour.
watch_liveness() {
  local budget="$1" deadline now seen worst=0 transitions=0 previous="online" status age readings=0
  deadline=$(($(date +%s) + budget))
  while :; do
    now="$(date +%s)"
    status="$(device_status)"
    # An unreadable status adds no sample, so the last real reading stands.
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

shaper_healthy || inconclusive "the shaper at $SHAPER_URL (pod $SHAPER_POD) is not answering"

case "$SCENARIO" in
  s1) run_s1 ;;
  s2) run_s2 ;;
  s3) run_s3 ;;
  s4) run_s4 ;;
  *) die "no such scenario: '$SCENARIO' — the drill runs s1, s2, s3 and s4" ;;
esac
