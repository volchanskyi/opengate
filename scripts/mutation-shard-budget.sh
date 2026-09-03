#!/usr/bin/env bash
# Pre-flight for the mutation workflow: does every shard still fit the job?
#
# A shard that outgrows the 90-minute cap does not fail with a useful message —
# the runner is shot mid-run, the artifact set comes back incomplete, and the
# whole nightly is lost after 90 minutes of compute. That has now happened on
# both legs: ten mesh-agent-core shards grew past their budget together, and
# three Go shards followed when a new integration test raised coverage and turned
# uncovered mutants into runnable ones.
#
# Counting mutants is cheap on both sides, so the same drift is visible in
# minutes. `cargo mutants --list` enumerates a Rust shard's mutants from the
# source; `gremlins unleash --dry-run` lists the Go mutants coverage actually
# reaches. Multiplied by the shard's measured per-mutant cost that gives the
# projection this refuses on, before the matrix has burned a night.
#
# On the Go side that product is only the first term. A mutant that removes a
# loop's exit condition never terminates and holds a worker for its whole leash,
# which is minutes rather than seconds, so the projection adds one leash per
# declared blocking mutant. Leaving that term out is how go-domain-alerts cleared
# this pre-flight at 31 minutes and was then shot at the 90-minute cap.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/mutation-shards.sh
. "$SCRIPT_DIR/lib/mutation-shards.sh"

# How a Rust shard's mutants are counted. Overridable so the behavior tests can
# state a count instead of building a workspace.
COUNTER="${MUTATION_SHARD_COUNTER:-}"

# Where the Go dry-run listing comes from. Overridable for the same reason: the
# real listing costs a module-wide coverage run.
GO_DRYRUN_FILE="${MUTATION_GO_DRYRUN_FILE:-}"

over=0

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

require_count() {
  local shard="$1" count="$2"
  case "$count" in
    '' | *[!0-9]*)
      echo "could not count mutants for $shard (got '$count')" >&2
      exit 2
      ;;
  esac
}

# --- Rust ---------------------------------------------------------------------

budget="$(mutation_rust_shard_budget_minutes)"

printf '%-34s %8s %10s %8s\n' shard mutants projected verdict
for shard in $(mutation_rust_shards); do
  pkg="$(mutation_rust_shard_package "$shard")" || exit 2
  cost="$(mutation_rust_package_milliminutes_per_mutant "$pkg")" || exit 2
  count="$(count_shard "$shard")" || exit 2
  count="${count//[[:space:]]/}"
  require_count "$shard" "$count"
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

# --- Go -----------------------------------------------------------------------
#
# gremlins reports a mutant as RUNNABLE only where coverage reaches it, so the
# listing — not the source — is what says how big a shard has become. One
# module-wide dry-run answers for every shard; the lines it prints carry the file
# each mutant belongs to, which is what buckets them.

go_dryrun() {
  if [ -n "$GO_DRYRUN_FILE" ]; then
    cat "$GO_DRYRUN_FILE"
    return
  fi
  (cd "$SCRIPT_DIR/../server" && GOFLAGS=-count=1 gremlins unleash . --dry-run 2>/dev/null)
}

# The file each RUNNABLE line names, one per line.
runnable_paths() {
  awk '$1 == "RUNNABLE" { split($NF, at, ":"); print at[1] }'
}

count_go_shard() {
  local shard="$1" listing="$2" unit total=0 hits
  for unit in $(mutation_go_shard_units "$shard"); do
    case "$unit" in
      dir:*) hits="$(grep -c "^${unit#dir:}/" "$listing")" ;;
      file:*) hits="$(grep -cx "${unit#file:}" "$listing")" ;;
      *) return 1 ;;
    esac
    total=$((total + hits))
  done
  printf '%s\n' "$total"
}

go_budget="$(mutation_go_shard_budget_minutes)"
# A shard's wall clock has two terms, and only the first is a count of anything.
# The second is one leash per mutant that never terminates — gremlins gives every
# mutant the coverage elapsed times the timeout coefficient, and a mutant with no
# exit condition holds all of it. Projecting the first term alone is what cleared
# go-domain-alerts at 31 minutes on the night it was shot at the 90-minute cap.
go_leash="$(mutation_go_leash_ceiling_seconds)" || exit 2
listing="$(mktemp)"
trap 'rm -f "$listing"' EXIT
go_dryrun | runnable_paths >"$listing"

echo
printf '%-34s %8s %10s %8s\n' shard mutants projected verdict
for shard in $(mutation_go_shards); do
  cost="$(mutation_go_shard_seconds_per_mutant "$shard")" || exit 2
  blocking="$(mutation_go_shard_blocking_mutants "$shard")" || exit 2
  count="$(count_go_shard "$shard" "$listing")" || exit 2
  require_count "$shard" "$count"
  require_count "$shard" "$blocking"
  # Seconds, rounded up to whole minutes, for the same reason as above.
  projected=$(((count * cost + blocking * go_leash + 59) / 60))
  if [ "$projected" -gt "$go_budget" ]; then
    verdict=OVER
    over=$((over + 1))
  else
    verdict=ok
  fi
  printf '%-34s %8s %8smin %8s\n' "$shard" "$count" "$projected" "$verdict"
done

if [ "$over" -gt 0 ]; then
  echo
  echo "::error::$over mutation shard(s) project past their budget" \
    "(Rust ${budget}min, Go ${go_budget}min)." \
    "Split the offending scopes in scripts/lib/mutation-shards.sh before the matrix runs." \
    "A Go shard whose projection is mostly leash is over because its mutants block," \
    "not because it grew: bound the harness rather than splitting it."
  exit 1
fi

echo
echo "All mutation shards fit their budget (Rust ${budget}min, Go ${go_budget}min)."
