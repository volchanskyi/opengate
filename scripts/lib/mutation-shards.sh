#!/usr/bin/env bash
# Single source of truth for the Go mutation-test shard split.
#
# The Go leg of .github/workflows/mutation.yml runs gremlins sharded so it scales
# horizontally instead of crossing a hand-tuned job cap as the server grows
# (td-gremlins-timeout-stability.md). gremlins accepts only one path arg, so a
# shard runs the WHOLE module (`gremlins unleash .`) — preserving module-wide
# coverage, hence no cross-shard coverage loss — and restricts *mutation* to its
# packages with `--exclude-files` (the other shards' packages + the global
# carve-outs). The split is balanced by measured per-package cost (api ~= 40% of
# the run) and each shard keeps a DB-backed package so its coverage-derived
# timeout budget stays generous (avoids the load-induced false-timeout collapse
# without widening timeout-coefficient).
#
# Consumed by the Makefile (make mutate-go), the workflow Go step, and
# scripts/tests/mutation-workflow.test.sh (partition + drift guards). Source this
# file, then call the functions below.

# Packages under server/internal that are NOT mutated (mirror the whole-package
# carve-outs in server/.gremlins.yaml; testutil is shared test scaffolding).
mutation_go_excluded_pkgs() {
  echo "testutil"
}

# Files/paths always excluded from mutation, as a gremlins --exclude-files
# regexp. Mirrors server/.gremlins.yaml exclude-files: a CLI -E overrides the
# config exclude-files, so a sharded run must re-state these. Kept in sync by
# scripts/tests/mutation-workflow.test.sh.
mutation_go_global_excludes() {
  echo 'openapi_gen\.go|cmd/meshserver/main\.go|tests/loadtest/main\.go|internal/testutil/'
}

# Shard ids, in run order.
mutation_go_shards() {
  echo "go-1 go-2"
}

# Space-separated internal package names for a shard id (no path prefix).
# Adding a server package requires adding it here; the partition guard test
# fails until it is assigned to exactly one shard.
mutation_go_shard_pkgs() {
  case "$1" in
    go-1) echo "api amt protocol relay metrics signaling osutil testpg usecase clientapi" ;;
    go-2) echo "agentapi notifications updater cert device auth db session audit" ;;
    *)
      echo "unknown mutation shard: $1" >&2
      return 1
      ;;
  esac
}

# gremlins --exclude-files regexp for a shard: the global carve-outs plus every
# package NOT in this shard, so `gremlins unleash . -E <regex>` mutates only this
# shard's packages while computing coverage across the whole module.
mutation_go_shard_exclude_regex() {
  local shard="$1" other pkg others=""
  mutation_go_shard_pkgs "$shard" >/dev/null || return 1
  for other in $(mutation_go_shards); do
    [ "$other" = "$shard" ] && continue
    for pkg in $(mutation_go_shard_pkgs "$other"); do
      others="${others:+$others|}$pkg"
    done
  done
  printf '%s|internal/(%s)/\n' "$(mutation_go_global_excludes)" "$others"
}
