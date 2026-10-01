#!/usr/bin/env bash
# Follow what actually holds a leaked object, on the machine the run destroys.
#
# The profiles the soak keeps say where an object was born and how many
# goroutines are parked on a line. For a stuck goroutine that is the whole
# answer. For a held object it is half of one: Go's heap profile records the
# allocation site, and a leak is not about where something was made — it is
# about what is still pointing at it. Nothing the target publishes about itself
# can answer that, because the answer is the shape of the live heap.
#
# A core dump can. This takes one off the running server without stopping it,
# and walks the reference graph back from the objects occupying the most memory
# to the root that keeps them alive — a global, or a variable in a live
# goroutine's frame, named with its function.
#
# It is here and nowhere else for the reason the endurance family runs on a
# throwaway machine at all: taking a core means ptrace on a neighbour's process,
# and reading it means a binary that still carries its debugging information.
# The staging pod drops every capability, runs as a non-root user and has a
# read-only filesystem; this runner is created by the job and destroyed with it.
#
# The core is most of the server's address space and holds the fixture's own
# data, and the repository is public. So it is taken outside the bundle,
# compressed, and encrypted to the maintainer's public key before anything reads
# it — a reader that cannot open it tonight leaves a dump that can be opened at
# the desk with one that can. The plain copy is removed on every exit. The
# program it was taken from travels with the reports, in the clear: it is built
# from public source, and a core names addresses and nothing else.
#
# Environment:
#   SOAK_DUMP_AGE_RECIPIENT  the maintainer's age public key (required)
#   RUNNER_TEMP              where the plain core is taken
#
# Usage: loadtest-reference-walk.sh <container> <binary-path-in-container> <out-dir> <dump-dir>
#        loadtest-reference-walk.sh --check-recipient
set -euo pipefail

# shellcheck source=lib/reference-walk.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/reference-walk.sh"

usage() {
  echo "usage: $0 <container> <binary-path-in-container> <out-dir> <dump-dir>" >&2
  echo "       $0 --check-recipient" >&2
}

# check_recipient refuses anything but a native age public key. The value is
# never printed: one of the shapes refused is a private key. An SSH key is
# refused because an SSH recipient writes a marker of the key into every file
# it encrypts.
check_recipient() {
  local recipient="${SOAK_DUMP_AGE_RECIPIENT:-}"
  case "$recipient" in
    "") refuse "SOAK_DUMP_AGE_RECIPIENT is empty, so the dump could not be encrypted to anybody" ;;
    AGE-SECRET-KEY-*) refuse "SOAK_DUMP_AGE_RECIPIENT holds a private key; it takes the public half (age-keygen -y)" ;;
    ssh-*) refuse "SOAK_DUMP_AGE_RECIPIENT holds an SSH key; it takes a native age public key (age1...)" ;;
  esac
  [[ "$recipient" =~ ^age1[02-9ac-hj-np-z]{58}$ ]] \
    || refuse "SOAK_DUMP_AGE_RECIPIENT is not a native age public key (age1 and 58 characters)"
}

# encrypt_dump CORE DUMP-DIR compresses and encrypts the core, and reads back
# that what it wrote is an age file.
encrypt_dump() {
  local core="$1" dump_dir="$2" dump header
  dump="$dump_dir/$(basename "$core").zst.age"
  mkdir -p "$dump_dir"
  if ! zstd -q -T0 --stdout "$core" | age -r "$SOAK_DUMP_AGE_RECIPIENT" -o "$dump"; then
    rm -f "$dump"
    return 1
  fi
  header="$(head -c 21 "$dump" 2>/dev/null || true)"
  [ "$header" = "age-encryption.org/v1" ] || {
    rm -f "$dump"
    return 1
  }
  echo "encrypted dump written to $dump"
}

main() {
  if [ "${1:-}" = "--check-recipient" ] && [ "$#" -eq 1 ]; then
    check_recipient
    echo "the dump will be encrypted to a native age public key"
    return 0
  fi
  if [ "$#" -ne 4 ]; then
    usage
    return 2
  fi
  local container="$1" binary="$2" out="$3" dump_dir="$4"

  check_recipient
  need docker "there is no container to take a core from"
  need gcore "no core can be taken; gcore ships with gdb"
  need readelf "the binary cannot be checked for the debugging information the walk reads"
  need viewcore "a core could be taken and nothing could read it"
  need zstd "the dump cannot be compressed before it is encrypted"
  need age "the dump cannot be encrypted, and it never leaves the runner in the clear"

  mkdir -p "$out"

  # The process to dump, named in the host's own numbering rather than the
  # container's: the debugger runs beside the container, not inside it.
  local pid
  pid="$(docker inspect -f '{{.State.Pid}}' "$container" 2>/dev/null || true)"
  if [ -z "$pid" ] || [ "$pid" = "0" ]; then
    refuse "$container is not running, so there is no live heap to walk"
  fi

  # The binary, because a core names addresses and nothing else. It is copied
  # out rather than read in place: the path inside the container resolves to
  # nothing out here.
  local exe="$out/target-binary"
  docker cp "$container:$binary" "$exe" \
    || refuse "could not take $binary out of $container, so the core cannot be read"

  # And the binary has to still carry its debugging information. A release build
  # strips it, and a stripped binary makes every reading below empty rather than
  # wrong — which is the shape that reports a leaking server clean.
  local sections
  sections="$(readelf -S "$exe" 2>/dev/null || true)"
  if ! grep -qF -- '.debug_info' <<<"$sections"; then
    refuse "$binary carries no debugging information, so the live heap cannot be typed; build the target with an empty GO_LDFLAGS"
  fi

  # The plain core lives only here, outside the bundle, and goes on every exit.
  SCRATCH="$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/reference-walk.XXXXXX")"
  trap 'elevate rm -rf "$SCRATCH"' EXIT

  local core="$SCRATCH/core.$pid"
  elevate gcore -o "$SCRATCH/core" "$pid" >"$out/gcore.log" 2>&1 \
    || refuse "gcore could not dump $container (pid $pid); its log is $out/gcore.log"
  elevate chmod 0644 "$core" 2>/dev/null || true
  if [ ! -s "$core" ]; then
    refuse "gcore reported success and wrote no core"
  fi

  # Encrypted before anything reads it, so a reader that fails on it tonight
  # still leaves the dump for one that can.
  local encrypted=yes
  if ! encrypt_dump "$core" "$dump_dir"; then
    encrypted=no
    echo "::error::the dump could not be compressed and encrypted, so it was not kept" >&2
  fi

  walk "$core" "$exe" "$out"

  [ "$encrypted" = yes ] \
    || refuse "the walk was written, and the dump it was read from could not be encrypted, so it was not kept"
  echo "reference walk written to $out"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
