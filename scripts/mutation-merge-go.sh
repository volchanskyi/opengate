#!/usr/bin/env bash
# Merge per-shard gremlins reports into the single canonical Go report the
# summarizer consumes. The Go mutation leg is sharded by package
# (td-gremlins-timeout-stability.md); each shard writes its own
# mutation-report-<shard>.json. This sums the count fields parse_go reads in
# scripts/mutation-summarize.sh, keeping that contract unchanged.
#
# Usage: mutation-merge-go.sh <out.json> <shard-report.json>...
set -euo pipefail

out="${1:?usage: mutation-merge-go.sh <out.json> <shard-report.json>...}"
shift

if [[ "$#" -lt 1 ]]; then
  echo "mutation-merge-go.sh: no shard reports given" >&2
  exit 2
fi

for f in "$@"; do
  if [[ ! -f "$f" ]]; then
    echo "mutation-merge-go.sh: missing shard report: $f" >&2
    exit 2
  fi
done

jq -s '{
  mutants_killed:      (map(.mutants_killed      // 0) | add),
  mutants_lived:       (map(.mutants_lived       // 0) | add),
  mutants_not_covered: (map(.mutants_not_covered // 0) | add),
  mutants_not_viable:  (map(.mutants_not_viable  // 0) | add)
}' "$@" >"$out"
