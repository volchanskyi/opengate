#!/usr/bin/env bash
# Runs the benchmarks the gauntlet holds a commit to: Go allocations against the CI baseline at the
# nightly's processor count, and every Rust suite the nightly publishes, once each.
#
# Usage:
#   benchmark-local-check.sh go           runs the Go benchmarks and holds them to the baseline
#   benchmark-local-check.sh go <output>  holds one Go benchmark output to the baseline
#   benchmark-local-check.sh rust         runs every Rust benchmark once
#
# Environment:
#   BASELINE_FILE  the CI baseline, benchmarks/baseline.json unless set
#
# Exit codes:
#   0  every benchmark the baseline names inside its tolerance, or every Rust suite ran
#   1  a regression, or a benchmark that failed
#   2  usage, a missing file, or an output that read no benchmark
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASELINE_FILE="${BASELINE_FILE:-$ROOT/benchmarks/baseline.json}"
# shellcheck source=scripts/benchmark-summarize.sh
. "$ROOT/scripts/benchmark-summarize.sh"

# The nightly's runner has four processors, and a run at the same count allocates as CI's does.
BENCH_CPUS=4

# The crates whose Criterion suites the nightly publishes.
RUST_SUITES=(mesh-protocol mesh-agent-core edge-tsdb)

usage() {
  echo "usage: $0 go [<go-bench-output>] | rust" >&2
  exit 2
}

# Holds one Go benchmark output to the baseline; a benchmark CI has not measured is named, not held.
check_go() {
  local output="$1" tsv rows baseline compared absent regressions line
  [ -f "$output" ] || {
    echo "no such benchmark output: $output" >&2
    return 2
  }
  [ -f "$BASELINE_FILE" ] || {
    echo "missing: $BASELINE_FILE" >&2
    return 2
  }
  tsv="$(parse_go_bench "$output")" || return 2
  [ -n "$tsv" ] || {
    echo "read no Go benchmark in $output" >&2
    return 2
  }
  rows="$(rows_from_tsv "$tsv")" || return 2
  baseline="$(jq -c '.' "$BASELINE_FILE")" || {
    echo "baseline is malformed: $BASELINE_FILE" >&2
    return 2
  }

  compared="$(jq -r --argjson rows "$rows" \
    '[$rows[] as $r | .benchmarks[]? | select(.lang == "go" and .name == $r.name)] | length' <<<"$baseline")"
  absent="$(jq -r --argjson rows "$rows" \
    '([$rows[] | .name] - [.benchmarks[]? | select(.lang == "go") | .name]) | join(" ")' <<<"$baseline")"
  printf 'compared %s Go benchmarks with the CI baseline at %s processors\n' "$compared" "$BENCH_CPUS"
  [ -z "$absent" ] || printf 'not in the CI baseline yet: %s\n' "$absent"

  # The workstation reads no nights, so a row with no committed numbers is named and not held.
  local limits
  limits="$(benchmark_limits "$rows" "$baseline" '[]' '[]' '[]')" || return 2
  regressions="$(hard_regressions "$limits")" || return 2
  [ -n "$regressions" ] || return 0
  while IFS= read -r line; do
    printf '  FAIL %s\n' "${line#  • go/}"
  done <<<"$regressions"
  return 1
}

run_go() {
  local output rc=0
  output="$(mktemp)"
  if ! (cd "$ROOT/server" && go test -bench=. -benchmem -count=1 -cpu "$BENCH_CPUS" -run='^$' ./internal/...) \
    >"$output" 2>&1; then
    cat "$output"
    rm -f "$output"
    return 1
  fi
  check_go "$output" || rc=$?
  rm -f "$output"
  return "$rc"
}

run_rust() {
  local crate packages=()
  for crate in "${RUST_SUITES[@]}"; do
    packages+=(-p "$crate")
  done
  (cd "$ROOT/agent" && cargo bench "${packages[@]}" --bench '*' -- --test)
}

[ "$#" -ge 1 ] || usage
case "$1" in
  go)
    [ "$#" -le 2 ] || usage
    if [ "$#" -eq 2 ]; then check_go "$2"; else run_go; fi
    ;;
  rust)
    [ "$#" -eq 1 ] || usage
    run_rust
    ;;
  *) usage ;;
esac
