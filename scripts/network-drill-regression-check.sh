#!/usr/bin/env bash
# Gate the canonical network-drill rows against a VictoriaMetrics read-back
# window, plus absolute floors that hold whether or not the window exists.
#
# The drill never blocks a deploy. What this decides is whether the nightly goes
# red and raises a Telegram alert — a slow recovery on a shared two-processor
# node is a trend to read, not a reason to stop shipping.
#
# Two comparisons, in this order:
#
#   1. The window. Fourteen days of this series, at least three samples, and a
#      current value more than the band past the median.
#   2. The floors. These hold from night one, and they are what the check
#      enforces on its own until the window exists. Which of the two applied is
#      stated in the output rather than left for a reader to infer.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/vm-query.sh
. "$SCRIPT_DIR/lib/vm-query.sh"

COMMIT_SHA="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
export VM_EXCLUDE_COMMIT="${VM_EXCLUDE_COMMIT:-$COMMIT_SHA}"

WINDOW_DAYS=14
MIN_WINDOW_SAMPLES=3

# The bands are not yet calibrated: they are written the night the first two
# weeks of runs exist, beside the two readings that bracket them. Until then
# this check enforces the floors alone and says so, rather than comparing
# against a median drawn from one or two nights of a shared cluster.
BANDS_CALIBRATED="${NETDRILL_BANDS_CALIBRATED:-0}"
RECONNECT_REL_TOL=2.0
STALENESS_REL_TOL=2.0

# The floors. Each is the product behaviour a customer would notice losing.
FLOOR_RECONNECT_SECONDS=120
FLOOR_GAP_FILL_RATIO=0.95
FLOOR_OFFLINE_TRANSITIONS=0

SUMMARY_FILE="${1:-netdrill-summary.json}"
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

# The window median and sample count for one series, or nothing when the series
# has neither. Fail-open by construction: vm-query answers an unreachable
# VictoriaMetrics with silence, and a gate that reddened on infrastructure would
# be turned off within a week.
window_median() {
  local metric="$1" scenario="$2" victim="$3" selector line
  selector="${metric}{$(vm_query_selector "env=\"ci\",scenario=\"${scenario}\",victim=\"${victim}\"")}"
  line="$(vm_query_window "quantile(0.5, median_over_time(${selector}[${WINDOW_DAYS}d]))" | head -1)"
  printf '%s\n' "${line##*$'\t'}"
}

window_count() {
  local metric="$1" scenario="$2" victim="$3" selector line
  selector="${metric}{$(vm_query_selector "env=\"ci\",scenario=\"${scenario}\",victim=\"${victim}\"")}"
  line="$(vm_query_window "count(count_over_time(${selector}[${WINDOW_DAYS}d]))" | head -1)"
  printf '%s\n' "${line##*$'\t'}"
}

# A value that has grown past its own history by more than the band. Only
# applied once the bands are calibrated, and only where the window is deep
# enough to have a median worth comparing to.
check_window_growth() {
  local metric="$1" scenario="$2" victim="$3" current="$4" tolerance="$5"
  [ "$BANDS_CALIBRATED" = "1" ] || return 0

  local median count threshold
  median="$(window_median "$metric" "$scenario" "$victim")"
  count="$(window_count "$metric" "$scenario" "$victim")"
  [ -n "$median" ] && num_ge "${count:-0}" "$MIN_WINDOW_SAMPLES" && num_pos "$median" || return 0

  threshold="$(mul "$median" "$(awk -v t="$tolerance" 'BEGIN { printf "%.6f", 1 + t }')")"
  if num_gt "$current" "$threshold"; then
    REGRESSIONS+=("${scenario}/${victim} ${metric}: ${median} -> ${current} (past the ${WINDOW_DAYS}-day median by more than the band)")
  fi
}

check_floor() {
  local metric="$1" scenario="$2" victim="$3" current="$4"
  case "$metric" in
    netdrill_reconnect_seconds)
      num_gt "$current" "$FLOOR_RECONNECT_SECONDS" \
        && REGRESSIONS+=("${scenario}/${victim} ${metric}: ${current}s against a floor of ${FLOOR_RECONNECT_SECONDS}s — a machine that goes dark has to come back on its own")
      ;;
    netdrill_gap_fill_ratio)
      num_lt "$current" "$FLOOR_GAP_FILL_RATIO" \
        && REGRESSIONS+=("${scenario}/${victim} ${metric}: ${current} against a floor of ${FLOOR_GAP_FILL_RATIO} — the hole the outage left in the customer's charts did not fill")
      ;;
    netdrill_offline_transitions)
      # A machine on a bad link, or catching up over a thin one, must not churn.
      # Crossing the offline line at all in those scenarios is the failure the
      # scenario exists to find.
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
  esac
  return 0
}

while IFS=$'\t' read -r metric scenario victim value; do
  [ -n "$metric" ] || continue
  case "$metric" in
    netdrill_reconnect_seconds) check_window_growth "$metric" "$scenario" "$victim" "$value" "$RECONNECT_REL_TOL" ;;
    netdrill_live_staleness_max_seconds) check_window_growth "$metric" "$scenario" "$victim" "$value" "$STALENESS_REL_TOL" ;;
  esac
  check_floor "$metric" "$scenario" "$victim" "$value"
done < <(jq -r '.[] | [.metric, .scenario, .victim, .value] | @tsv' "$SUMMARY_FILE")

if [ "$BANDS_CALIBRATED" = "1" ]; then
  echo "network-drill: checked against the ${WINDOW_DAYS}-day window and the absolute floors"
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
