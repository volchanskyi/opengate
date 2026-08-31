#!/usr/bin/env bash
# Rewrite an lcov report's source paths to be relative to the repository root,
# and prove the rewrite landed.
#
# `cargo llvm-cov --lcov` names every file by its absolute path on the machine
# that ran it. Every scanner that reads the report runs the analysis inside a
# container with the tree mounted somewhere else — /usr/src for the Docker
# scanner `make sonar` uses, /github/workspace for the scan action CI uses — so
# an absolute host path resolves to no indexed file and the coverage for it is
# dropped. Silently: the report uploads, the analysis succeeds, and every Rust
# file comes back measured by nothing.
#
# It had never worked. `coverage` and `lines_to_cover` had no value, ever, for
# any Rust file in the project's whole history, while two ≥80% jobs went on
# passing against the same report — the gate simply never saw the language. The
# TypeScript report is written with relative paths and imports fine, which is
# what this makes the Rust one do.
#
# The read-back is the point, not the rewrite. A guard that answers yes when it
# cannot ask is what let this sit unnoticed, so the file has to come out the
# other side naming at least one source, and naming none by an absolute path.
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
  # Compared as a literal prefix rather than matched as a pattern: a repository
  # path is a filename and may hold characters a regex would read as syntax.
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
