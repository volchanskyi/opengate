#!/usr/bin/env bash
# One holder at a time over the staging namespace.
#
# The staging deploy and the nightly load run drive the same release. The deploy
# truncates the database, rolls the server and creates and deletes machines; the
# load run reads that same server, mints against an account the truncate
# removes, and asks the node for room for its own generator pods. Nothing
# stopped the two overlapping: they sit in different GitHub concurrency groups,
# and a group shared between them would be held for as long as a deploy waits on
# its reviewer — hours, on the record — so a nightly would be cancelled rather
# than delayed, silently, because a scheduled run is never retried.
#
# The lock therefore lives where the state does. A Lease in the namespace is
# held only while the work actually runs, covers a `kubectl` somebody types by
# hand and a workflow_dispatch as readily as the two schedules, and expires on
# its own if the holder dies without releasing.
#
# Environment:
#   NAMESPACE                     namespace holding the lease (required)
#   STAGING_LEASE_NAME            lease object name
#   STAGING_LEASE_TTL_SECONDS     how long a holder's claim outlives its last
#                                 write, so a job killed mid-run frees it
#   STAGING_LEASE_WAIT_SECONDS    how long `acquire` waits before giving up
#   STAGING_LEASE_POLL_SECONDS    gap between attempts
#   STAGING_LEASE_RENEW_SECONDS   gap between renewals while the claim is held
#   STAGING_LEASE_STATE_DIR       where the renewer's pid and its account of
#                                 itself are kept, for the release to read
#   STAGING_LEASE_KUBECTL         the kubectl to run; the tests pass a stand-in
#
# A claim is renewed for as long as its holder is working, because the duration
# it declares is far shorter than the work: forty-five minutes against a run
# that can be five hours. Past the duration any waiter may take the namespace
# from under a run still in progress, and both would then be driving the same
# server with neither of them knowing. So `acquire` leaves a renewer behind and
# `release` stops it — and a claim taken anyway is recorded, and fails the
# release, because a measurement taken on somebody else's namespace is not a
# measurement.
#
# Usage:
#   NAMESPACE=opengate-staging scripts/staging-lease.sh acquire "cd-run-1234"
#   NAMESPACE=opengate-staging scripts/staging-lease.sh renew "cd-run-1234"
#   NAMESPACE=opengate-staging scripts/staging-lease.sh release "cd-run-1234"
set -euo pipefail

LEASE_NAME="${STAGING_LEASE_NAME:-opengate-staging-guard}"
TTL_SECONDS="${STAGING_LEASE_TTL_SECONDS:-2700}"
WAIT_SECONDS="${STAGING_LEASE_WAIT_SECONDS:-1800}"
POLL_SECONDS="${STAGING_LEASE_POLL_SECONDS:-10}"
# A third of the duration, so two renewals can be missed — a slow API server, a
# runner that was descheduled — before the claim any waiter reads goes stale.
RENEW_SECONDS="${STAGING_LEASE_RENEW_SECONDS:-$((TTL_SECONDS / 3))}"
[ "$RENEW_SECONDS" -lt 5 ] && RENEW_SECONDS=5
STATE_DIR="${STAGING_LEASE_STATE_DIR:-${RUNNER_TEMP:-${TMPDIR:-/tmp}}}"
KUBECTL="${STAGING_LEASE_KUBECTL:-kubectl}"

: "${NAMESPACE:?NAMESPACE is required}"

# Where the renewer says who it is and what became of it. Both are per lease
# name, so a machine running two of these at once does not read the other's.
RENEWER_PID_FILE="$STATE_DIR/staging-lease-$LEASE_NAME.pid"
RENEWER_LOST_FILE="$STATE_DIR/staging-lease-$LEASE_NAME.lost"

now_epoch() { date -u +%s; }

