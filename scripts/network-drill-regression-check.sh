#!/usr/bin/env bash
# Gates network-drill rows against the median of the previous fourteen dates (three needed)
# and absolute floors; a regression reddens the nightly and alerts, and blocks no deploy.
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/vm-query.sh
. "$SCRIPT_DIR/lib/vm-query.sh"
# shellcheck source=scripts/lib/summary-table.sh
. "$SCRIPT_DIR/lib/summary-table.sh"

WINDOW_DAYS=14
MIN_WINDOW_SAMPLES=3

# Until the bands are calibrated from two weeks of runs, only the floors are enforced and the
# output says so.
BANDS_CALIBRATED="${NETDRILL_BANDS_CALIBRATED:-0}"
RECONNECT_REL_TOL=2.0
STALENESS_REL_TOL=2.0

# The reconnect floor is the worst healthy case: a 90 s establish that cannot finish plus a 30 s
# backoff cap; the attempt floor is the backoff cap plus an establish margin.
FLOOR_RECONNECT_SECONDS=120
FLOOR_RECONNECT_ATTEMPT_SECONDS=35
FLOOR_GAP_FILL_RATIO=0.95
FLOOR_OFFLINE_TRANSITIONS=0

SUMMARY_FILE="${1:-netdrill-summary.json}"
TABLE_FILE="${2:-}"
[[ -f "$SUMMARY_FILE" ]] || {
  echo "missing: $SUMMARY_FILE" >&2
  exit 2
}

num_gt() { awk -v a="$1" -v b="$2" 'BEGIN { exit !(a + 0 > b + 0) }'; }
num_lt() { awk -v a="$1" -v b="$2" 'BEGIN { exit !(a + 0 < b + 0) }'; }
num_ge() { awk -v a="$1" -v b="$2" 'BEGIN { exit !(a + 0 >= b + 0) }'; }
num_pos() { awk -v a="$1" 'BEGIN { exit !(a + 0 > 0) }'; }
mul() { awk -v a="$1" -v b="$2" 'BEGIN { printf "%.6f", a * b }'; }

REGRESSIONS=()

# The window median and date count for one series as "median<TAB>count", empty without history;
# vm-query answers an unreachable VictoriaMetrics with silence, so the gate fails open.
window_stats() {
  local metric="$1" scenario="$2" victim="$3"
  vm_nightly_window "$metric" "env=\"ci\",scenario=\"${scenario}\",victim=\"${victim}\"" "$WINDOW_DAYS" \
    | awk -F'\t' 'NR == 1 { print $2 "\t" $3 }'
}

# A value past its own history by more than the band; applies once the bands are calibrated and
# the window holds enough dates.
check_window_growth() {
  local metric="$1" scenario="$2" victim="$3" current="$4" tolerance="$5"
  [ "$BANDS_CALIBRATED" = "1" ] || return 0

  local stats median count threshold
  stats="$(window_stats "$metric" "$scenario" "$victim")"
  median="${stats%%$'\t'*}"
  count="${stats##*$'\t'}"
  [ -n "$median" ] && num_ge "${count:-0}" "$MIN_WINDOW_SAMPLES" && num_pos "$median" || return 0

  threshold="$(mul "$median" "$(awk -v t="$tolerance" 'BEGIN { printf "%.6f", 1 + t }')")"
  if num_gt "$current" "$threshold"; then
    REGRESSIONS+=("${scenario}/${victim} ${metric}: ${median} -> ${current} (past the median of the last ${WINDOW_DAYS} nights by more than the band)")
  fi
}

check_floor() {
  local metric="$1" scenario="$2" victim="$3" current="$4"
  case "$metric" in
    netdrill_reconnect_seconds)
      num_gt "$current" "$FLOOR_RECONNECT_SECONDS" \
        && REGRESSIONS+=("${scenario}/${victim} ${metric}: ${current}s against a floor of ${FLOOR_RECONNECT_SECONDS}s — a machine that goes dark has to come back on its own")
      ;;
    netdrill_reconnect_attempt_seconds)
      num_gt "$current" "$FLOOR_RECONNECT_ATTEMPT_SECONDS" \
        && REGRESSIONS+=("${scenario}/${victim} ${metric}: ${current}s against a floor of ${FLOOR_RECONNECT_ATTEMPT_SECONDS}s — the reconnect itself is slow, whatever the outage happened to interrupt")
      ;;
    netdrill_reconnected)
      num_lt "$current" 1 \
        && REGRESSIONS+=("${scenario}/${victim} ${metric}: the machine never came back inside the recovery window — a site that goes dark has to return on its own")
      ;;
    netdrill_gap_fill_ratio)
      num_lt "$current" "$FLOOR_GAP_FILL_RATIO" \
        && REGRESSIONS+=("${scenario}/${victim} ${metric}: ${current} against a floor of ${FLOOR_GAP_FILL_RATIO} — the hole the outage left in the customer's charts did not fill")
      ;;
    netdrill_offline_transitions)
      # A machine on a bad or thin link must not churn; any offline crossing there is the failure.
      case "$scenario" in
        s2 | s3)
          num_gt "$current" "$FLOOR_OFFLINE_TRANSITIONS" \
            && REGRESSIONS+=("${scenario}/${victim} ${metric}: ${current} against a floor of ${FLOOR_OFFLINE_TRANSITIONS} — the machine lost its connection where it was meant to hold it")
          ;;
      esac
      ;;
    netdrill_session_survived)
      num_lt "$current" 1 \
        && REGRESSIONS+=("${scenario}/${victim} ${metric}: the session did not survive the machine returning on a new address — every customer with a rebooting router carries a nightly gap")
      ;;
    netdrill_alerts_replayed)
      # An alert cannot be fetched afterwards: no history backs a signal and the machine cannot be
      # asked, so an outage that swallows one swallows the incident.
      num_lt "$current" 1 \
        && REGRESSIONS+=("${scenario}/${victim} ${metric}: the alert the machine raised while it was dark never arrived — every outage costs the incidents raised inside it, and nothing says so")
      ;;
  esac
  return 0
}

