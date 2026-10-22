#!/usr/bin/env bash
# Unions per-shard Stryker reports into the one web report scripts/mutation-summarize.sh reads,
# refusing two shards that report one file, which would count its mutants twice.
#
# Usage: mutation-merge-web.sh <out.json> <shard-report.json>...
set -euo pipefail

out="${1:?usage: mutation-merge-web.sh <out.json> <shard-report.json>...}"
shift
rm -f "$out"

if [[ "$#" -lt 1 ]]; then
  echo "mutation-merge-web.sh: no shard reports given" >&2
  exit 2
fi

for f in "$@"; do
  if [[ ! -f "$f" ]]; then
    echo "mutation-merge-web.sh: missing shard report: $f" >&2
    exit 2
  fi
  if ! jq -e '
    type == "object"
    and (.files | type == "object")
    and ([.files[] | (.mutants | type == "array")] | all)
  ' "$f" >/dev/null 2>&1; then
    echo "mutation-merge-web.sh: malformed shard report: $f" >&2
    exit 2
  fi
done

if ! jq -se '[.[].files | keys[]] | length == (unique | length)' "$@" >/dev/null; then
  echo "mutation-merge-web.sh: two shard reports name one file" >&2
  exit 2
fi

out_dir="$(dirname "$out")"
mkdir -p "$out_dir"
tmp="$(mktemp "$out_dir/.mutation-merge-web.XXXXXX")"
cleanup() {
  [[ -z "${tmp:-}" ]] || rm -f "$tmp"
}
trap cleanup EXIT

jq -s 'reduce .[1:][] as $shard (.[0]; .files += $shard.files)' "$@" >"$tmp"
mv "$tmp" "$out"
tmp=""
