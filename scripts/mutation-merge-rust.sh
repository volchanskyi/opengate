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
rm -f "$out"

if [[ "$#" -lt 1 ]]; then
  echo "mutation-merge-rust.sh: no shard outcome files given" >&2
  exit 2
fi

for f in "$@"; do
  if [[ ! -f "$f" ]]; then
    echo "mutation-merge-rust.sh: missing shard outcome file: $f" >&2
    exit 2
  fi
  if ! jq -e '
    type == "object"
    and (.end_time | type == "string")
    and (.end_time | length > 0)
    and (.caught | type == "number")
    and (.missed | type == "number")
    and (.timeout | type == "number")
    and (.unviable | type == "number")
  ' "$f" >/dev/null 2>&1; then
    echo "mutation-merge-rust.sh: malformed or incomplete shard outcome: $f" >&2
    exit 2
  fi
done

out_dir="$(dirname "$out")"
mkdir -p "$out_dir"
tmp="$(mktemp "$out_dir/.mutation-merge-rust.XXXXXX")"
cleanup() {
  [[ -z "${tmp:-}" ]] || rm -f "$tmp"
}
trap cleanup EXIT

jq -s '{
  end_time: (map(.end_time) | max),
  caught:   (map(.caught)   | add),
  missed:   (map(.missed)   | add),
  timeout:  (map(.timeout)  | add),
  unviable: (map(.unviable) | add)
}' "$@" >"$tmp"
mv "$tmp" "$out"
tmp=""
