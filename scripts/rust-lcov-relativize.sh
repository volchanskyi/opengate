#!/usr/bin/env bash
# Rewrites an lcov report's paths relative to the repository root, since the scanner mounts the
# tree elsewhere; the read-back requires a source and no absolute path.
#
# Usage: rust-lcov-relativize.sh <lcov-file> <repository-root>
set -euo pipefail

usage() {
  echo "usage: $0 <lcov-file> <repository-root>" >&2
}

main() {
  if [ "$#" -ne 2 ]; then
    usage
    return 2
  fi

  local lcov="$1" root="${2%/}"
  if [ ! -s "$lcov" ]; then
    echo "::error::no lcov report at $lcov, so there is no coverage to relativize." >&2
    return 1
  fi

  local rewritten="$lcov.relative"
  # Compared as a literal prefix, since a repository path may hold regex syntax characters.
  awk -v root="$root/" '
    index($0, "SF:") == 1 {
      path = substr($0, 4)
      if (index(path, root) == 1) {
        print "SF:" substr(path, length(root) + 1)
        next
      }
    }
    { print }
  ' "$lcov" >"$rewritten"
  mv "$rewritten" "$lcov"

  local named absolute
  named="$(grep -c '^SF:' "$lcov" || true)"
  absolute="$(grep -c '^SF:/' "$lcov" || true)"

  if [ "$named" -eq 0 ]; then
    echo "::error::$lcov names no source file, so it would import coverage for nothing." >&2
    return 1
  fi
  if [ "$absolute" -ne 0 ]; then
    {
      echo "::error::$absolute of $named source paths in $lcov are still absolute, so a"
      echo "  scanner reading the tree at another mount point will drop their coverage."
      grep -m3 '^SF:/' "$lcov"
      echo "  Expected every path to start below: $root"
    } >&2
    return 1
  fi

  echo "relativized $named source paths in $lcov against $root"
  return 0
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
