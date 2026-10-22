#!/usr/bin/env bash
# Counts the mutants Stryker instruments for one web shard, or for the config's own list, without
# running a test: a command runner that exits 0 stands in for vitest.
#
# Usage: mutation-web-count.sh [shard]
#
# Exit codes:
#   0  the count is printed
#   2  Stryker is not installed, the shard is unknown, or Stryker printed no count
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/mutation-shards.sh
. "$SCRIPT_DIR/lib/mutation-shards.sh"

WEB_DIR="$(cd "$SCRIPT_DIR/../web" && pwd)"
STRYKER="$WEB_DIR/node_modules/.bin/stryker"
if [ ! -x "$STRYKER" ]; then
  echo "mutation-web-count.sh: Stryker is not installed; run: (cd web && npm ci)" >&2
  exit 2
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

if [ "$#" -ge 1 ]; then
  globs="$(mutation_web_shard_mutate "$1")" || exit 2
  mutate="$(jq -R . <<<"$globs" | jq -s .)"
else
  mutate="$(jq '.mutate' "$WEB_DIR/stryker.config.json")"
fi

jq --argjson mutate "$mutate" --arg sandbox "$work/sandbox" '{
  packageManager,
  mutate: $mutate,
  testRunner: "command",
  commandRunner: {command: "true"},
  coverageAnalysis: "off",
  checkers: [],
  reporters: [],
  concurrency: 1,
  tempDirName: $sandbox
}' "$WEB_DIR/stryker.config.json" >"$work/stryker.json"

if ! out="$(cd "$WEB_DIR" && "$STRYKER" run "$work/stryker.json" --dryRunOnly 2>&1)"; then
  printf '%s\n' "$out" >&2
  exit 2
fi
count="$(sed -n 's/.*Instrumented [0-9]* source file(s) with \([0-9]*\) mutant(s).*/\1/p' <<<"$out")"
if ! [[ "$count" =~ ^[0-9]+$ ]]; then
  printf 'mutation-web-count.sh: Stryker printed no mutant count:\n%s\n' "$out" >&2
  exit 2
fi
printf '%s\n' "$count"
