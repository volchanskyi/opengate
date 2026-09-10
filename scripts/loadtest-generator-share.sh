#!/usr/bin/env bash
# Run the load generator inside a processor and memory allowance of its own.
#
# On the throwaway stack the generator and the system under test share one
# runner, and the stack declares what every one of its own services may use —
# the database, the metrics store, the server. The generator declared nothing,
# so it took whatever was left, and "the server ran out of processor" and "the
# generator did" were the same observation at the top of the scaling sweep.
#
# An allowance is also what makes the generator's own reading mean something.
# The harness measures its room against a cgroup quota where the kernel gives it
# one and against the whole box otherwise, and only the first can say the run
# measured the generator rather than the target — which is why the staging pod's
# reading is a verdict and the runner's is evidence. This is what gives the
# runner the same footing.
#
# A share that could not be applied is announced and the run goes ahead without
# one. Nothing then claims a share it does not have: the harness reads its own
# scope and the bundle says which of the two it got, so a reader sees an
# unbounded generator rather than being told about an allowance that was never
# created.
#
# Usage: loadtest-generator-share.sh <processors> <memory> -- <command...>
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

  # A scope rather than a service: the command keeps this shell's standard
  # streams, so the harness's own output is what the caller reads and the
  # keep-or-discard rule around it is unchanged.
  #
  # setpriv drops back to the invoking user inside the scope. Without it the
  # harness runs as root and writes a bundle the runner user cannot read, which
  # turns an allowance into a lost artifact.
  echo "generator allowance: ${processors} processors, ${memory} memory" >&2
  exec sudo -n systemd-run --scope --quiet \
    --property="CPUQuota=$(quota_percent "$processors")" \
    --property="MemoryMax=${memory}" \
    -- setpriv --reuid="$(id -u)" --regid="$(id -g)" --init-groups "$@"
}

# share_is_available reports whether this machine can be asked for a transient
# allowance at all.
#
# The last check is the whole of it: rather than testing the pieces an allowance
# is made of — a systemd, a bus, a cgroup hierarchy, a sudo that does not ask —
# it makes one and throws it away. Each piece can be present and the making
# still fail, and by the time it fails this wrapper has already handed it the
# harness, so a wrapper that guessed would take the run down with it. A trial
# scope costs milliseconds and answers the question that is actually being
# asked.
share_is_available() {
  command -v systemd-run >/dev/null 2>&1 || return 1
  command -v setpriv >/dev/null 2>&1 || return 1
  command -v sudo >/dev/null 2>&1 || return 1
  sudo -n systemd-run --scope --quiet -- true >/dev/null 2>&1
}

# quota_percent turns a processor count into what CPUQuota is spelled in. One
# processor is 100%, and a fractional share is the fraction of one.
quota_percent() {
  awk -v processors="$1" 'BEGIN { printf "%d%%\n", processors * 100 }'
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