# The Lease API decodes acquireTime and renewTime as MicroTime — RFC3339 with
# exactly six digits of fractional seconds — and refuses the whole object at
# decode when either is shaped any other way, before it reads a holder at all.
# The microseconds carry nothing this lock uses; the duration is whole seconds.
now_micro() { date -u +%Y-%m-%dT%H:%M:%S.000000Z; }

# Prints the lease as JSON, or nothing when it does not exist. A missing lease
# and a broken cluster are different answers, so only "not found" is swallowed.
#
# The two streams are kept apart. The credential plugin the cluster is reached
# through writes a warning to stderr on every call, whatever the verb and
# whatever the outcome, so a read that folds stderr into stdout hands its caller
# a line of prose in front of the object and every `jq` over it fails on a lease
# that is perfectly well formed. Only the failing arm reads stderr, where the
# server's reason for refusing is the whole of what there is to go on.
read_lease() {
  local out err err_file status=0
  err_file="$(mktemp)"
  # The assignment is the condition, so a non-zero kubectl does not end the
  # script the way a bare call under `set -e` would.
  out="$($KUBECTL -n "$NAMESPACE" get lease "$LEASE_NAME" -o json 2>"$err_file")" || status=$?
  err="$(cat "$err_file")"
  rm -f "$err_file"

  if [ "$status" -eq 0 ]; then
    printf '%s' "$out"
    return 0
  fi
  if grep -qi 'not found' <<<"$err"; then
    return 0
  fi
  echo "staging-lease: reading the lease failed: $err" >&2
  return 1
}

# The fourth argument is when the claim was first taken, which a renewal carries
# forward: acquireTime is how long this holder has had the namespace, and
# rewriting it every renewal would report a claim that was always brand new.
lease_manifest() {
  local holder="$1" stamp="$2" resource_version="${3:-}" acquired="${4:-$2}"
  local version_line=""
  [ -n "$resource_version" ] && version_line="
    resourceVersion: \"$resource_version\""
  cat <<MANIFEST
apiVersion: coordination.k8s.io/v1
kind: Lease
metadata:
  name: $LEASE_NAME
  namespace: $NAMESPACE$version_line
spec:
  holderIdentity: "$holder"
  acquireTime: "$acquired"
  renewTime: "$stamp"
  leaseDurationSeconds: $TTL_SECONDS
MANIFEST
}

# A claim is stale once its renewTime plus its own declared duration is in the
# past. The holder's duration is used rather than ours, so a holder that asked
# for longer is honoured for as long as it asked.
lease_is_expired() {
  local json="$1" renew duration renew_epoch
  renew="$(printf '%s' "$json" | jq -r '.spec.renewTime // empty')"
  duration="$(printf '%s' "$json" | jq -r '.spec.leaseDurationSeconds // empty')"
  # A lease carrying neither is not a claim anybody can wait on.
  [ -n "$renew" ] && [ -n "$duration" ] || return 0
  renew_epoch="$(date -u -d "$renew" +%s 2>/dev/null || echo 0)"
  [ "$renew_epoch" -eq 0 ] && return 0
  [ "$(now_epoch)" -gt "$((renew_epoch + duration))" ]
}

# The lock rests on the API refusing a second writer, and each of the two writes
# has its own single refusal meaning somebody got there first. They are not
# interchangeable: NotFound is a lost race on a `replace`, where the claim went
# away underneath the version just read, and is never one on a `create`, where
# nothing that already exists could answer it — a namespace missing or being
# torn down does. Reading the two as one wording is how a run comes to wait out
# its whole deadline on a holder that cannot exist.
#
# Every other refusal — a manifest the API will not decode, a credential without
# the rights — is this run's own fault and ends it with the server's own words.
create_lost_race() {
  grep -qiE 'alreadyexists|already exists' <<<"$1"
}

replace_lost_race() {
  grep -qiE 'conflict|notfound|not found' <<<"$1"
}

