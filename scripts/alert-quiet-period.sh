#!/usr/bin/env bash
# Hold back the shared-infrastructure alerts while a test holds the staging
# claim.
#
# The staging deploy, the load run, the fault drill and the network drill all
# run on the node production runs on, and each drives it hard on purpose. While
# one of them holds the claim, the rules watching the node and the containers on
# it — every rule labelled `watches: shared` — report the test rather than a
# fault. Those are silenced here. The rules labelled `watches: production` read
# production's namespace alone and stay live throughout.
#
# The quiet period rides the claim: `open` when it is taken, `extend` on every
# renewal, `close` when it is released. Each call sets the end a claim's
# duration from now, so a run that dies holding it leaves the node quiet for no
# longer than its claim could have held the namespace.
#
# One silence per holder, named in its comment. `open` and `extend` reuse the
# holder's live silence and move its end; finding none — Grafana forgets every
# silence when its pod restarts — they create one. `close` expires the holder's
# and nobody else's. Every call reads the silences back and refuses an answer
# that does not show what it asked for.
#
# Grafana is reached from inside its own pod, and the administrator password is
# expanded there, from the pod's own environment, so it never reaches the
# runner or the log.
#
# Environment:
#   ALERT_QUIET_SECONDS  how long the quiet period lasts from this call (required)
#   ALERT_QUIET_KUBECTL  the kubectl to run; the tests pass a stand-in
#
# Usage:
#   ALERT_QUIET_SECONDS=2700 scripts/alert-quiet-period.sh open "load-test-1234-1"
#   ALERT_QUIET_SECONDS=2700 scripts/alert-quiet-period.sh extend "load-test-1234-1"
#   ALERT_QUIET_SECONDS=2700 scripts/alert-quiet-period.sh close "load-test-1234-1"
set -euo pipefail

KUBECTL="${ALERT_QUIET_KUBECTL:-kubectl}"

# shellcheck source=lib/kubectl-retry.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/kubectl-retry.sh"

GRAFANA_NAMESPACE=monitoring
GRAFANA_WORKLOAD=deploy/monitoring-grafana
# Grafana's own Alertmanager, under the sub-path the chart serves Grafana from.
SILENCES_API=http://127.0.0.1:3000/grafana/api/alertmanager/grafana/api/v2

# The label every rule carries, and the value the shared rules hold.
QUIET_LABEL=watches
QUIET_VALUE=shared
CREATED_BY=staging-claim

# The request as the pod's shell runs it. Single-quoted, so the password is
# expanded inside the pod and nowhere else; the method and path arrive as
# arguments, and a body arrives on standard input.
# shellcheck disable=SC2016 # expanded by the pod's shell, which holds the password
IN_POD_REQUEST='
if [ "$1" = POST ]; then
  exec curl -sS --fail-with-body -X POST -H "Content-Type: application/json" \
    -u "admin:$GF_SECURITY_ADMIN_PASSWORD" --data-binary @- "$0$2"
fi
exec curl -sS --fail-with-body -X "$1" -u "admin:$GF_SECURITY_ADMIN_PASSWORD" "$0$2"'

usage() {
  echo "usage: alert-quiet-period.sh {open|extend|close} <holder>" >&2
}

# comment_for is the text a holder's silence carries, and the key it is found
# by again.
comment_for() { printf 'quiet while %s holds the staging claim' "$1"; }

# grafana <method> <path> sends one request and prints Grafana's answer. The
# answer is standard output alone: the credential plugin the cluster is reached
# through writes a warning to standard error on every call. Only a failure reads
# standard error, and only kubectl's own error line from it.
#
# A request the cluster never delivered — its connection to the node dropped
# before the command reached the pod — is asked again: nothing ran. One that
# reached Grafana and was refused is Grafana's answer and is asked once. What it
# prints on failure is one line naming which of the two happened, because a
# caller that turns it into an annotation shows the first line and no other.
grafana() {
  local method="$1" path="$2" out err_file status=0 reason
  err_file="$(mktemp)"
  if [ "$method" = POST ]; then
    out="$(KUBECTL_RETRY_BIN="$KUBECTL" kubectl_retry --unstarted --stdin \
      -n "$GRAFANA_NAMESPACE" exec -i "$GRAFANA_WORKLOAD" -- \
      sh -c "$IN_POD_REQUEST" "$SILENCES_API" "$method" "$path" 2>"$err_file")" || status=$?
  else
    out="$(KUBECTL_RETRY_BIN="$KUBECTL" kubectl_retry --unstarted \
      -n "$GRAFANA_NAMESPACE" exec -i "$GRAFANA_WORKLOAD" -- \
      sh -c "$IN_POD_REQUEST" "$SILENCES_API" "$method" "$path" 2>"$err_file")" || status=$?
  fi
  if [ "$status" -ne 0 ]; then
    reason="$(awk '/^(error|Error)/ { last = $0 } NF { any = $0 } END { print (last != "" ? last : any) }' "$err_file")"
    rm -f "$err_file"
    if [ "$status" -eq "$KUBECTL_RETRY_LOST" ]; then
      echo "alert-quiet-period: $method $path never reached Grafana; the cluster dropped the connection on every attempt: $reason" >&2
    else
      echo "alert-quiet-period: Grafana refused $method $path: ${out:-$reason}" >&2
    fi
    return 1
  fi
  rm -f "$err_file"
  printf '%s\n' "$out"
}

