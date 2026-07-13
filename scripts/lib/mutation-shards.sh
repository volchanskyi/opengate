#!/usr/bin/env bash
# Single source of truth for the Go mutation-test shard split.
#
# The Go leg of .github/workflows/mutation.yml runs gremlins sharded so it scales
# horizontally instead of crossing a hand-tuned job cap as the server grows
# (td-gremlins-timeout-stability.md). gremlins accepts only one path arg, so a
# shard runs the WHOLE module (`gremlins unleash .`) — preserving module-wide
# coverage, hence no cross-shard coverage loss — and restricts *mutation* to its
# packages with `--exclude-files` (the other shards' packages + the global
# carve-outs). Because coverage (and therefore gremlins' per-mutant timeout
# budget) is computed over the whole module in every shard, the split is free to
# balance purely by runtime.
#
# Balancing is by *CI* cost, which is dominated by Postgres-backed packages:
# each DB mutant re-pays per-schema migration setup (~20-40s on the 4-vCPU
# runner), so DB-package time >> pure-package time regardless of mutant count.
# `api` alone is the single largest cost (~45min CI) and is one package that
# cannot be split, so it gets its own shard; the other DB packages are spread so
# no shard clusters them; the pure/crypto packages are split across two shards
# (amt anchors one, the wire/relay/observability packages the other) after the
# Edge Sentinel telemetry/correlate growth pushed the single pure shard past the
# 75min cap. Add a shard if a package pushes one over cap.
#
# Consumed by the Makefile (make mutate-go), the workflow Go step + publish merge,
# and scripts/tests/mutation-workflow.test.sh (partition + drift guards). Source
# this file, then call the functions below.

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

# Shard ids, in run order. Named for what they hold (the workflow job appears as
# "Mutation (<shard>)").
mutation_go_shards() {
  echo "go-api go-db go-pure-1 go-pure-2"
}

# Space-separated internal package names for a shard id (no path prefix).
# Adding a server package requires adding it here; the partition guard test
# fails until it is assigned to exactly one shard.
mutation_go_shard_pkgs() {
  case "$1" in
    # api is the irreducible hotspot (largest CI cost) — isolated.
    go-api) echo "api" ;;
    # Remaining Postgres-backed packages, spread so they do not cluster.
    go-db) echo "agentapi auth db dbtx device inventory session audit usecase" ;;
    # Pure + crypto (no Postgres), split into two shards balanced by non-test
    # source size (~2.8k LOC each) after telemetry/correlate growth pushed the
    # single pure shard past the 75min cap (cancelled 2026-07-02). amt dominates
    # (crypto, high mutant count) so it anchors go-pure-1; go-pure-2 carries the
    # wire/relay/observability packages.
    go-pure-1) echo "amt updater notifications cert" ;;
    go-pure-2) echo "protocol correlate telemetry relay metrics signaling testpg testvm osutil clientapi" ;;
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