acquire() {
  local holder="$1" deadline json current out
  deadline=$(($(now_epoch) + WAIT_SECONDS))

  while :; do
    # Whoever was named on an earlier pass may have released since; carrying the
    # name forward reports contention with a run that has already gone.
    current=""
    json="$(read_lease)"

    if [ -z "$json" ]; then
      # `create` is refused if another holder got there first, which is what
      # makes this safe without a read-then-write window.
      if out="$(lease_manifest "$holder" "$(now_micro)" | $KUBECTL create -f - 2>&1)"; then
        echo "staging-lease: held by $holder"
        start_renewing "$holder"
        return 0
      fi
      if ! create_lost_race "$out"; then
        echo "::error::staging-lease: creating ${LEASE_NAME} in ${NAMESPACE} was refused: $out" >&2
        return 1
      fi
    else
      current="$(printf '%s' "$json" | jq -r '.spec.holderIdentity // empty')"

      if [ "$current" = "$holder" ]; then
        echo "staging-lease: already held by $holder"
        start_renewing "$holder"
        return 0
      fi

      if lease_is_expired "$json"; then
        local resource_version
        resource_version="$(printf '%s' "$json" | jq -r '.metadata.resourceVersion // empty')"
        # Carrying the version we read makes this a compare-and-set: if another
        # waiter took the same expired lease first, the replace is refused.
        if out="$(lease_manifest "$holder" "$(now_micro)" "$resource_version" \
          | $KUBECTL replace -f - 2>&1)"; then
          echo "staging-lease: took over an expired claim from ${current:-nobody}, held by $holder"
          start_renewing "$holder"
          return 0
        fi
        if ! replace_lost_race "$out"; then
          echo "::error::staging-lease: taking over ${LEASE_NAME} in ${NAMESPACE} was refused: $out" >&2
          return 1
        fi
      fi
    fi

    if [ "$(now_epoch)" -ge "$deadline" ]; then
      echo "::error::staging-lease: ${LEASE_NAME} in ${NAMESPACE} is held by ${current:-another run} and did not free within ${WAIT_SECONDS}s" >&2
      return 1
    fi

    echo "staging-lease: held by ${current:-another run}; waiting ${POLL_SECONDS}s"
    sleep "$POLL_SECONDS"
  done
}

# renew moves this holder's own claim forward. It is a compare-and-set on the
# version just read, so a renewal that races a takeover loses rather than
# writing over the new holder.
#
# A claim that is gone, or that somebody else now holds, is refused. Writing it
# back would put two runs on the same server with neither of them knowing, and
# the run that lost it needs to hear so: everything it measures from here is
# measured against a namespace it does not hold.
renew() {
  local holder="$1" json current resource_version acquired out
  json="$(read_lease)"

  if [ -z "$json" ]; then
    echo "::error::staging-lease: there is no ${LEASE_NAME} in ${NAMESPACE} to renew for $holder" >&2
    return 1
  fi

  current="$(printf '%s' "$json" | jq -r '.spec.holderIdentity // empty')"
  if [ "$current" != "$holder" ]; then
    echo "::error::staging-lease: ${LEASE_NAME} in ${NAMESPACE} is held by ${current:-nobody}, not $holder" >&2
    return 1
  fi

  resource_version="$(printf '%s' "$json" | jq -r '.metadata.resourceVersion // empty')"
  acquired="$(printf '%s' "$json" | jq -r '.spec.acquireTime // empty')"
  [ -n "$acquired" ] || acquired="$(now_micro)"

  if out="$(lease_manifest "$holder" "$(now_micro)" "$resource_version" "$acquired" \
    | $KUBECTL replace -f - 2>&1)"; then
    return 0
  fi
  echo "::error::staging-lease: renewing ${LEASE_NAME} in ${NAMESPACE} for $holder was refused: $out" >&2
  return 1
}

