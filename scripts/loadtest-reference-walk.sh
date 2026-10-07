#!/usr/bin/env bash
# Takes a core of the running server and walks the reference graph from its largest objects.
# The core is encrypted to the maintainer's key, and the plain copy is removed on exit.
#
# Environment:
#   SOAK_DUMP_AGE_RECIPIENT  the maintainer's age public key (required)
#   RUNNER_TEMP              where the plain core is taken
#
# Usage:
#   loadtest-reference-walk.sh <container> <binary-path-in-container> <out-dir> <dump-dir>
#   loadtest-reference-walk.sh --check-recipient
set -euo pipefail

# shellcheck source=lib/reference-walk.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/reference-walk.sh"

usage() {
  echo "usage: $0 <container> <binary-path-in-container> <out-dir> <dump-dir>" >&2
  echo "       $0 --check-recipient" >&2
}

# check_recipient accepts only a native age public key and never prints the value, which may be
# a private key. An SSH recipient writes a marker of the key into every file it encrypts.
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

  # The pid is the host's numbering, since the debugger runs beside the container.
  local pid
  pid="$(docker inspect -f '{{.State.Pid}}' "$container" 2>/dev/null || true)"
  if [ -z "$pid" ] || [ "$pid" = "0" ]; then
    refuse "$container is not running, so there is no live heap to walk"
  fi

  # A core names addresses only, so the binary is copied out to read it against.
  local exe="$out/target-binary"
  docker cp "$container:$binary" "$exe" \
    || refuse "could not take $binary out of $container, so the core cannot be read"

  # A stripped release binary makes every reading empty, which would report a leaking server clean.
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

  # The dump is encrypted before the walk reads the core, so a failed walk still leaves it.
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
