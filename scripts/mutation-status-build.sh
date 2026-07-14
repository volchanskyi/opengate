#!/usr/bin/env bash
# Validate every expected mutation artifact and emit an always-present run status.
# Incomplete artifacts are data, not a script error: this script exits 0 with
# complete=false so the workflow can upload and push the diagnostic before it
# fails the run as incomplete.
set -euo pipefail

ARTIFACTS_DIR="${1:?Usage: $0 <artifacts-dir> <status.json>}"
OUT="${2:?Usage: $0 <artifacts-dir> <status.json>}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=lib/mutation-shards.sh
source "$SCRIPT_DIR/lib/mutation-shards.sh"

rm -f "$OUT"
work="$(mktemp -d)"
tmp_out=""
cleanup() {
  rm -rf "$work"
  [[ -z "${tmp_out:-}" ]] || rm -f "$tmp_out"
}
trap cleanup EXIT

entries=()
all_valid=1
rust_valid=1
go_valid=1

record_shard() {
  local shard="$1"
  local complete="$2"
  local reason="$3"
  entries+=("$(jq -nc \
    --arg shard "$shard" \
    --argjson complete "$complete" \
    --arg reason "$reason" \
    '{key:$shard,value:{complete:$complete,reason:$reason}}')")
  if [[ "$complete" != "true" ]]; then
    all_valid=0
  fi
}

valid_rust_outcome() {
  jq -e '
    type == "object"
    and (.end_time | type == "string")
    and (.end_time | length > 0)
    and (.caught | type == "number")
    and (.missed | type == "number")
    and (.timeout | type == "number")
    and (.unviable | type == "number")
  ' "$1" >/dev/null 2>&1
}

valid_go_report() {
  jq -e '
    type == "object"
    and (.mutants_killed | type == "number")
    and (.mutants_lived | type == "number")
    and (.mutants_not_covered | type == "number")
    and (.mutants_not_viable | type == "number")
  ' "$1" >/dev/null 2>&1
}

valid_web_report() {
  jq -e '
    type == "object"
    and (.files | type == "object")
    and ([.files[] | (.mutants | type == "array")] | all)
  ' "$1" >/dev/null 2>&1
}

rust_inputs=()
for shard in $(mutation_rust_shards); do
  file="$ARTIFACTS_DIR/mutation-$shard/agent/mutants.out/outcomes.json"
  rust_inputs+=("$file")
  if [[ ! -f "$file" ]]; then
    record_shard "$shard" false missing
    rust_valid=0
  elif valid_rust_outcome "$file"; then
    record_shard "$shard" true ok
  else
    record_shard "$shard" false invalid
    rust_valid=0
  fi
done

go_inputs=()
for shard in $(mutation_go_shards); do
  file="$ARTIFACTS_DIR/mutation-$shard/server/mutation-report-$shard.json"
  go_inputs+=("$file")
  if [[ ! -f "$file" ]]; then
    record_shard "$shard" false missing
    go_valid=0
  elif valid_go_report "$file"; then
    record_shard "$shard" true ok
  else
    record_shard "$shard" false invalid
    go_valid=0
  fi
done

web_file="$ARTIFACTS_DIR/mutation-web/web/reports/mutation/mutation.json"
if [[ ! -f "$web_file" ]]; then
  record_shard web false missing
elif valid_web_report "$web_file"; then
  record_shard web true ok
else
  record_shard web false invalid
fi

# Exercise the same merge implementations used by publish. With valid inputs
# these should be infallible, but completeness must not claim success if the
# canonical language merge cannot actually be formed.
if [[ "$rust_valid" -eq 1 ]] \
  && ! "$SCRIPT_DIR/mutation-merge-rust.sh" "$work/rust.json" "${rust_inputs[@]}" >/dev/null 2>&1; then
  all_valid=0
fi
if [[ "$go_valid" -eq 1 ]] \
  && ! "$SCRIPT_DIR/mutation-merge-go.sh" "$work/go.json" "${go_inputs[@]}" >/dev/null 2>&1; then
  all_valid=0
fi

complete=false
[[ "$all_valid" -eq 1 ]] && complete=true
shards="$(printf '%s\n' "${entries[@]}" | jq -s 'from_entries')"
commit="${GITHUB_SHA:-$(git -C "$SCRIPT_DIR/.." rev-parse HEAD 2>/dev/null || echo unknown)}"
run_id="${GITHUB_RUN_ID:-unknown}"

out_dir="$(dirname "$OUT")"
mkdir -p "$out_dir"
tmp_out="$(mktemp "$out_dir/.mutation-status.XXXXXX")"
jq -n \
  --arg commit "$commit" \
  --arg run_id "$run_id" \
  --argjson complete "$complete" \
  --argjson shards "$shards" \
  '{commit:$commit,run_id:$run_id,complete:$complete,shards:$shards}' >"$tmp_out"
mv "$tmp_out" "$OUT"
tmp_out=""
