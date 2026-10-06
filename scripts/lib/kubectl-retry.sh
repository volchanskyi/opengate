#!/usr/bin/env bash
# Retries a short cluster call after a dropped connection; any other failure returns at once.
# Probes under a fault, long-lived workload execs and non-idempotent writes are never repeated.
# Environment:
#   KUBECTL_RETRY_ATTEMPTS  how many times in total (default 4)
#   KUBECTL_RETRY_DELAY     seconds between attempts (default 3)
#   KUBECTL_RETRY_BIN       the kubectl to run (default: kubectl on PATH)
#
# Usage:  . scripts/lib/kubectl-retry.sh   then   kubectl_retry <kubectl args...>

# Dropped-connection texts from kubectl; any other text fails on the first attempt.
KUBECTL_RETRY_TRANSPORT='EOF|error sending request|connection refused|connection reset|broken pipe|i/o timeout|TLS handshake timeout|Unable to connect to the server|client connection lost|etcdserver: request timed out|http2: |unexpected stream'

# The subset showing the request never reached the node, so nothing ran.
KUBECTL_RETRY_UNSTARTED='error sending request|Unable to connect to the server|TLS handshake timeout'

# How kubectl reports a command the pod ran that exited non-zero, the command's own answer.
KUBECTL_RETRY_COMMAND_EXIT='command terminated with exit code'

# The status returned when every attempt lost its connection, telling the caller no answer came.
KUBECTL_RETRY_LOST=75

# kubectl_retry [--unstarted] [--stdin] <args...> runs a kubectl call, retrying dropped connections.
# --stdin replays the buffered input on each attempt; --unstarted retries only undelivered requests.
kubectl_retry() {
  local attempts="${KUBECTL_RETRY_ATTEMPTS:-4}"
  local delay="${KUBECTL_RETRY_DELAY:-3}"
  local bin="${KUBECTL_RETRY_BIN:-kubectl}"
  local retryable="$KUBECTL_RETRY_TRANSPORT"
  local stdin_copy="" status=0 attempt=1
  local stderr_copy
  stderr_copy="$(mktemp)"

  if [ "${1:-}" = "--unstarted" ]; then
    shift
    retryable="$KUBECTL_RETRY_UNSTARTED"
  fi
  if [ "${1:-}" = "--stdin" ]; then
    shift
    stdin_copy="$(mktemp)"
    cat >"$stdin_copy"
  fi

  while :; do
    status=0
    if [ -n "$stdin_copy" ]; then
      "$bin" "$@" <"$stdin_copy" 2>"$stderr_copy" || status=$?
    else
      "$bin" "$@" 2>"$stderr_copy" </dev/null || status=$?
    fi

    if [ "$status" -eq 0 ]; then
      rm -f "$stderr_copy" "$stdin_copy"
      return 0
    fi

    # The output reaches the caller's stderr on every attempt, so a retried failure stays logged.
    cat "$stderr_copy" >&2

    if grep -qF "$KUBECTL_RETRY_COMMAND_EXIT" "$stderr_copy" \
      || ! grep -qE "$retryable" "$stderr_copy"; then
      echo "kubectl ${1:-} refused for a reason retrying cannot change; not retried." >&2
      rm -f "$stderr_copy" "$stdin_copy"
      return "$status"
    fi

    if [ "$attempt" -ge "$attempts" ]; then
      echo "kubectl ${1:-} lost its connection on all ${attempts} attempts; giving up." >&2
      rm -f "$stderr_copy" "$stdin_copy"
      return "$KUBECTL_RETRY_LOST"
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
