#!/usr/bin/env bash
# Runs the load generator inside a processor and memory allowance of its own.
# A share that cannot be applied is announced and the run continues; the bundle records the outcome.
#
# Usage:
#   loadtest-generator-share.sh <processors> <memory> -- <command...>
set -euo pipefail

usage() {
  echo "usage: $0 <processors> <memory> -- <command...>" >&2
}

main() {
  if [ "$#" -lt 4 ]; then
    usage
    return 2
  fi

  local processors="$1" memory="$2"
  shift 2
  if [ "$1" != "--" ]; then
    usage
    return 2
  fi
  shift

  if ! share_is_available; then
    echo "::warning::this machine cannot give the generator an allowance of its own, so it runs against the whole box and its bundle will say so." >&2
    exec "$@"
  fi

  # A scope keeps this shell's standard streams; setpriv drops back to the invoking user so the
  # bundle stays readable by the runner user.
  echo "generator allowance: ${processors} processors, ${memory} memory" >&2
  exec sudo -n systemd-run --scope --quiet \
    --property="CPUQuota=$(quota_percent "$processors")" \
    --property="MemoryMax=${memory}" \
    -- setpriv --reuid="$(id -u)" --regid="$(id -g)" --init-groups "$@"
}

# share_is_available makes and discards one transient scope, since every piece of an allowance can
# be present and the making still fail after the harness has been handed over.
share_is_available() {
  command -v systemd-run >/dev/null 2>&1 || return 1
  command -v setpriv >/dev/null 2>&1 || return 1
  command -v sudo >/dev/null 2>&1 || return 1
  sudo -n systemd-run --scope --quiet -- true >/dev/null 2>&1
}

# quota_percent turns a processor count into a CPUQuota percentage; one processor is 100%.
quota_percent() {
  awk -v processors="$1" 'BEGIN { printf "%d%%\n", processors * 100 }'
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
