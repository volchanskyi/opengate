#!/usr/bin/env bash
# Writes the Fuzz run summary, as Markdown, from each target's libFuzzer log.
#
# Usage: fuzz-summarize.sh <fuzz-crate-dir>
#   The targets are the crate's [[bin]] entries; each one's output is <dir>/logs/<target>.log.
#
# Exit codes:
#   0  summary written, including for a run that produced nothing to read
#   2  usage, or a crate that names no target
set -euo pipefail

DIR="${1:?usage: $0 <fuzz-crate-dir>}"

PURPOSE='Feeds the agent millions of generated, malformed messages to find one that crashes it.'

targets="$(awk '/^\[\[bin\]\]/ { bin = 1; next } /^\[/ { bin = 0 } bin && $1 == "name" { gsub(/"/, "", $3); print $3 }' "$DIR/Cargo.toml")"
if [ -z "$targets" ]; then
  echo "$DIR/Cargo.toml names no fuzz target" >&2
  exit 2
fi

# One line per log: runs|seconds|paths|inputs|crashes|input|finished. A status line is `#<runs>`
# and a tab; `Done` closes a finished run, and a crash names the file its input was written to.
read_log() {
  awk '
    /^#[0-9]+\t/ {
      runs = substr($1, 2)
      for (i = 2; i < NF; i++) {
        if ($i == "cov:") paths = $(i + 1)
        if ($i == "corp:") { inputs = $(i + 1); sub(/\/.*/, "", inputs) }
      }
    }
    /^Done [0-9]+ runs in [0-9]+ second/ { runs = $2; seconds = $5; finished = 1 }
    /Test unit written to / {
      n = split($NF, parts, "/")
      if (parts[n] ~ /^(crash|oom|timeout|leak)-/) { crashes++; input = "artifacts/" parts[n - 1] "/" parts[n] }
    }
    END { printf "%s|%s|%s|%s|%d|%s|%d\n", runs, seconds, paths, inputs, crashes, input, finished }
  ' "$1"
}

rows=""
problems=()
logged=0
while read -r target; do
  log="$DIR/logs/$target.log"
  if [ ! -s "$log" ]; then
    rows+="| $target | did not run | | | | |"$'\n'
    problems+=("$target did not run")
    continue
  fi
  logged=$((logged + 1))
  IFS='|' read -r runs seconds paths inputs crashes input finished < <(read_log "$log")
  if [ "$crashes" -gt 0 ]; then
    rows+="| $target | $runs | — | $paths | $inputs | $crashes: ${input##*/} |"$'\n'
    problems+=("$target crashed; its input is $input in the run's artifacts")
  elif [ "$finished" -eq 1 ]; then
    rows+="| $target | $runs | $seconds s | $paths | $inputs | none |"$'\n'
  else
    rows+="| $target | ${runs:-0}, stopped | — | ${paths:-—} | ${inputs:-—} | none |"$'\n'
    problems+=("$target stopped before it finished")
  fi
done <<<"$targets"

printf '## Fuzz\n\n%s\n\n' "$PURPOSE"
if [ "$logged" -eq 0 ]; then
  printf '**Result:** the run produced nothing to read: no target wrote a log.\n'
  exit 0
fi
if [ "${#problems[@]}" -eq 0 ]; then
  printf '**Result:** pass: no target crashed.\n\n'
else
  result="$(printf '%s. ' "${problems[@]}")"
  printf '**Result:** FAIL: %s\n\n' "${result% }"
fi
printf '| Target | Runs | Time | Code paths reached | Inputs kept | Crashes |\n'
printf '|---|---|---|---|---|---|\n'
printf '%s' "$rows"
