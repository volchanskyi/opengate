#!/usr/bin/env bash
# Judges the store probe in a catch-up leg's bundle against the go/no-go marks the jq below holds,
# each mark a missed one names.
#
# Usage: scripts/perf-catchup-gate.sh BUNDLE
#
# Exit codes:
#   0  go
#   1  no-go, each missed mark named
#   2  the bundle holds no probe reading
set -euo pipefail

[ "$#" -eq 1 ] || {
  echo "Usage: scripts/perf-catchup-gate.sh BUNDLE" >&2
  exit 2
}
BUNDLE="$1"

jq -e '(.store_probe.one_sender.catch_up.readings_per_second | type == "number")
  and (.store_probe.one_sender.live.requests | type == "number")
  and (.store_probe.quiet.cpu_share | type == "number")' "$BUNDLE" >/dev/null 2>&1 || {
  echo "::error::$BUNDLE holds no store probe reading, so the go/no-go has nothing to judge." >&2
  exit 2
}

missed="$(jq -r '
  .store_probe as $p | $p.one_sender as $one
  | [
      (if $one.catch_up.readings_per_second < 10000 then
        "the store took \($one.catch_up.readings_per_second | floor) readings a second with one sender, under 10000 readings a second" else empty end),
      (if $one.duration_seconds < 900 then
        "the probe wrote for \($one.duration_seconds | floor) s, short of 15 minutes" else empty end),
      (if $p.oom_killed then "the store ran out of memory" else empty end),
      (if $one.store.restarted then "the store started again during the probe" else empty end),
      (if ($one.live.requests == 0) or ($one.live.within_five_seconds / $one.live.requests < 0.99) then
        "\($one.live.within_five_seconds) of \($one.live.requests) live-shaped requests were accepted within 5 s, under 99%" else empty end),
      (if $p.quiet.cpu_share >= 0.5 then
        "on a quiet minute the store used \($p.quiet.cpu_share * 100 | floor)% of its processor, not under half its processor" else empty end),
      (if $p.quiet.memory_share >= 0.5 then
        "on a quiet minute the store held \($p.quiet.memory_share * 100 | floor)% of its memory, not under half its memory" else empty end)
    ][]' "$BUNDLE")"

if [ -n "$(jq -r '.store_probe.two_senders // empty' "$BUNDLE")" ]; then
  jq -r '.store_probe.two_senders as $two
    | "two senders: \($two.catch_up.readings_per_second | floor) readings a second, \($two.live.within_five_seconds) of \($two.live.requests) live requests within 5 s"' "$BUNDLE"
fi

if [ -n "$missed" ]; then
  while IFS= read -r line; do
    echo "::error::no-go: $line"
  done <<<"$missed"
  exit 1
fi
jq -r '.store_probe as $p
  | "go: the store took \($p.one_sender.catch_up.readings_per_second | floor) readings a second with one sender for \($p.one_sender.duration_seconds | floor) s, \($p.one_sender.live.within_five_seconds) of \($p.one_sender.live.requests) live requests within 5 s; quiet minute \($p.quiet.cpu_share * 100 | floor)% processor, \($p.quiet.memory_share * 100 | floor)% memory"' "$BUNDLE"
