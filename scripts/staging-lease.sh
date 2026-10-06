#!/usr/bin/env bash
# Holds one holder at a time over the staging namespace through a Lease that a renewer keeps alive.
# The Lease expires on its own when the holder dies, and the claim also opens an alert quiet period.
#
# Environment:
#   NAMESPACE                     namespace holding the lease (required)
#   STAGING_LEASE_NAME            lease object name
#   STAGING_LEASE_TTL_SECONDS     how long a claim outlives its last write
#   STAGING_LEASE_WAIT_SECONDS    how long `acquire` waits before giving up
#   STAGING_LEASE_POLL_SECONDS    gap between attempts
#   STAGING_LEASE_RENEW_SECONDS   gap between renewals while the claim is held
#   STAGING_LEASE_STATE_DIR       where the renewer's pid and its account of itself are kept
#   STAGING_LEASE_KUBECTL         the kubectl to run; the tests pass a stand-in
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
# A third of the duration, so two renewals can be missed before the claim goes stale.
RENEW_SECONDS="${STAGING_LEASE_RENEW_SECONDS:-$((TTL_SECONDS / 3))}"
[ "$RENEW_SECONDS" -lt 5 ] && RENEW_SECONDS=5
STATE_DIR="${STAGING_LEASE_STATE_DIR:-${RUNNER_TEMP:-${TMPDIR:-/tmp}}}"
KUBECTL="${STAGING_LEASE_KUBECTL:-kubectl}"

: "${NAMESPACE:?NAMESPACE is required}"

RENEWER_PID_FILE="$STATE_DIR/staging-lease-$LEASE_NAME.pid"
RENEWER_LOST_FILE="$STATE_DIR/staging-lease-$LEASE_NAME.lost"
RENEWER_QUIET_FILE="$STATE_DIR/staging-lease-$LEASE_NAME.quiet"

QUIET_PERIOD="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/alert-quiet-period.sh"

now_epoch() { date -u +%s; }

# The Lease API decodes acquireTime and renewTime as MicroTime, RFC3339 with six fractional digits.
now_micro() { date -u +%Y-%m-%dT%H:%M:%S.000000Z; }

# Stderr stays apart from stdout because the credential plugin warns there on every call.
read_lease() {
  local out err err_file status=0
  err_file="$(mktemp)"
  # The assignment is the condition, so a non-zero kubectl does not end the script under set -e.
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

# The fourth argument is the first-claim time, which a renewal carries forward as acquireTime.
# jq builds JSON so resourceVersion lands under metadata, where the API server reads it.
lease_manifest() {
  local holder="$1" stamp="$2" resource_version="${3:-}" acquired="${4:-$2}"
  jq -n \
    --arg name "$LEASE_NAME" --arg namespace "$NAMESPACE" \
    --arg version "$resource_version" --arg holder "$holder" \
    --arg acquired "$acquired" --arg renewed "$stamp" --argjson ttl "$TTL_SECONDS" \
    '{apiVersion: "coordination.k8s.io/v1", kind: "Lease",
      metadata: ({name: $name, namespace: $namespace}
        + (if $version == "" then {} else {resourceVersion: $version} end)),
      spec: {holderIdentity: $holder, acquireTime: $acquired, renewTime: $renewed,
        leaseDurationSeconds: $ttl}}'
}

lease_is_expired() {
  local json="$1" renew duration renew_epoch
  renew="$(printf '%s' "$json" | jq -r '.spec.renewTime // empty')"
  duration="$(printf '%s' "$json" | jq -r '.spec.leaseDurationSeconds // empty')"
  [ -n "$renew" ] && [ -n "$duration" ] || return 0
  renew_epoch="$(date -u -d "$renew" +%s 2>/dev/null || echo 0)"
  [ "$renew_epoch" -eq 0 ] && return 0
  [ "$(now_epoch)" -gt "$((renew_epoch + duration))" ]
}

