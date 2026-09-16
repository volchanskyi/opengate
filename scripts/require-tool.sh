#!/usr/bin/env bash
# Refuse a target whose tool is not installed, naming the version to install.
#
# Six Makefile targets each carried their own "not found, install with ..."
# line, and every one of them named a version by resolving it at run time —
# `@latest`, or no version at all. Three named a tool this repository's manifest
# already pins, so the advice and the pin disagreed and nothing read both.
#
# It is not theoretical. staticcheck stopped working outright when the Go it had
# been built with fell behind the code it analyses, and the failure surfaced
# inside a gauntlet step whose subject is dead code — an error about export data
# formats, in a check about unused symbols.
#
# So the install command is built here, from scripts/lib/tool-versions.sh, and
# the Makefile asks for a tool by name. One home for the version, one place that
# spells the command, and scripts/tests/tool-version-parity.test.sh holds this
# file to the manifest.
#
# Usage: require-tool.sh <tool>
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tool-versions.sh
. "$HERE/lib/tool-versions.sh"

# install_command TOOL — how to install it at the pinned version. A tool this
# does not know refuses rather than printing a command that would install
# whatever resolves today, which is the shape the whole file exists to remove.
install_command() {
  case "$1" in
    staticcheck)
      printf 'go install honnef.co/go/tools/cmd/staticcheck@v%s' "$TOOL_VERSION_STATICCHECK"
      ;;
    gosec)
      printf 'go install github.com/securego/gosec/v2/cmd/gosec@v%s' "$TOOL_VERSION_GOSEC"
      ;;
    cargo-fuzz)
      printf 'cargo install --locked --version %s cargo-fuzz' "$TOOL_VERSION_CARGO_FUZZ"
      ;;
    cargo-mutants)
      printf 'cargo install --locked --version %s cargo-mutants' "$TOOL_VERSION_CARGO_MUTANTS"
      ;;
    yamllint)
      printf 'pip install --user yamllint==%s' "$TOOL_VERSION_YAMLLINT"
      ;;
    checkov)
      printf 'pipx install checkov==%s' "$TOOL_VERSION_CHECKOV"
      ;;
    *)
      return 1
      ;;
  esac
}

main() {
  if [ "$#" -ne 1 ]; then
    echo "usage: $0 <tool>" >&2
    return 2
  fi

  local tool="$1" command
  if ! command="$(install_command "$tool")"; then
    echo "ERROR: $tool has no pinned version in scripts/lib/tool-versions.sh." >&2
    echo "  Add its row there before asking for it here — a tool nothing pins is one" >&2
    echo "  that has chosen a version on your behalf." >&2
    return 2
  fi

  if command -v "$tool" >/dev/null 2>&1; then
    return 0
  fi

  echo "ERROR: $tool not found. Install the pinned version:" >&2
  echo "  $command" >&2
  return 1
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
