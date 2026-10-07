#!/usr/bin/env bash
# Walks a Go core dump from its heaviest objects back to the roots that hold them.
# Sourced by scripts/loadtest-reference-walk.sh and scripts/core-walk-check.sh.
# Usage:  . scripts/lib/reference-walk.sh   then   walk <core> <binary> <out-dir>

# walkedTypes is how many of the heaviest types are followed back to a root; each reads the graph.
walkedTypes=5

refuse() {
  echo "::error::$1" >&2
  exit 1
}

# elevate runs a command as root, since the kernel's pointer-tracing restriction refuses attaching
# to another user's process.
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

walk() {
  local core="$1" exe="$2" out="$3"

  # The overview runs first as the read-back: a core viewcore cannot open fails here with a message.
  viewcore "$core" --exe "$exe" overview >"$out/overview.txt" 2>"$out/viewcore.log" \
    || refuse "viewcore could not read $core; its log is $out/viewcore.log"

  viewcore "$core" --exe "$exe" breakdown >"$out/memory-breakdown.txt" 2>>"$out/viewcore.log" || true
  viewcore "$core" --exe "$exe" goroutines >"$out/goroutines.txt" 2>>"$out/viewcore.log" || true
  viewcore "$core" --exe "$exe" histogram --top 40 >"$out/type-histogram.txt" 2>>"$out/viewcore.log" \
    || refuse "viewcore read the core and could not weigh what is in it"

  # Every live object stays out of the reports: a heap lists millions and only one per type is needed.
  local objects="$out/.objects"
  viewcore "$core" --exe "$exe" objects >"$objects" 2>>"$out/viewcore.log" \
    || refuse "viewcore could not list the live objects, so there is nothing to walk back from"

  # awk counts the types itself, since a reader that exits early kills the writer and pipefail
  # reports that as the pipeline's failure.
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
  echo "reference-walk.sh is a sourced library; source it and call walk" >&2
  exit 2
fi
