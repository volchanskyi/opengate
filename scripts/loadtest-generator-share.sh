#!/usr/bin/env bash
# Runs a load generator inside the processor and memory allowance every generator of the leg shares.
# A share that cannot be applied is announced and the run continues; the bundle records the outcome.
#
# Usage:
#   loadtest-generator-share.sh <processors> <memory> -- <command...>
set -euo pipefail

# Every generator given the allowance runs in this slice, so the harness and the technician
# scenarios share one processor quota and one memory cap.
GENERATOR_SLICE="opengate-generator.slice"

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

  # The limits sit on the slice, which a later generator's scope joins. A scope keeps this shell's
  # standard streams; setpriv drops back to the invoking user so the bundle stays readable.
  echo "generator allowance: ${processors} processors, ${memory} memory, shared in $GENERATOR_SLICE" >&2
  sudo -n systemctl set-property --runtime "$GENERATOR_SLICE" \
    "CPUQuota=$(quota_percent "$processors")" "MemoryMax=${memory}"
  # Sudo keeps the caller's environment on request but always sets its own PATH, so env hands the
  # caller's PATH back to the command.
  exec sudo -n --preserve-env systemd-run --scope --quiet --slice="$GENERATOR_SLICE" \
    -- setpriv --reuid="$(id -u)" --regid="$(id -g)" --init-groups env PATH="$PATH" "$@"
}

# share_is_available makes and discards one transient scope the way main asks for it, since every
# piece of an allowance can be present and the making still fail after the harness is handed over.
share_is_available() {
  command -v systemd-run >/dev/null 2>&1 || return 1
  command -v setpriv >/dev/null 2>&1 || return 1
  command -v sudo >/dev/null 2>&1 || return 1
  sudo -n --preserve-env systemd-run --scope --quiet -- true >/dev/null 2>&1
}

# quota_percent turns a processor count into a CPUQuota percentage; one processor is 100%.
quota_percent() {
  awk -v processors="$1" 'BEGIN { printf "%d%%\n", processors * 100 }'
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
