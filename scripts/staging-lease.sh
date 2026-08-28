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
#   STAGING_LEASE_KUBECTL         the kubectl to run; the tests pass a stand-in
#
# Usage:
#   NAMESPACE=opengate-staging scripts/staging-lease.sh acquire "cd-run-1234"
#   NAMESPACE=opengate-staging scripts/staging-lease.sh release "cd-run-1234"
set -euo pipefail

LEASE_NAME="${STAGING_LEASE_NAME:-opengate-staging-guard}"
TTL_SECONDS="${STAGING_LEASE_TTL_SECONDS:-2700}"
WAIT_SECONDS="${STAGING_LEASE_WAIT_SECONDS:-1800}"
POLL_SECONDS="${STAGING_LEASE_POLL_SECONDS:-10}"
KUBECTL="${STAGING_LEASE_KUBECTL:-kubectl}"

: "${NAMESPACE:?NAMESPACE is required}"

now_epoch() { date -u +%s; }

# The Lease API decodes acquireTime and renewTime as MicroTime — RFC3339 with
# exactly six digits of fractional seconds — and refuses the whole object at
# decode when either is shaped any other way, before it reads a holder at all.
# The microseconds carry nothing this lock uses; the duration is whole seconds.
now_micro() { date -u +%Y-%m-%dT%H:%M:%S.000000Z; }

# Prints the lease as JSON, or nothing when it does not exist. A missing lease
# and a broken cluster are different answers, so only "not found" is swallowed.
read_lease() {
  local out
  # The assignment is the condition, so a non-zero kubectl does not end the
  # script the way a bare call under `set -e` would.
  if out="$($KUBECTL -n "$NAMESPACE" get lease "$LEASE_NAME" -o json 2>&1)"; then
    printf '%s' "$out"
    return 0
  fi
  if printf '%s' "$out" | grep -qi 'not found'; then
    return 0
  fi
  echo "staging-lease: reading the lease failed: $out" >&2
  return 1
}

lease_manifest() {
  local holder="$1" stamp="$2" resource_version="${3:-}"
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
  acquireTime: "$stamp"
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
  printf '%s' "$1" | grep -qiE 'alreadyexists|already exists'
}

replace_lost_race() {
  printf '%s' "$1" | grep -qiE 'conflict|notfound|not found'
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

release() {
  local holder="$1" json current
  json="$(read_lease)"

  if [ -z "$json" ]; then
    echo "staging-lease: nothing to release"
    return 0
  fi

  current="$(printf '%s' "$json" | jq -r '.spec.holderIdentity // empty')"
  if [ "$current" != "$holder" ]; then
    # Someone else's claim — most likely ours expired and was taken over while
    # this job was still running. Deleting it would drop a live holder's lock.
    echo "staging-lease: not releasing, ${LEASE_NAME} is held by ${current:-nobody}, not $holder"
    return 0
  fi

  $KUBECTL -n "$NAMESPACE" delete lease "$LEASE_NAME" --ignore-not-found >/dev/null
  echo "staging-lease: released by $holder"
}

main() {
  local action="${1:-}" holder="${2:-}"
  case "$action" in
    acquire | release) ;;
    *)
      echo "usage: staging-lease.sh {acquire|release} <holder>" >&2
      return 2
      ;;
  esac
  if [ -z "$holder" ]; then
    echo "staging-lease: a holder identity is required" >&2
    return 2
  fi
  "$action" "$holder"
}

main "$@"
