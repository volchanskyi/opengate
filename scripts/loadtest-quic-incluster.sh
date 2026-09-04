#!/usr/bin/env bash
# Hold the QUIC fleet inside the staging cluster, and read its verdict back.
#
# The fleet has to stay connected for the whole k6 window, because the relay
# scenario opens the operator's side of a real session and a session needs a
# machine on the other end of it. Held on one `kubectl exec` running for that
# whole window, the fleet lives and dies with a stream three hops long — runner,
# API server, kubelet — and the run has no account of it either way.
#
# So the harness is launched detached in the pod and left there. The launch is a
# call that returns in a moment; what it started is a process the pod owns, and
# every later question about it — is it offering a fleet, what did it decide,
# what did it print — is a fresh call that can be asked again if the first one
# does not arrive.
#
# Two properties follow, and they are the reason for the shape:
#
#   * A launch that never happened is made again. The API server's request to
#     the kubelet can be refused outright, and a harness that never ran built no
#     fixture, so making the call again is safe. The decision is taken by asking
#     the pod what it holds — never by reading the error text, which changes with
#     the client and says nothing about what the pod did.
#   * A launch that happened is never made twice. A second harness would build a
#     second fixture over the first one's names, and a customer's name is unique
#     inside its tenant, so the second run would be refused partway through and
#     leave the fleet it was supposed to double.
#
# `start` returns only once the harness has said it is offering its fleet. A
# sleep in its place reports success on a fleet that never started, and the
# scenarios beside it then measure a system with nothing connected to it.
#
# Environment:
#   LOADTEST_POD                            pod holding /tmp/loadtest (required)
#   NAMESPACE                               namespace it runs in (default opengate-staging)
#   LOADTEST_QUIC_POD_LOG                   harness output inside the pod
#   LOADTEST_QUIC_POD_STATUS                harness exit code inside the pod
#   LOADTEST_QUIC_START_ATTEMPTS            launches to make before giving up
#   LOADTEST_QUIC_START_TIMEOUT_SECONDS     wait for the harness to offer its fleet
#   LOADTEST_QUIC_COLLECT_TIMEOUT_SECONDS   wait for the harness to reach a verdict
#   LOADTEST_QUIC_POLL_SECONDS              gap between questions
#
# Usage:
#   loadtest-quic-incluster.sh start -- <harness command...>
#   loadtest-quic-incluster.sh collect
set -euo pipefail

# The line the harness prints once its fixture is built and it is about to offer
# its fleet. It is the harness's own account of itself, which is what makes it a
# readiness signal rather than a guess about how long a fixture takes.
FLEET_ANNOUNCEMENT='Starting QUIC load test'

# NO_FLEET is the verdict when there is no run to report on — nothing was
# launched, or nothing reached a verdict inside the bound. It is deliberately
# outside the harness's own vocabulary (0 clean, 1 some agents failed, 2 the run
# measured nothing), so scripts/loadtest-quic-run.sh reads it as an abort and
# discards whatever output exists.
NO_FLEET=4

POD="${LOADTEST_POD:-}"
NAMESPACE="${NAMESPACE:-opengate-staging}"
POD_LOG="${LOADTEST_QUIC_POD_LOG:-/tmp/loadtest-fleet.log}"
POD_STATUS="${LOADTEST_QUIC_POD_STATUS:-/tmp/loadtest-fleet.status}"
START_ATTEMPTS="${LOADTEST_QUIC_START_ATTEMPTS:-3}"
START_TIMEOUT="${LOADTEST_QUIC_START_TIMEOUT_SECONDS:-600}"
COLLECT_TIMEOUT="${LOADTEST_QUIC_COLLECT_TIMEOUT_SECONDS:-1500}"
POLL="${LOADTEST_QUIC_POLL_SECONDS:-5}"

usage() {
  echo "usage: $0 start -- <harness command...>" >&2
  echo "       $0 collect" >&2
}

# pod_sh runs one short script inside the pod. Every question this shim asks is
# one of these, so a call that does not arrive costs the answer rather than the
# run.
pod_sh() {
  kubectl -n "$NAMESPACE" exec "$POD" -- sh -c "$1"
}

# The launcher, run inside the pod. It detaches the harness from the exec that
# started it and writes down both halves of what the run will be asked for: what
# the harness printed, and what it exited with.
#
# The guard is what makes a repeated launch safe: a pod already holding a fleet
# says so and starts nothing.
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

# pod_holds_fleet answers whether a harness was ever started in this pod, and
# refuses to guess when it could not ask. A guard that answers yes when the
# question did not arrive is the false green it exists to close, and here the
# wrong answer starts a second fixture over the first one's names.
pod_holds_fleet() {
  local attempt
  for attempt in 1 2 3; do
    if pod_sh "test -e '$POD_LOG'" >/dev/null 2>&1; then
      return 0
    fi
    # A pod that answered "no such file" and a call that never arrived are the
    # same exit code here, so the second question is whether the pod is
    # answering at all.
    if pod_sh "true" >/dev/null 2>&1; then
      return 1
    fi
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
  if kubectl -n "$NAMESPACE" exec "$POD" -- \
    sh -c "$LAUNCHER" loadtest-fleet-launcher "$POD_LOG" "$POD_STATUS" "$@"; then
    return 0
  fi
  echo "::warning::the launch of the QUIC fleet was refused before it reached $POD." >&2
  # The refusal says nothing about what the pod did with the command, so the pod
  # is asked.
  pod_holds_fleet
}

# await_fleet waits for the harness's own announcement, and stops early when the
# harness has already reached a verdict — a run that exited before offering a
# fleet has an answer, and it is in its output.
await_fleet() {
  local deadline=$((SECONDS + START_TIMEOUT))
  local status
  while [ "$SECONDS" -lt "$deadline" ]; do
    if pod_log | grep -qF "$FLEET_ANNOUNCEMENT"; then
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
        # Nothing was started, so nothing was built and the call is safe to make
        # again.
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
  # Anything else means the pod could not be asked, and pod_holds_fleet has
  # already said so.
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
