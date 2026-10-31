#!/usr/bin/env bash
# Runs a benchmark suite at the anchor commit and at tonight's on one machine, in turns, and fails
# only on a slowdown benchstat finds significant and past the limit.
#
# Usage:
#   benchmark-anchor.sh rounds go|rust <anchor-tree> <tonight-tree> <out-dir>
#   benchmark-anchor.sh judge go|rust <out-dir> <baseline.json> <anchor-commit> <tonight-commit>
#
# Environment:
#   BENCHMARK_ANCHOR_SUMMARY  where the verdict table is appended (default benchmark-anchor.md)
#
# Exit codes:
#   0  the rounds ran, or no slowdown past the limit
#   1  a benchmark failed to run, or a slowdown past the limit, or a named benchmark missing
#   2  usage, an input missing, or benchstat at its pin missing
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/summary-table.sh
. "$HERE/lib/summary-table.sh"

# Ten readings a side, the count Go's benchstat documentation asks for.
ROUNDS=10
# A slowdown past this share fails, where benchstat also finds it significant.
LIMIT_PERCENT=10
# The crates whose Criterion suites the nightly runs.
RUST_SUITES=(mesh-protocol mesh-agent-core edge-tsdb)
SUMMARY="${BENCHMARK_ANCHOR_SUMMARY:-benchmark-anchor.md}"
# Fences benchstat's own table on the page.
FENCE='~~~'

usage() {
  sed -n '5,7p' "${BASH_SOURCE[0]}" | sed 's/^# *//' >&2
  exit 2
}

# go_round TREE OUT runs the Go suite once in TREE at the nightly's four processors.
go_round() {
  (cd "$1/server" && go test -bench=. -benchmem -count=1 -cpu 4 -run='^$' ./internal/...) >"$2"
}

# rust_round TREE OUT runs the Rust suites once in TREE, each benchmark's mean becoming a reading.
rust_round() {
  local tree="$1" out="$2" crate file packages=()
  for crate in "${RUST_SUITES[@]}"; do
    packages+=(-p "$crate")
  done
  (cd "$tree/agent" && cargo bench "${packages[@]}" --bench '*') >&2 || return 1
  : >"$out"
  while IFS= read -r file; do
    printf 'Benchmark%s 1 %s ns/op\n' "$(basename "$(dirname "$(dirname "$file")")")" \
      "$(jq -r '.mean.point_estimate' "$file")" >>"$out"
  done < <(find "$tree/agent/target/criterion" -path '*/new/estimates.json' -type f | sort)
  [ -s "$out" ]
}

# results keeps a round's benchmark lines, naming each without go test's processor suffix: the
# goos, pkg and cpu lines would label a table of their own.
results() {
  sed -nE '/^Benchmark/ { s/^(Benchmark[^[:space:]]*)-[0-9]+([[:space:]])/\1\2/; p; }' "$1"
}