# live_silences_of <holder> prints the holder's silences that have not ended,
# as a JSON array.
live_silences_of() {
  local holder="$1" listed
  listed="$(grafana GET /silences)" || return 1
  if ! jq -e 'type == "array"' <<<"$listed" >/dev/null 2>&1; then
    echo "alert-quiet-period: Grafana answered the silence list with something that is not a list: $listed" >&2
    return 1
  fi
  jq -c --arg by "$CREATED_BY" --arg comment "$(comment_for "$holder")" \
    '[.[] | select(.createdBy == $by and .comment == $comment and .status.state != "expired")]' \
    <<<"$listed"
}

# hold opens the holder's quiet period, or moves the end of the one it already
# has, to a claim's duration from now.
hold() {
  local holder="$1" seconds="$2" live body answer id ends
  live="$(live_silences_of "$holder")" || return 1
  ends="$(date -u -d "+${seconds} seconds" +%Y-%m-%dT%H:%M:%SZ)"

  if [ "$(jq 'length' <<<"$live")" -gt 0 ]; then
    # The same silence moved rather than a second one beside it. Its start is
    # carried as it was: a live silence given a new start is replaced rather
    # than extended.
    body="$(jq -c --arg ends "$ends" '.[0] | {id, matchers, startsAt, endsAt: $ends, createdBy, comment}' <<<"$live")"
  else
    body="$(jq -nc \
      --arg label "$QUIET_LABEL" --arg value "$QUIET_VALUE" --arg by "$CREATED_BY" \
      --arg comment "$(comment_for "$holder")" \
      --arg starts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --arg ends "$ends" \
      '{matchers: [{name: $label, value: $value, isRegex: false, isEqual: true}],
        startsAt: $starts, endsAt: $ends, createdBy: $by, comment: $comment}')"
  fi

  answer="$(grafana POST /silences <<<"$body")" || return 1
  id="$(jq -r '.silenceID // empty' <<<"$answer" 2>/dev/null || true)"
  if [ -z "$id" ]; then
    echo "alert-quiet-period: Grafana answered without naming a silence, so nothing was held back: $answer" >&2
    return 1
  fi

  live="$(live_silences_of "$holder")" || return 1
  if ! jq -e --arg id "$id" 'any(.[]; .id == $id)' <<<"$live" >/dev/null; then
    echo "alert-quiet-period: silence $id is not live when read back, so nothing is held back" >&2
    return 1
  fi
  echo "alert-quiet-period: ${QUIET_LABEL}=${QUIET_VALUE} alerts quiet until ${ends} for ${holder} (silence ${id})"
}

# close expires every live silence the holder opened. Another holder's is left
# alone: the claim changes hands with its own quiet period.
close() {
  local holder="$1" live id
  live="$(live_silences_of "$holder")" || return 1
  if [ "$(jq 'length' <<<"$live")" -eq 0 ]; then
    echo "alert-quiet-period: no quiet period to end for ${holder}"
    return 0
  fi
  for id in $(jq -r '.[].id' <<<"$live"); do
    grafana DELETE "/silence/${id}" >/dev/null || return 1
  done

  live="$(live_silences_of "$holder")" || return 1
  if [ "$(jq 'length' <<<"$live")" -gt 0 ]; then
    echo "alert-quiet-period: ${holder}'s quiet period is still live when read back" >&2
    return 1
  fi
  echo "alert-quiet-period: ${QUIET_LABEL}=${QUIET_VALUE} alerts live again after ${holder}"
}

main() {
  local action="${1:-}" holder="${2:-}"
  case "$action" in
    open | extend | close) ;;
    *)
      usage
      return 2
      ;;
  esac
  if [ -z "$holder" ]; then
    echo "alert-quiet-period: a holder identity is required" >&2
    return 2
  fi
  local seconds="${ALERT_QUIET_SECONDS:?ALERT_QUIET_SECONDS is required}"

  case "$action" in
    open | extend) hold "$holder" "$seconds" ;;
    close) close "$holder" ;;
  esac
}

main "$@"
