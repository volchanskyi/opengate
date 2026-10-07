#!/usr/bin/env bash
# Builds a small program with the pinned Go toolchain, cores it, and walks the core with the
# pinned reader; the newest upstream reader must still fail the same walk.
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

  # The upstream reader runs in a subshell so its refusal ends only that shell.
  # The subshell clears the command hash so it finds the upstream viewcore.
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