# keep_renewing is the loop the renewer runs. It is a verb of its own so the
# process left behind is this script rather than an inline shell somebody has to
# reconstruct from a process listing.
#
# A renewal it cannot make ends it, and what it could not do is written down
# where the release step reads it. Carrying on would keep a dead claim's holder
# believing it still held the namespace, which is the state this whole path
# exists to prevent.
keep_renewing() {
  local holder="$1" out
  while :; do
    sleep "$RENEW_SECONDS"
    if ! out="$(renew "$holder" 2>&1)"; then
      printf '%s\n' "${out:-the renewal was refused}" >"$RENEWER_LOST_FILE"
      return 1
    fi
  done
}

# start_renewing leaves a renewer behind and makes sure it is actually there. A
# renewer that was never started is the false green in miniature: the claim
# looks held, and expires forty-five minutes into a five-hour run.
start_renewing() {
  local holder="$1" pid
  stop_renewing
  rm -f "$RENEWER_LOST_FILE"
  mkdir -p "$STATE_DIR"

  setsid "$0" keep-renewing "$holder" </dev/null >/dev/null 2>&1 &
  pid=$!
  printf '%s\n' "$pid" >"$RENEWER_PID_FILE"
  disown "$pid" 2>/dev/null || true

  if ! kill -0 "$pid" 2>/dev/null; then
    echo "::error::staging-lease: the renewer for $holder did not start, so the claim would expire mid-run" >&2
    rm -f "$RENEWER_PID_FILE"
    return 1
  fi
  echo "staging-lease: renewing every ${RENEW_SECONDS}s while $holder works"
}

# stop_renewing ends the renewer this machine started, if there is one. A
# renewer outliving its run would write over the next run's own claim.
stop_renewing() {
  local pid
  [ -f "$RENEWER_PID_FILE" ] || return 0
  pid="$(cat "$RENEWER_PID_FILE")"
  rm -f "$RENEWER_PID_FILE"
  [ -n "$pid" ] || return 0
  kill "$pid" 2>/dev/null || true
}

release() {
  local holder="$1" json current lost=""

  stop_renewing
  if [ -f "$RENEWER_LOST_FILE" ]; then
    lost="$(cat "$RENEWER_LOST_FILE")"
    rm -f "$RENEWER_LOST_FILE"
  fi

  json="$(read_lease)"

  if [ -z "$json" ]; then
    echo "staging-lease: nothing to release"
    report_any_loss "$lost"
    return
  fi

  current="$(printf '%s' "$json" | jq -r '.spec.holderIdentity // empty')"
  if [ "$current" != "$holder" ]; then
    # Someone else's claim — most likely ours expired and was taken over while
    # this job was still running. Deleting it would drop a live holder's lock.
    echo "staging-lease: not releasing, ${LEASE_NAME} is held by ${current:-nobody}, not $holder"
    report_any_loss "${lost:-${LEASE_NAME} is held by ${current:-nobody}, not $holder}"
    return
  fi

  $KUBECTL -n "$NAMESPACE" delete lease "$LEASE_NAME" --ignore-not-found >/dev/null
  echo "staging-lease: released by $holder"
  report_any_loss "$lost"
}

# report_any_loss fails the release when the namespace was taken from under the
# run. Everything this job measured after that moment was measured against a
# server another run was also driving, so a green step here would be the false
# green ci-cd-determinism.md exists to refuse — and the release is the one step
# in the job that always runs.
report_any_loss() {
  local lost="$1"
  [ -z "$lost" ] && return 0
  echo "::error::staging-lease: the claim was lost while this run was still working: $lost" >&2
  return 1
}

main() {
  local action="${1:-}" holder="${2:-}"
  case "$action" in
    acquire | renew | release | keep-renewing | stop-renewing) ;;
    *)
      echo "usage: staging-lease.sh {acquire|renew|release} <holder>" >&2
      return 2
      ;;
  esac
  if [ -z "$holder" ]; then
    echo "staging-lease: a holder identity is required" >&2
    return 2
  fi
  case "$action" in
    keep-renewing) keep_renewing "$holder" ;;
    stop-renewing) stop_renewing ;;
    *) "$action" "$holder" ;;
  esac
}

main "$@"
