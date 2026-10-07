#!/usr/bin/env bash
# Builds the pinned viewcore commit with the on-demand gcmask patch applied.
# --upstream builds the newest upstream commit unpatched, for scripts/core-walk-check.sh.
#
# Usage: install-viewcore.sh [--upstream] <output-path>
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tool-versions.sh
. "$HERE/lib/tool-versions.sh"

MODULE="golang.org/x/debug"
PATCH="$HERE/patches/viewcore-gcmask-on-demand.patch"

usage() {
  echo "usage: $0 [--upstream] <output-path>" >&2
  exit 2
}

# The module cache hands its files over read-only, so they are made writable
# before they are removed.
WORK=""
cleanup() {
  [ -n "$WORK" ] || return 0
  chmod -R u+w "$WORK" 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

main() {
  local upstream=no
  if [ "${1:-}" = "--upstream" ]; then
    upstream=yes
    shift
  fi
  [ "$#" -eq 1 ] || usage
  local dest="$1"

  local wanted="$MODULE@$TOOL_VERSION_VIEWCORE"
  [ "$upstream" = no ] || wanted="$MODULE@latest"

  WORK="$(mktemp -d)"
  local work="$WORK"

  local fetched source version
  fetched="$(cd "$work" && go mod download -json "$wanted")"
  source="$(jq -r '.Dir // empty' <<<"$fetched")"
  version="$(jq -r '.Version // empty' <<<"$fetched")"
  [ -n "$source" ] || {
    echo "install-viewcore: could not fetch $wanted: $fetched" >&2
    exit 1
  }
  cp -R "$source" "$work/debug"
  chmod -R u+w "$work/debug"

  if [ "$upstream" = no ]; then
    (cd "$work/debug" && git apply "$PATCH") || {
      echo "install-viewcore: $PATCH does not apply to $MODULE $version" >&2
      exit 1
    }
  fi

  mkdir -p "$(dirname "$dest")"
  (cd "$work/debug" && GOFLAGS=-mod=mod go build -trimpath -o "$dest" ./cmd/viewcore)
  if [ "$upstream" = no ]; then
    echo "viewcore $version, patched, at $dest"
  else
    echo "viewcore $version, the newest upstream commit, unpatched, at $dest"
  fi
}

main "$@"
