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
# The core is analysed where it is taken and is never carried out. It is most of
# the server's address space, it contains the fixture's own data, and everything
# worth keeping from it is the text this writes.
#
# Usage: loadtest-reference-walk.sh <container> <binary-path-in-container> <out-dir>
set -euo pipefail

# walkedTypes is how many of the heaviest types are followed back to a root. The
# histogram beside it lists every type, so this bounds the expensive half rather
# than the reported half: each walk reads the whole reference graph.
walkedTypes=5

usage() {
  echo "usage: $0 <container> <binary-path-in-container> <out-dir>" >&2
}

refuse() {
  echo "::error::$1" >&2
  exit 1
}

# elevate runs a command as root. Taking a core means attaching to a process
# this user does not own, which the kernel's own pointer-tracing restriction
# refuses for anybody else.
elevate() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  else
    sudo "$@"
  fi
}

need() {
  command -v "$1" >/dev/null 2>&1 || refuse "$1 is not installed, so $2"
}

main() {
  if [ "$#" -ne 3 ]; then
    usage
    return 2
  fi
  local container="$1" binary="$2" out="$3"

  need docker "there is no container to take a core from"
  need gcore "no core can be taken; gcore ships with gdb"
  need readelf "the binary cannot be checked for the debugging information the walk reads"
  need viewcore "a core could be taken and nothing could read it"

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

  local core="$out/core.$pid"
  elevate gcore -o "$out/core" "$pid" >"$out/gcore.log" 2>&1 \
    || refuse "gcore could not dump $container (pid $pid); its log is $out/gcore.log"
  elevate chmod 0644 "$core" 2>/dev/null || true
  if [ ! -s "$core" ]; then
    refuse "gcore reported success and wrote no core at $core"
  fi

  walk "$core" "$exe" "$out"

  # Neither the core nor the copy of the binary survives the job. The core is
  # most of the target's address space, including every row of the fixture; the
  # binary is tens of megabytes of something the source already describes. What
  # is worth keeping is the text above them.
  elevate rm -f "$core"
  rm -f "$exe"
  echo "reference walk written to $out"
}

# walk reads the core and writes what it found.
walk() {
  local core="$1" exe="$2" out="$3"

  # The overview is first because it is the read-back: a core viewcore cannot
  # open fails here, where the message says so, rather than as four empty
  # reports nobody questions.
  viewcore "$core" --exe "$exe" overview >"$out/overview.txt" 2>"$out/viewcore.log" \
    || refuse "viewcore could not read $core; its log is $out/viewcore.log"

  viewcore "$core" --exe "$exe" breakdown >"$out/memory-breakdown.txt" 2>>"$out/viewcore.log" || true
  viewcore "$core" --exe "$exe" goroutines >"$out/goroutines.txt" 2>>"$out/viewcore.log" || true
  viewcore "$core" --exe "$exe" histogram --top 40 >"$out/type-histogram.txt" 2>>"$out/viewcore.log" \
    || refuse "viewcore read the core and could not weigh what is in it"

  # Every live object, kept out of the reports on purpose: a heap this size lists
  # millions of them, and all that is wanted is one address per type.
  local objects="$out/.objects"
  viewcore "$core" --exe "$exe" objects >"$objects" 2>>"$out/viewcore.log" \
    || refuse "viewcore could not list the live objects, so there is nothing to walk back from"

  # The heaviest types, taken with awk's own counter rather than through head:
  # a reader that exits early kills the writer behind it, and under pipefail the
  # dead writer becomes the pipeline's verdict.
  local types
  types="$(awk -v most="$walkedTypes" \
    'NR > 1 && NF >= 4 { name = $4; for (i = 5; i <= NF; i++) name = name " " $i; print name; if (++taken == most) exit }' \
    "$out/type-histogram.txt")"
  if [ -z "$types" ]; then
    refuse "the type histogram named nothing, so the core carries no live heap this can walk"
  fi

  {
    echo "What holds the heaviest objects in $(basename "$core")"
    echo
    echo "Each entry is one object of that type, followed back to the root that"
    echo "keeps it alive: a global, or a variable in a live goroutine's frame."
    echo
  } >"$out/reference-walk.txt"

  local type address
  while IFS= read -r type; do
    [ -n "$type" ] || continue
    address="$(awk -v want="$type" \
      '{ name = $0; sub(/^[[:space:]]*[^[:space:]]+[[:space:]]+/, "", name); if (name == want) { print $1; exit } }' \
      "$objects")"
    {
      echo "=== $type ==="
      if [ -z "$address" ]; then
        echo "the histogram weighed this type and the object list names none of it"
      elif ! viewcore "$core" --exe "$exe" reachable "$address" 2>&1; then
        echo "no root reaches $address — it is garbage the collector has not yet taken"
      fi
      echo
    } >>"$out/reference-walk.txt"
  done <<<"$types"

  rm -f "$objects"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
