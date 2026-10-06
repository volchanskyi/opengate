#!/usr/bin/env bash
# Holds the QUIC fleet in a detached process in the staging pod and reads its verdict back.
# A refused launch is repeated; a launch that happened is never made twice.
#
# Environment:
#   LOADTEST_POD                            pod holding /tmp/loadtest (required)
#   NAMESPACE                               namespace it runs in (default opengate-staging)
#   LOADTEST_QUIC_POD_LOG                   harness output inside the pod
#   LOADTEST_QUIC_POD_STATUS                harness exit code inside the pod
#   LOADTEST_QUIC_START_ATTEMPTS            launches to make before giving up
#   LOADTEST_QUIC_START_TIMEOUT_SECONDS     wait for the harness to offer its fleet
#   LOADTEST_QUIC_FILED_TIMEOUT_SECONDS     wait for the harness to file its estate
#   LOADTEST_QUIC_COLLECT_TIMEOUT_SECONDS   wait for the harness to reach a verdict
#   LOADTEST_QUIC_POLL_SECONDS              gap between questions
#
# Usage:
#   loadtest-quic-incluster.sh start -- <harness command...>
#   loadtest-quic-incluster.sh await-filed
#   loadtest-quic-incluster.sh walk-started-at
#   loadtest-quic-incluster.sh collect
set -euo pipefail

# The harness prints this once its fixture is built and it is about to offer its fleet.
FLEET_ANNOUNCEMENT='Starting QUIC load test'

# The harness prints this once every machine is filed under its customer and building.
FILED_ANNOUNCEMENT='Estate filed'

# The harness prints this when it starts walking the profile, followed by the start second.
WALK_ANNOUNCEMENT='Walk started at'

# NO_FLEET is the verdict when nothing launched or nothing reached a verdict in the bound.
# It lies outside the harness's own exit codes (0, 1, 2), so the runner reads it as an abort.
NO_FLEET=4

POD="${LOADTEST_POD:-}"
NAMESPACE="${NAMESPACE:-opengate-staging}"
POD_LOG="${LOADTEST_QUIC_POD_LOG:-/tmp/loadtest-fleet.log}"
POD_STATUS="${LOADTEST_QUIC_POD_STATUS:-/tmp/loadtest-fleet.status}"
START_ATTEMPTS="${LOADTEST_QUIC_START_ATTEMPTS:-3}"
START_TIMEOUT="${LOADTEST_QUIC_START_TIMEOUT_SECONDS:-600}"
# Filing follows the arrivals, so this bound covers a ramp as well as a fixture build.
FILED_TIMEOUT="${LOADTEST_QUIC_FILED_TIMEOUT_SECONDS:-900}"
COLLECT_TIMEOUT="${LOADTEST_QUIC_COLLECT_TIMEOUT_SECONDS:-1500}"
POLL="${LOADTEST_QUIC_POLL_SECONDS:-5}"

usage() {
  echo "usage: $0 start -- <harness command...>" >&2
  echo "       $0 await-filed" >&2
  echo "       $0 walk-started-at" >&2
  echo "       $0 collect" >&2
}

# pod_sh runs one short script inside the pod.
pod_sh() {
  # Not retried: each caller loops over it already or reads a blank as the answer.
  kubectl -n "$NAMESPACE" exec "$POD" -- sh -c "$1"
}

# The launcher detaches the harness in the pod and records its output and exit code.
# A pod already holding a fleet says so and starts nothing.
read -r -d '' LAUNCHER <<'LAUNCHER_EOF' || true
log="$1"
status="$2"
shift 2
if [ -e "$log" ]; then
  echo "fleet already launched"
  exit 0
fi
rm -f "$status"
: >"$log"
export QUIC_LOG="$log" QUIC_STATUS="$status"
inner='"$@" >>"$QUIC_LOG" 2>&1; echo $? >"$QUIC_STATUS"'
if command -v setsid >/dev/null 2>&1; then
  setsid sh -c "$inner" sh "$@" >/dev/null 2>&1 &
else
  nohup sh -c "$inner" sh "$@" >/dev/null 2>&1 &
fi
echo "fleet launched"
LAUNCHER_EOF

# pod_holds_fleet answers whether a harness was ever started in this pod, and returns 2 when
# the pod did not answer. The pod replies with a word, since `test -e` and kubectl both exit 1.
pod_holds_fleet() {
  local attempt answer
  for attempt in 1 2 3; do
    answer="$(pod_sh "if [ -e '$POD_LOG' ]; then echo held; else echo none; fi" 2>/dev/null || true)"
    # The last line is the pod's word; a client may print its own lines first.
    answer="${answer##*$'\n'}"
    case "$answer" in
      held) return 0 ;;
      none) return 1 ;;
    esac
    sleep "$POLL"
  done
  echo "::error::$POD did not answer whether it holds a fleet, so this run will not start a second one over the first one's fixture." >&2
  return 2
}

pod_log() {
  pod_sh "cat '$POD_LOG' 2>/dev/null" 2>/dev/null || true
}

pod_status() {
  pod_sh "cat '$POD_STATUS' 2>/dev/null" 2>/dev/null | tr -d '[:space:]' || true
}