# floor_of prints "expected<TAB>words<TAB>unit" for a reading, with "no limit" where it is only
# recorded.
floor_of() {
  local metric="$1" scenario="$2"
  case "$metric" in
    netdrill_reconnect_seconds) printf '≤ %s s\ttime to come back\ts\n' "$FLOOR_RECONNECT_SECONDS" ;;
    netdrill_reconnect_attempt_seconds) printf '≤ %s s\ttime the reconnect itself took\ts\n' "$FLOOR_RECONNECT_ATTEMPT_SECONDS" ;;
    netdrill_reconnected) printf 'came back\twhether it came back\tflag\n' ;;
    netdrill_gap_fill_ratio) printf '≥ %s %%\tshare of the gap filled in\tshare\n' "$(awk -v f="$FLOOR_GAP_FILL_RATIO" 'BEGIN { printf "%g", f * 100 }')" ;;
    netdrill_offline_transitions)
      case "$scenario" in
        s2 | s3) printf '%s\ttimes the machine went offline\tcount\n' "$FLOOR_OFFLINE_TRANSITIONS" ;;
        *) printf 'no limit\ttimes the machine went offline\tcount\n' ;;
      esac
      ;;
    netdrill_session_survived) printf 'survived\twhether the session survived a new address\tflag\n' ;;
    netdrill_alerts_replayed) printf 'arrived\twhether the alert raised in the dark arrived\tflag\n' ;;
    *) printf 'no limit\t%s\tvalue\n' "${metric#netdrill_}" ;;
  esac
}

shown() {
  case "$2" in
    s) awk -v v="$1" 'BEGIN { printf "%g s", v }' ;;
    share) awk -v v="$1" 'BEGIN { printf "%g %%", v * 100 }' ;;
    flag) if awk -v v="$1" 'BEGIN { exit !(v + 0 >= 1) }'; then printf 'yes'; else printf 'no'; fi ;;
    *) awk -v v="$1" 'BEGIN { printf "%g", v }' ;;
  esac
}

TABLE_ROWS=()
while IFS=$'\t' read -r metric scenario victim value; do
  [ -n "$metric" ] || continue
  before="${#REGRESSIONS[@]}"
  case "$metric" in
    netdrill_reconnect_seconds) check_window_growth "$metric" "$scenario" "$victim" "$value" "$RECONNECT_REL_TOL" ;;
    netdrill_reconnect_attempt_seconds) check_window_growth "$metric" "$scenario" "$victim" "$value" "$RECONNECT_REL_TOL" ;;
    netdrill_live_staleness_max_seconds) check_window_growth "$metric" "$scenario" "$victim" "$value" "$STALENESS_REL_TOL" ;;
  esac
  check_floor "$metric" "$scenario" "$victim" "$value"
  IFS=$'\t' read -r expected words unit <<<"$(floor_of "$metric" "$scenario")"
  result="pass"
  [ "${#REGRESSIONS[@]}" -eq "$before" ] || result="FAIL"
  [ "$expected" != "no limit" ] || result="—"
  TABLE_ROWS+=("$scenario $victim: $words"$'\t'"$expected"$'\t'"$(shown "$value" "$unit")"$'\t'"$result")
done < <(jq -r '.[] | [.metric, .scenario, .victim, .value] | @tsv' "$SUMMARY_FILE")

if [ -n "$TABLE_FILE" ]; then
  {
    printf '### Network drill\n\n'
    summary_table_header
    for row in "${TABLE_ROWS[@]}"; do
      IFS=$'\t' read -r measurement expected actual result <<<"$row"
      summary_table_row "$measurement" "$expected" "$actual" "$result"
    done
    summary_legend
    printf -- '- **Measurement** — a scenario (s1 dark and back, s2 thin uplink, s3 lossy link, s4 new address) and the machine it measured: the real one, the herd, or the link itself.\n'
  } >"$TABLE_FILE"
fi

if [ "$BANDS_CALIBRATED" = "1" ]; then
  echo "network-drill: checked against the last ${WINDOW_DAYS} nights and the absolute floors"
else
  echo "network-drill: the trend window is not yet calibrated, so only the absolute floors were enforced"
fi

if [ "${#REGRESSIONS[@]}" -eq 0 ]; then
  echo "network-drill: no regression"
  exit 0
fi

echo "network-drill: regression" >&2
printf '  - %s\n' "${REGRESSIONS[@]}" >&2
exit 1
