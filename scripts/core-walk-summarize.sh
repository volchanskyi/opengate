#!/usr/bin/env bash
# Writes the Core Walk run summary, as Markdown, from what scripts/core-walk-check.sh printed.
#
# Usage: core-walk-summarize.sh <check-log> <go.mod>
#
# Exit codes:
#   0  summary written, including for a run that produced nothing to read
#   2  usage
set -euo pipefail

LOG="${1:?usage: $0 <check-log> <go.mod>}"
GO_MOD="${2:?usage: $0 <check-log> <go.mod>}"

PURPOSE="Checks our memory-leak tracer still reads the current Go version's memory dumps. The weekly long test uses it to find what holds leaked memory."

printf '## Core Walk\n\n%s\n\n' "$PURPOSE"
if [ ! -s "$LOG" ]; then
  printf '**Result:** the run produced nothing to read: the check wrote no output.\n'
  exit 0
fi

log="$(<"$LOG")"
toolchain="$(awk '$1 == "toolchain" { print $2 }' "$GO_MOD")"
refusal="$(grep -m 1 '^::error::' <<<"$log" || true)"
refusal="${refusal#::error::}"

pinned='not reached'
if grep -qF 'the pinned reader walks a core' <<<"$log"; then
  pinned='yes'
elif grep -qF '::error::the reader did not walk' <<<"$log"; then
  pinned='no'
fi

upstream='not reached'
if grep -qF 'the newest upstream reader still cannot walk it' <<<"$log"; then
  upstream='not yet, so the patch stays'
elif grep -qF '::error::the newest upstream reader walks' <<<"$log"; then
  upstream='yes, so the patch can go'
fi

if [ -n "$refusal" ]; then
  result="FAIL: $refusal"
elif [ "$pinned" = yes ] && [ "$upstream" != 'not reached' ]; then
  result='pass'
else
  result='FAIL: the check stopped before it finished'
fi

printf '**Result:** %s\n\n' "$result"
printf '| Check | Answer |\n|---|---|\n'
printf '| Go version | %s |\n' "${toolchain:-not pinned in go.mod}"
printf '| The pinned reader walked the dump | %s |\n' "$pinned"
printf '| The unpatched upstream reader walks it | %s |\n' "$upstream"