# launch makes one attempt and says whether the pod is now holding a harness.
launch() {
  # Not retried: the caller's loop re-reads what the pod holds before each attempt.
  if kubectl -n "$NAMESPACE" exec "$POD" -- \
    sh -c "$LAUNCHER" loadtest-fleet-launcher "$POD_LOG" "$POD_STATUS" "$@"; then
    return 0
  fi
  echo "::warning::the launch of the QUIC fleet was refused before it reached $POD." >&2
  # The pod is asked, since a refusal does not say what the pod did with the command.
  pod_holds_fleet
}

# await_fleet waits for the fleet announcement and stops early once the harness has a verdict.
await_fleet() {
  local deadline=$((SECONDS + START_TIMEOUT))
  local status log
  while [ "$SECONDS" -lt "$deadline" ]; do
    # The log is read into a variable, since `grep -q` exits at its first match under pipefail.
    log="$(pod_log)"
    if grep -qF "$FLEET_ANNOUNCEMENT" <<<"$log"; then
      echo "the QUIC fleet is holding in $POD"
      return 0
    fi
    status="$(pod_status)"
    if [ -n "$status" ]; then
      echo "::error::the QUIC harness exited ($status) before it offered a fleet." >&2
      pod_log >&2
      return "$NO_FLEET"
    fi
    sleep "$POLL"
  done
  echo "::error::the QUIC harness did not offer a fleet within ${START_TIMEOUT}s." >&2
  pod_log >&2
  return "$NO_FLEET"
}

# await_filed waits for the estate announcement, which follows the arrivals, and stops early
# once the harness has a verdict. Scenarios that read a building need the estate filed first.
await_filed() {
  local deadline=$((SECONDS + FILED_TIMEOUT))
  local status
  while [ "$SECONDS" -lt "$deadline" ]; do
    if grep -qF "$FILED_ANNOUNCEMENT" <<<"$(pod_log)"; then
      echo "the estate is filed in $POD"
      return 0
    fi
    status="$(pod_status)"
    if [ -n "$status" ]; then
      echo "::error::the QUIC harness exited ($status) before it filed its estate." >&2
      pod_log >&2
      return "$NO_FLEET"
    fi
    sleep "$POLL"
  done
  echo "::error::the estate was not filed within ${FILED_TIMEOUT}s, so a scoped read would find an empty building." >&2
  pod_log >&2
  return "$NO_FLEET"
}

# walk_started_at prints the second the harness started walking and fails when the log has none.
walk_started_at() {
  local announced line
  announced="$(grep -F "$WALK_ANNOUNCEMENT" <<<"$(pod_log)" || true)"
  line="${announced##*$'\n'}"
  if [[ ! "$line" =~ ([0-9]+)[[:space:]]*$ ]]; then
    echo "::error::$POD has not said when it started walking, so a generator cannot join the walk where it is." >&2
    return "$NO_FLEET"
  fi
  printf '%s\n' "${BASH_REMATCH[1]}"
}

start() {
  if [ "$#" -eq 0 ]; then
    usage
    return 2
  fi
  if [ "$1" = "--" ]; then
    shift
  fi
  if [ "$#" -eq 0 ]; then
    usage
    return 2
  fi

  local attempt=1 holds
  while :; do
    holds=0
    launch "$@" || holds=$?
    case "$holds" in
      0) break ;;
      1)
        # Nothing was started, so the launch is safe to repeat.
        if [ "$attempt" -ge "$START_ATTEMPTS" ]; then
          echo "::error::the QUIC fleet was never launched: $START_ATTEMPTS attempts were refused before reaching $POD." >&2
          return "$NO_FLEET"
        fi
        echo "::warning::nothing was started in $POD, so the launch is being made again (attempt $((attempt + 1)) of $START_ATTEMPTS)." >&2
        attempt=$((attempt + 1))
        sleep "$POLL"
        ;;
      *) return "$NO_FLEET" ;;
    esac
  done

  await_fleet
}

collect() {
  local holds=0
  pod_holds_fleet || holds=$?
  if [ "$holds" -eq 1 ]; then
    echo "::error::no fleet was launched in $POD, so this run has no QUIC measurement to collect." >&2
    return "$NO_FLEET"
  fi
  # Any other status means the pod could not be asked.
  if [ "$holds" -ne 0 ]; then
    return "$NO_FLEET"
  fi

  local deadline=$((SECONDS + COLLECT_TIMEOUT))
  local status
  while [ "$SECONDS" -lt "$deadline" ]; do
    status="$(pod_status)"
    if [ -n "$status" ]; then
      pod_log
      return "$status"
    fi
    sleep "$POLL"
  done

  pod_log
  echo "::error::the QUIC harness in $POD reached no verdict within ${COLLECT_TIMEOUT}s." >&2
  return "$NO_FLEET"
}

main() {
  if [ "$#" -eq 0 ]; then
    usage
    return 2
  fi
  local verb="$1"
  shift

  if [ -z "$POD" ]; then
    echo "loadtest-quic-incluster: LOADTEST_POD must name the staged QUIC pod" >&2
    return 2
  fi

  case "$verb" in
    start) start "$@" ;;
    await-filed) await_filed ;;
    walk-started-at) walk_started_at ;;
    collect) collect "$@" ;;
    *)
      usage
      return 2
      ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
