#!/usr/bin/env bash
# Pre-flight for the mutation workflow: does every Rust shard still fit the job?
#
# A shard that outgrows the 75-minute cap does not fail with a useful message —
# the runner is shot mid-run, the artifact set comes back incomplete, and the
# whole nightly is lost after 75 minutes of compute. That is what happened when
# ten mesh-agent-core shards grew past their budget together.
#
# Counting mutants needs no build, so the same drift is visible in seconds.
# `cargo mutants --list` enumerates a shard's mutants from the source; multiplied
# by the package's measured per-mutant cost it gives the projection this refuses
# on. Run it before the matrix and a drifted split costs one short job instead of
# a night.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/mutation-shards.sh
. "$SCRIPT_DIR/lib/mutation-shards.sh"

# How a shard's mutants are counted. Overridable so the behavior tests can state
# a count instead of building a workspace.
COUNTER="${MUTATION_SHARD_COUNTER:-}"

count_shard() {
  local shard="$1"
  local -a args
  if [ -n "$COUNTER" ]; then
    "$COUNTER" "$shard"
    return
  fi
  mapfile -t args < <(mutation_rust_shard_args "$shard") || return 1
  (cd "$SCRIPT_DIR/../agent" && cargo mutants "${args[@]}" --list 2>/dev/null | wc -l)
}

budget="$(mutation_rust_shard_budget_minutes)"
over=0

printf '%-34s %8s %10s %8s\n' shard mutants projected verdict
for shard in $(mutation_rust_shards); do
  pkg="$(mutation_rust_shard_package "$shard")" || exit 2
  cost="$(mutation_rust_package_milliminutes_per_mutant "$pkg")" || exit 2
  count="$(count_shard "$shard")" || exit 2
  count="${count//[[:space:]]/}"
  case "$count" in
    '' | *[!0-9]*)
      echo "could not count mutants for $shard (got '$count')" >&2
      exit 2
      ;;
  esac
  # Thousandths of a minute, rounded up, so a shard is never reported cheaper
  # than it is.
  projected=$(((count * cost + 999) / 1000))
  if [ "$projected" -gt "$budget" ]; then
    verdict=OVER
    over=$((over + 1))
  else
    verdict=ok
  fi
  printf '%-34s %8s %8smin %8s\n' "$shard" "$count" "$projected" "$verdict"
done

if [ "$over" -gt 0 ]; then
  echo
  echo "::error::$over Rust mutation shard(s) project past the ${budget}min budget." \
    "Split the offending scopes in scripts/lib/mutation-shards.sh before the matrix runs."
  exit 1
fi

echo
echo "All Rust mutation shards fit the ${budget}min budget."
