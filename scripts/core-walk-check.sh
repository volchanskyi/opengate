#!/usr/bin/env bash
# Prove the core reader still reads what the pinned Go toolchain writes.
#
# The endurance run's reference walk (scripts/loadtest-reference-walk.sh) reads
# the Go runtime's own heap structures out of a core — spans, type descriptors,
# allocation bitmaps — and those are unexported and move between Go releases.
# So whenever the toolchain or the reader's pin moves, this builds a small
# program with the toolchain server/go.mod pins, takes a core of it while it
# runs, and puts the core through the same walk the endurance run uses: the
# overview, the memory breakdown, the goroutines, the type histogram, the
# object list, and the walk from the heaviest objects back to what holds them.
# The program holds a chain of main.node values from one global, main.root, and
# the check passes only when the walk names that root.
#
# Usage: core-walk-check.sh <out-dir>
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/loadtest-reference-walk.sh
. "$ROOT/scripts/loadtest-reference-walk.sh"

check_main() {
  if [ "$#" -ne 1 ]; then
    echo "usage: $0 <out-dir>" >&2
    return 2
  fi
  local out="$1"
  need go "no probe can be built"
  need gcore "no core can be taken; gcore ships with gdb"
  need viewcore "a core could be taken and nothing could read it"

  local toolchain
  toolchain="$(awk '$1 == "toolchain" { print $2 }' "$ROOT/server/go.mod")"
  [ -n "$toolchain" ] || refuse "server/go.mod pins no toolchain, so there is no version to hold the reader to"

  local work
  work="$(mktemp -d)"
  mkdir -p "$out"
  cat >"$work/go.mod" <<MOD
module corewalkprobe

go ${toolchain#go}
MOD
  cat >"$work/main.go" <<'PROBE'
package main

import (
	"fmt"
	"time"
)

// node is what the walk is asked to name and follow back to a root. It is
// small enough to carry its type in its span rather than in an allocation
// header, which is the case the reader types.
type node struct {
	next    *node
	payload [64]byte
}

var root *node

func main() {
	for i := 0; i < 512; i++ {
		root = &node{next: root}
	}
	fmt.Println("ready")
	for {
		time.Sleep(time.Hour)
	}
}
PROBE
  (cd "$work" && GOTOOLCHAIN="$toolchain" go build -o probe .) \
    || refuse "the probe did not build with $toolchain"

  "$work/probe" >"$work/probe.out" 2>&1 &
  local pid=$!
  local waited=0
  until grep -q '^ready$' "$work/probe.out" 2>/dev/null; do
    waited=$((waited + 1))
    [ "$waited" -le 30 ] || refuse "the probe never said it was ready"
    sleep 1
  done

  local core="$work/core.$pid"
  elevate gcore -o "$work/core" "$pid" >"$out/gcore.log" 2>&1 \
    || refuse "gcore could not dump the probe (pid $pid); its log is $out/gcore.log"
  kill "$pid" 2>/dev/null || true
  elevate chmod 0644 "$core" 2>/dev/null || true
  [ -s "$core" ] || refuse "gcore reported success and wrote no core"

  walk "$core" "$work/probe" "$out"

  local walked
  walked="$(awk '/^=== main\.node ===$/ { inside = 1; next } /^=== / { inside = 0 } inside' "$out/reference-walk.txt")"
  elevate rm -f "$core"
  rm -rf "$work"
  if ! grep -qF 'main.root' <<<"$walked"; then
    refuse "the reader did not walk a main.node back to main.root on a core $toolchain wrote: $walked"
  fi
  echo "the pinned reader walks a core $toolchain wrote back to its root"
}

check_main "$@"
