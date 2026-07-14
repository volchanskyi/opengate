#!/usr/bin/env bash
# Merge per-shard gremlins reports into the single canonical Go report the
# summarizer consumes. The Go mutation leg is sharded by package
# directory/file mutation units; each shard writes its own
# mutation-report-<shard>.json. This sums the count fields parse_go reads in
# scripts/mutation-summarize.sh, keeping that contract unchanged.
#
# Usage: mutation-merge-go.sh <out.json> <shard-report.json>...
set -euo pipefail

out="${1:?usage: mutation-merge-go.sh <out.json> <shard-report.json>...}"
shift
rm -f "$out"

if [[ "$#" -lt 1 ]]; then
  echo "mutation-merge-go.sh: no shard reports given" >&2
  exit 2
fi

for f in "$@"; do
  if [[ ! -f "$f" ]]; then
    echo "mutation-merge-go.sh: missing shard report: $f" >&2
    exit 2
  fi
  if ! jq -e '
    type == "object"
    and (.mutants_killed | type == "number")
    and (.mutants_lived | type == "number")
    and (.mutants_not_covered | type == "number")
    and (.mutants_not_viable | type == "number")
  ' "$f" >/dev/null 2>&1; then
    echo "mutation-merge-go.sh: malformed shard report: $f" >&2
    exit 2
  fi
done

out_dir="$(dirname "$out")"
mkdir -p "$out_dir"
tmp="$(mktemp "$out_dir/.mutation-merge-go.XXXXXX")"
cleanup() {
  [[ -z "${tmp:-}" ]] || rm -f "$tmp"
}
trap cleanup EXIT

jq -s '{
  mutants_killed:      (map(.mutants_killed)      | add),
  mutants_lived:       (map(.mutants_lived)       | add),
  mutants_not_covered: (map(.mutants_not_covered) | add),
  mutants_not_viable:  (map(.mutants_not_viable)  | add)
}' "$@" >"$tmp"
mv "$tmp" "$out"
tmp=""
