#!/usr/bin/env bash
# Prints the store container's state in one line, and its last 100 log lines when it is not running,
# was killed out of memory, or a probe reading is missing or says the store stopped answering.
#
# Usage: scripts/perf-store-state.sh CONTAINER PROBE_READING...
#
# Exit codes:
#   0  the state was printed
#   1  the container does not exist
#   2  usage
set -euo pipefail

[ "$#" -ge 1 ] || {
  echo "Usage: scripts/perf-store-state.sh CONTAINER PROBE_READING..." >&2
  exit 2
}
CONTAINER="$1"
shift

state="$(docker inspect -f '{{.State.Status}} {{.State.OOMKilled}} {{.State.ExitCode}} {{.RestartCount}}' \
  "$CONTAINER" 2>/dev/null)" || {
  echo "::error::there is no container $CONTAINER, so the store's state cannot be read." >&2
  exit 1
}
read -r status oom exit_code restarts <<<"$state"
echo "store $CONTAINER: $status, killed out of memory: $oom, exit code $exit_code, restarts $restarts"

# A failed probe step leaves an empty reading, which tells as much as a stopped store.
telling=false
[ "$status" = running ] && [ "$oom" = false ] || telling=true
for reading in "$@"; do
  jq -e 'type == "object" and length > 0 and ((.store.stopped_answering // false) | not)' \
    "$reading" >/dev/null 2>&1 || telling=true
done

if [ "$telling" = true ]; then
  echo "the store's last 100 log lines:"
  docker logs --tail 100 "$CONTAINER" 2>&1
fi
