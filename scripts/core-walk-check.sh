#!/usr/bin/env bash
# Prove the core reader still reads what the pinned Go toolchain writes, and
# that it still needs the patch it is built with.
#
# The endurance run's reference walk (scripts/lib/reference-walk.sh) reads
# the Go runtime's own heap structures out of a core — spans, type descriptors,
# allocation bitmaps — and those are unexported and move between Go releases.
# So whenever the toolchain or the reader's pin moves, and every night besides,
# this builds a small program with the toolchain server/go.mod pins, takes a
# core of it while it runs, and puts the core through the same walk the
# endurance run uses: the overview, the memory breakdown, the goroutines, the
# type histogram, the object list, and the walk from the heaviest objects back
# to what holds them.
#
# The program holds two things the walk must name. A chain of main.node values
# from one global, main.root. And one main.wide, a type with far more pointer
# words than the compiler describes in a map of its own: Go builds that map at
# run time, on first use, and reaches it through one more pointer. A reader that
# does not follow the pointer reads the slot as the map and loses the edge to
# the main.leaf held in the last slot — or walks off the end of the binary's
# memory, which is how the first soak dump holding such a type was lost.
#
# The same core then goes through the newest upstream reader, unpatched. While
# that still cannot follow main.leaf back to main.wideRoot the patch is earning
# its place. The night it can, this fails and says to drop the patch.
#
# Environment:
#   VIEWCORE_UPSTREAM  the newest upstream reader, unpatched (required)
#
# Usage: core-walk-check.sh <out-dir>
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/reference-walk.sh
. "$ROOT/scripts/lib/reference-walk.sh"

check_main() {
  if [ "$#" -ne 1 ]; then
    echo "usage: $0 <out-dir>" >&2
    return 2
  fi
  local out="$1"
  : "${VIEWCORE_UPSTREAM:?VIEWCORE_UPSTREAM is required — without the upstream reader nothing says whether the patch is still needed}"
  need go "no probe can be built"
  need gcore "no core can be taken; gcore ships with gdb"
  need viewcore "a core could be taken and nothing could read it"
  [ -x "$VIEWCORE_UPSTREAM" ] || refuse "$VIEWCORE_UPSTREAM is not an executable reader"

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
	"runtime"
	"time"
)

// node is what the walk is asked to name and follow back to a root. It is
// small enough to carry its type in its span rather than in an allocation
// header, which is the case the reader types.
type node struct {
	next    *node
	payload [64]byte
}

// wide has far more pointer words than the compiler describes in a map of its
// own, so the runtime builds its pointer map on first use and GCData reaches
// it through one more pointer.
type wide struct {
	slots [1 << 20]*leaf
}

// leaf is held only through the last slot of a wide, so the walk reaches it
// only by reading that pointer map.
type leaf struct {
	payload [1 << 20]byte
}

var (
	root     *node
	wideRoot *wide
)

func main() {
	for i := 0; i < 512; i++ {
		root = &node{next: root}
	}
	wideRoot = &wide{}
	wideRoot.slots[len(wideRoot.slots)-1] = &leaf{}
	// A collection scans wideRoot, which is what builds its pointer map.
	runtime.GC()
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
  walked="$(walked_from "$out" main.node)"
  if ! grep -qF 'main.root' <<<"$walked"; then
    elevate rm -f "$core"
    refuse "the reader did not walk a main.node back to main.root on a core $toolchain wrote: $walked"
  fi
  walked="$(walked_from "$out" main.leaf)"
  if ! grep -qF 'main.wideRoot' <<<"$walked"; then
    elevate rm -f "$core"
    refuse "the reader did not walk a main.leaf back to main.wideRoot through a pointer map the runtime built on demand, on a core $toolchain wrote: $walked"
  fi
  echo "the pinned reader walks a core $toolchain wrote back to its roots"

  # The same core through the newest upstream reader, in a shell of its own so
  # its refusal ends that shell and not this check. The shell forgets where it
  # last found viewcore, or it would run the patched one again.
  local upstream_bin="$work/upstream-bin" upstream_walked=""
  mkdir -p "$upstream_bin" "$out/upstream"
  ln -s "$VIEWCORE_UPSTREAM" "$upstream_bin/viewcore"
  if (
    PATH="$upstream_bin:$PATH"
    hash -r
    walk "$core" "$work/probe" "$out/upstream"
  ) >"$out/upstream.log" 2>&1; then
    upstream_walked="$(walked_from "$out/upstream" main.leaf)"
  fi
  elevate rm -f "$core"
  rm -rf "$work"
  if grep -qF 'main.wideRoot' <<<"$upstream_walked"; then
    refuse "the newest upstream reader walks a main.leaf back to main.wideRoot unpatched: upstream follows pointer maps built on demand now, so delete scripts/patches/viewcore-gcmask-on-demand.patch, install that commit unpatched, and pin it in scripts/lib/tool-versions.sh"
  fi
  echo "the newest upstream reader still cannot walk it (see $out/upstream.log), so the patch is still needed"
}

# walked_from OUT TYPE prints what the walk wrote about one type.
walked_from() {
  awk -v want="=== $2 ===" '$0 == want { inside = 1; next } /^=== / { inside = 0 } inside' "$1/reference-walk.txt" 2>/dev/null
}

check_main "$@"