rounds() {
  local lang="$1" anchor="$2" tonight="$3" out="$4" n side tree raw
  case "$lang" in go | rust) ;; *) usage ;; esac
  [ -d "$anchor" ] && [ -d "$tonight" ] || {
    echo "missing work tree: $anchor or $tonight" >&2
    return 2
  }
  mkdir -p "$out"
  : >"$out/anchor.txt"
  : >"$out/tonight.txt"
  if [ "$lang" = rust ]; then
    # Criterion keeps a benchmark's folder after the benchmark is gone.
    rm -rf "$anchor/agent/target/criterion" "$tonight/agent/target/criterion"
  fi
  for ((n = 1; n <= ROUNDS; n++)); do
    for side in anchor tonight; do
      tree="$anchor"
      [ "$side" = anchor ] || tree="$tonight"
      raw="$out/$side-$n.raw"
      echo "round $n of $ROUNDS: $side" >&2
      if ! "${lang}_round" "$tree" "$raw"; then
        echo "::error::the $lang benchmarks failed at the $side commit, round $n" >&2
        return 1
      fi
      results "$raw" >>"$out/$side.txt"
    done
    [ "$n" -gt 1 ] || cp "$out/tonight-1.raw" "$out/tonight-first.txt"
  done
  rm -f "$out"/*.raw
}

# change_rows prints name<TAB>change<TAB>p for each row of benchstat's time table.
change_rows() {
  awk -F, '
    $2 == "sec/op" { table = 1; next }
    table && $0 == "" { exit }
    table && $1 != "geomean" { print $1 "\t" $6 "\t" $7 }
  ' <<<"$1"
}

# reading NAME FILE prints NAME's median reading in FILE, or nothing when FILE holds none.
reading() {
  awk -v n="$1" '$1 == n { print $3 }' "$2" | sort -g | awk '
    { v[NR] = $1 }
    END { if (NR) printf "%.4g", (NR % 2 ? v[(NR + 1) / 2] : (v[NR / 2] + v[NR / 2 + 1]) / 2) }'
}

judge() {
  local lang="$1" dir="$2" baseline="$3" anchor_commit="$4" tonight_commit="$5" f
  case "$lang" in go | rust) ;; *) usage ;; esac
  for f in "$dir/anchor.txt" "$dir/tonight.txt" "$baseline"; do
    [ -f "$f" ] || {
      echo "missing: $f" >&2
      return 2
    }
  done
  [ -s "$dir/tonight.txt" ] || {
    echo "tonight's run measured nothing: $dir/tonight.txt" >&2
    return 2
  }
  "$HERE/require-tool.sh" benchstat >&2 || return 2

  local work csv text
  work="$(mktemp -d)"
  cp "$dir/anchor.txt" "$work/anchor"
  cp "$dir/tonight.txt" "$work/tonight"
  if ! csv="$(cd "$work" && benchstat -format csv anchor tonight 2>/dev/null)"; then
    rm -rf "$work"
    echo "benchstat could not compare the anchor with tonight" >&2
    return 2
  fi
  text="$(cd "$work" && benchstat anchor tonight 2>&1 || true)"
  rm -rf "$work"
  local rows
  rows="$(change_rows "$csv")"

  local names name line now was change p verdict result failed=() table=()
  names="$(jq -r --arg lang "$lang" '.benchmarks[]? | select(.lang == $lang) | .name' "$baseline")"
  while IFS= read -r name; do
    [ -n "$name" ] || continue
    line="Benchmark${name#Benchmark}"
    now="$(reading "$line" "$dir/tonight.txt")"
    was="$(reading "$line" "$dir/anchor.txt")"
    change=""
    p=""
    IFS=$'\t' read -r _ change p < <(awk -F'\t' -v n="${name#Benchmark}" '$1 == n' <<<"$rows") || true
    if [ -z "$now" ]; then
      verdict="named in the baseline, missing from tonight's run"
      result="FAIL"
      failed+=("  • ${lang}/${name}: missing from tonight's run")
    elif [ "$anchor_commit" = "$tonight_commit" ]; then
      verdict="${now} ns; the same commit on both sides, recorded"
      result="—"
    elif [ -z "$was" ]; then
      verdict="${now} ns; absent at the anchor, reported until the anchor moves"
      result="—"
    elif [ -z "$change" ] || [ "$change" = "~" ] || [[ "$change" == -* ]]; then
      verdict="${was} → ${now} ns (benchstat ${change:-~}, ${p})"
      result="pass"
    elif awk -v c="${change//[+%]/}" -v l="$LIMIT_PERCENT" 'BEGIN { exit !(c > l) }'; then
      verdict="${was} → ${now} ns (benchstat ${change}, ${p})"
      result="FAIL"
      failed+=("  • ${lang}/${name} ns_op: ${verdict}")
    else
      verdict="${was} → ${now} ns (benchstat ${change}, ${p})"
      result="pass"
    fi
    table+=("$(summary_table_row "${lang}/${name} $(summary_term ns/op), ${ROUNDS} turns a side" \
      "≤ +${LIMIT_PERCENT}% on the anchor, where benchstat finds a change" "$verdict" "$result")")
  done <<<"$names"

  {
    printf '### %s benchmarks against the anchor %s\n\n' "$lang" "${anchor_commit:0:12}"
    summary_table_header
    printf '%s\n' "${table[@]}"
    printf '\n%s\n%s\n%s\n\n' "$FENCE" "$text" "$FENCE"
  } >>"$SUMMARY"

  if ((${#failed[@]})); then
    echo "REGRESSION_ALERT:⚠️ Benchmark regression on ${GITHUB_REF_NAME:-main} against the anchor"
    echo "REGRESSION_ALERT:"
    printf 'REGRESSION_ALERT:%s\n' "${failed[@]}"
    return 1
  fi
  return 0
}

main() {
  case "${1:-}" in
    rounds)
      [ "$#" -eq 5 ] || usage
      rounds "$2" "$3" "$4" "$5"
      ;;
    judge)
      [ "$#" -eq 6 ] || usage
      judge "$2" "$3" "$4" "$5" "$6"
      ;;
    *) usage ;;
  esac
}

main "$@"