# Runs one quiet-period verb for this holder; a failure warns, joins the run summary and returns 1,
# so the renewer can keep the account for the release.
quiet() {
  local verb="$1" holder="$2" out
  if out="$(ALERT_QUIET_SECONDS="$TTL_SECONDS" ALERT_QUIET_KUBECTL="$KUBECTL" \
    "$QUIET_PERIOD" "$verb" "$holder" 2>&1)"; then
    printf '%s\n' "$out"
    return 0
  fi
  printf '::warning::staging-lease: the alert quiet period could not %s for %s: %s\n' \
    "$verb" "$holder" "$out"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    printf -- '- The alert quiet period could not %s for %s, so shared alerts were not held back: %s\n' \
      "$verb" "$holder" "${out##*$'\n'}" >>"$GITHUB_STEP_SUMMARY" 2>/dev/null || true
  fi
  return 1
}

claimed() {
  local holder="$1"
  start_renewing "$holder"
  quiet open "$holder" || true
}

# NotFound is a lost race on a `replace` but a missing namespace on a `create`, so each write has
# its own refusal wording; any other refusal ends the run with the server's words.
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
    # The holder named on an earlier pass may have released since, so the name resets each pass.
    current=""
    json="$(read_lease)"

    if [ -z "$json" ]; then
      # `create` is refused when another holder got there first, which closes the read-write window.
      if out="$(lease_manifest "$holder" "$(now_micro)" | $KUBECTL create -f - 2>&1)"; then
        echo "staging-lease: held by $holder"
        claimed "$holder"
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
        claimed "$holder"
        return 0
      fi

      if lease_is_expired "$json"; then
        local resource_version
        resource_version="$(printf '%s' "$json" | jq -r '.metadata.resourceVersion // empty')"
        # The version read makes this a compare-and-set: a waiter that took the lease first wins.
        if out="$(lease_manifest "$holder" "$(now_micro)" "$resource_version" \
          | $KUBECTL replace -f - 2>&1)"; then
          echo "staging-lease: took over an expired claim from ${current:-nobody}, held by $holder"
          claimed "$holder"
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

# Compare-and-set on the version just read, so a renewal racing a takeover loses.
# A gone or foreign claim is refused so the run that lost the namespace hears about it.
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

# A refused renewal ends the loop and is written where the release reads it.
keep_renewing() {
  local holder="$1" out
  while :; do
    sleep "$RENEW_SECONDS"
    if ! out="$(renew "$holder" 2>&1)"; then
      printf '%s\n' "${out:-the renewal was refused}" >"$RENEWER_LOST_FILE"
      return 1
    fi
    # The summary it inherited belongs to a step that has already finished.
    if ! out="$(GITHUB_STEP_SUMMARY="" quiet extend "$holder")"; then
      printf '%s\n' "$out" >>"$RENEWER_QUIET_FILE"
    fi
  done
}

# An unstarted renewer lets the claim expire, so the start is checked.
start_renewing() {
  local holder="$1" pid
  stop_renewing
  rm -f "$RENEWER_LOST_FILE" "$RENEWER_QUIET_FILE"
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

# Ends the renewer this machine started, which would otherwise write over the next run's claim.
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
  if [ -s "$RENEWER_QUIET_FILE" ]; then
    cat "$RENEWER_QUIET_FILE"
    if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
      printf -- '- The alert quiet period missed %s extension(s) while %s held the claim, so shared alerts were not held back throughout.\n' \
        "$(grep -c "^::warning::" "$RENEWER_QUIET_FILE")" "$holder" >>"$GITHUB_STEP_SUMMARY" 2>/dev/null || true
    fi
  fi
  rm -f "$RENEWER_QUIET_FILE"

  quiet close "$holder" || true

  json="$(read_lease)"

  if [ -z "$json" ]; then
    echo "staging-lease: nothing to release"
    report_any_loss "$lost"
    return
  fi

  current="$(printf '%s' "$json" | jq -r '.spec.holderIdentity // empty')"
  if [ "$current" != "$holder" ]; then
    # Someone else's claim, likely a takeover after expiry; deleting it would drop a live lock.
    echo "staging-lease: not releasing, ${LEASE_NAME} is held by ${current:-nobody}, not $holder"
    report_any_loss "${lost:-${LEASE_NAME} is held by ${current:-nobody}, not $holder}"
    return
  fi

  $KUBECTL -n "$NAMESPACE" delete lease "$LEASE_NAME" --ignore-not-found >/dev/null
  echo "staging-lease: released by $holder"
  report_any_loss "$lost"
}

# Fails the release when the namespace was taken from under the run, since later measurements
# came from a server another run also drove.
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
