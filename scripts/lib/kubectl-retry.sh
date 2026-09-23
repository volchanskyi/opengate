#!/usr/bin/env bash
# Retry one short cluster call whose connection was dropped, and nothing else.
#
# A drill died three minutes and forty-nine seconds in because a `chmod` inside a
# pod that was already created and ready had its connection dropped: `Internal
# error occurred: error sending request: ... EOF`. Twelve steps of setup, then
# nothing measured. The health check seven lines below it retried sixty times
# over two minutes; the calls above it got one attempt each. The step already
# knew the cluster was unreliable and guarded the wrong half. The same signature
# has cost three nights across the drill and the load tests.
#
# The contract is narrower than "retry a cluster call", and the narrowness is
# what keeps it safe: short, idempotent, fire-and-forget calls whose only failure
# mode of interest is the transport. Three kinds of call in these workflows must
# never be repeated — a probe whose failure *is* the measurement being taken
# during a deliberate network fault, a long-lived exec carrying the workload
# (where a dropped connection does not kill the process in the pod, so a second
# attempt runs a second generator against the same server), and a non-idempotent
# write (where a transport drop cannot say whether the statement landed).
#
# A refusal that will never succeed is not retried either. A missing pod does not
# become present by asking again, and four attempts at it is four times the wait
# before the real reason is printed.
#
# Environment:
#   KUBECTL_RETRY_ATTEMPTS  how many times in total (default 4)
#   KUBECTL_RETRY_DELAY     seconds between attempts (default 3)
#
# Usage:  . scripts/lib/kubectl-retry.sh   then   kubectl_retry <kubectl args...>

# What a dropped connection looks like coming back out of kubectl. Anything not
# on this list is taken at its word and fails on the first attempt.
KUBECTL_RETRY_TRANSPORT='EOF|error sending request|connection refused|connection reset|broken pipe|i/o timeout|TLS handshake timeout|Unable to connect to the server|client connection lost|etcdserver: request timed out|http2: |unexpected stream'

# kubectl_retry <args...>         — run one kubectl call with no standard input.
# kubectl_retry --stdin <args...> — the same, reading standard input once and
#                                   replaying it on every attempt.
#
# Whether there is input to read is stated rather than sniffed. A library that
# guesses from "is standard input a terminal" answers yes for every call made
# from a script, and then waits forever on a pipe nobody is writing to — which is
# the alert not arriving, slowly. And a retry that replays the command but not
# what was piped into it delivers an empty file into the pod and reports success,
# so the two have to be replayed together.
kubectl_retry() {
  local attempts="${KUBECTL_RETRY_ATTEMPTS:-4}"
  local delay="${KUBECTL_RETRY_DELAY:-3}"
  local stdin_copy="" status=0 attempt=1
  local stderr_copy
  stderr_copy="$(mktemp)"

  if [ "${1:-}" = "--stdin" ]; then
    shift
    stdin_copy="$(mktemp)"
    cat >"$stdin_copy"
  fi

  while :; do
    status=0
    if [ -n "$stdin_copy" ]; then
      kubectl "$@" <"$stdin_copy" 2>"$stderr_copy" || status=$?
    else
      kubectl "$@" 2>"$stderr_copy" </dev/null || status=$?
    fi

    if [ "$status" -eq 0 ]; then
      rm -f "$stderr_copy" "$stdin_copy"
      return 0
    fi

    # Whatever it said goes to the caller's stderr either way, so a failure that
    # is about to be retried is still visible in the log.
    cat "$stderr_copy" >&2

    if ! grep -qE "$KUBECTL_RETRY_TRANSPORT" "$stderr_copy"; then
      echo "kubectl ${1:-} refused for a reason retrying cannot change; not retried." >&2
      rm -f "$stderr_copy" "$stdin_copy"
      return "$status"
    fi

    if [ "$attempt" -ge "$attempts" ]; then
      echo "kubectl ${1:-} lost its connection on all ${attempts} attempts; giving up." >&2
      rm -f "$stderr_copy" "$stdin_copy"
      return "$status"
    fi

    echo "kubectl ${1:-} lost its connection (attempt ${attempt} of ${attempts}); retrying in ${delay}s." >&2
    attempt=$((attempt + 1))
    [ "$delay" = "0" ] || sleep "$delay"
  done
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  echo "kubectl-retry.sh is a sourced library; source it and call kubectl_retry" >&2
  exit 2
fi
