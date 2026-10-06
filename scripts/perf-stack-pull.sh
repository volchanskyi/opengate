#!/usr/bin/env bash
# Pulls the performance stack's images, retrying a dropped registry connection; a refusal a retry
# cannot change, such as a missing tag, fails on the first attempt.
#
# Environment:
#   PERF_PULL_ATTEMPTS  how many times in total (default 4)
#   PERF_PULL_DELAY     seconds before the second attempt, doubling after (default 10)
#
# Usage: perf-stack-pull.sh <compose-file>
set -euo pipefail

# What a dropped or refused connection to a registry looks like.
TRANSIENT='connection reset|connection refused|i/o timeout|TLS handshake timeout|EOF|Client\.Timeout|context deadline exceeded|no such host|toomanyrequests|Service Unavailable|Bad Gateway|Gateway Timeout'

ERR_FILE=""
cleanup() { [ -z "$ERR_FILE" ] || rm -f "$ERR_FILE"; }
trap cleanup EXIT

main() {
  if [ "$#" -ne 1 ]; then
    echo "usage: $0 <compose-file>" >&2
    return 2
  fi
  local file="$1" attempts="${PERF_PULL_ATTEMPTS:-4}" delay="${PERF_PULL_DELAY:-10}"
  local attempt=1 last
  ERR_FILE="$(mktemp)"

  while :; do
    if docker compose -f "$file" pull --quiet --ignore-buildable 2>"$ERR_FILE"; then
      echo "the stack's images are pulled (attempt $attempt of $attempts)"
      return 0
    fi
    last="$(awk 'NF { line = $0 } END { print line }' "$ERR_FILE")"
    if ! grep -qE "$TRANSIENT" "$ERR_FILE"; then
      echo "::error::the stack's images could not be pulled, for a reason asking again cannot change: $last" >&2
      return 1
    fi
    if [ "$attempt" -ge "$attempts" ]; then
      echo "::error::the registry dropped the connection on all $attempts attempts to pull the stack's images: $last" >&2
      return 1
    fi
    echo "the registry dropped the connection (attempt $attempt of $attempts); asking again in ${delay}s: $last"
    [ "$delay" = "0" ] || sleep "$delay"
    delay=$((delay * 2))
    attempt=$((attempt + 1))
  done
}

main "$@"
