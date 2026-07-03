#!/usr/bin/env bash
# Merge per-shard cargo-mutants outcome files into the single outcomes.json the
# summarizer consumes. The rust mutation leg is sharded with `cargo mutants
# --shard k/n`; each shard writes its own mutants.out/outcomes.json over a
# fraction of the workspace mutants. This sums the top-level count fields
# parse_rust reads in scripts/mutation-summarize.sh, keeping that contract
# unchanged (mirrors scripts/mutation-merge-go.sh for the Go leg).
#
# Usage: mutation-merge-rust.sh <out.json> <shard-outcomes.json>...
set -euo pipefail

out="${1:?usage: mutation-merge-rust.sh <out.json> <shard-outcomes.json>...}"
shift

if [[ "$#" -lt 1 ]]; then
  echo "mutation-merge-rust.sh: no shard outcome files given" >&2
  exit 2
fi

for f in "$@"; do
  if [[ ! -f "$f" ]]; then
    echo "mutation-merge-rust.sh: missing shard outcome file: $f" >&2
    exit 2
  fi
done

jq -s '{
  caught:   (map(.caught   // 0) | add),
  missed:   (map(.missed   // 0) | add),
  timeout:  (map(.timeout  // 0) | add),
  unviable: (map(.unviable // 0) | add)
}' "$@" >"$out"
