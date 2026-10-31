#!/usr/bin/env bash
# Runs the benchmarks the gauntlet holds a commit to: Go allocations against the CI baseline at the
# nightly's processor count, and every Rust suite once, its benchmark names held to the baseline's.
#
# Usage:
#   benchmark-local-check.sh go             runs the Go benchmarks and holds them to the baseline
#   benchmark-local-check.sh go <output>    holds one Go benchmark output to the baseline
#   benchmark-local-check.sh rust           runs every Rust benchmark once and holds the names
#   benchmark-local-check.sh rust <output>  holds one Rust test-mode output's names to the baseline
#
# Environment:
#   BASELINE_FILE  the CI baseline, benchmarks/baseline.json unless set
#
# Exit codes:
#   0  every Go benchmark inside its tolerance, or the Rust benchmarks the baseline names ran
#   1  a regression, a benchmark that failed, or a Rust benchmark one side names and one lacks
#   2  usage, a missing file, or an output that read no benchmark
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASELINE_FILE="${BASELINE_FILE:-$ROOT/benchmarks/baseline.json}"
# shellcheck source=scripts/benchmark-summarize.sh
. "$ROOT/scripts/benchmark-summarize.sh"

# The nightly's runner has four processors, and a run at the same count allocates as CI's does.
BENCH_CPUS=4

# The crates whose Criterion suites the nightly compares.
RUST_SUITES=(mesh-protocol mesh-agent-core edge-tsdb)

usage() {
  echo "usage: $0 go [<go-bench-output>] | rust [<rust-test-output>]" >&2
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
  limits="$(benchmark_limits "$rows" "$baseline" '[]' '[]')" || return 2
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

# Holds the Rust benchmarks one test-mode output ran to the ones the baseline names, both ways, by
# the name the nightly's rows carry: a group's benchmark under its own name.
check_rust() {
  local output="$1" ran named missing extra
  [ -f "$output" ] || {
    echo "no such benchmark output: $output" >&2
    return 2
  }
  [ -f "$BASELINE_FILE" ] || {
    echo "missing: $BASELINE_FILE" >&2
    return 2
  }
  ran="$(sed -n 's/^Testing \(.*\)$/\1/p' "$output" | sed 's|.*/||' | sort -u)"
  [ -n "$ran" ] || {
    echo "read no Rust benchmark in $output" >&2
    return 2
  }
  named="$(jq -r '.benchmarks[]? | select(.lang == "rust") | .name' "$BASELINE_FILE" | sort -u)"
  missing="$(comm -13 <(printf '%s\n' "$ran") <(printf '%s\n' "$named") | paste -sd' ' -)"
  extra="$(comm -23 <(printf '%s\n' "$ran") <(printf '%s\n' "$named") | paste -sd' ' -)"
  printf 'compared %s Rust benchmarks with the CI baseline\n' "$(wc -l <<<"$ran" | tr -d ' ')"
  [ -z "$missing" ] || printf '  FAIL named in the CI baseline, not run: %s\n' "$missing"
  [ -z "$extra" ] || printf '  FAIL run, not named in the CI baseline: %s\n' "$extra"
  [ -z "$missing" ] && [ -z "$extra" ]
}

run_rust() {
  local crate output rc=0 packages=()
  for crate in "${RUST_SUITES[@]}"; do
    packages+=(-p "$crate")
  done
  output="$(mktemp)"
  if ! (cd "$ROOT/agent" && cargo bench "${packages[@]}" --bench '*' -- --test) >"$output" 2>&1; then
    cat "$output"
    rm -f "$output"
    return 1
  fi
  check_rust "$output" || rc=$?
  rm -f "$output"
  return "$rc"
}

[ "$#" -ge 1 ] || usage
case "$1" in
  go)
    [ "$#" -le 2 ] || usage
    if [ "$#" -eq 2 ]; then check_go "$2"; else run_go; fi
    ;;
  rust)
    [ "$#" -le 2 ] || usage
    if [ "$#" -eq 2 ]; then check_rust "$2"; else run_rust; fi
    ;;
  *) usage ;;
esac
