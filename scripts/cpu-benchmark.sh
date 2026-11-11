#!/usr/bin/env bash
# Runs the fixed processor benchmark on this machine in a container held to a quarter processor and
# writes its reading, so two machines' processors compare on the same work.
#
# Usage:
#   scripts/cpu-benchmark.sh OUT
#
# Exit codes:
#   0  the reading was written
#   1  the benchmark did not run, or wrote no reading
#   2  usage
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="docker.io/library/alpine:3.21.9"

usage() {
  echo "Usage: scripts/cpu-benchmark.sh OUT" >&2
  exit 2
}

[ "$#" -eq 1 ] || usage
OUT="$1"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

GOARCH="$(cd "$ROOT/server" && go env GOARCH)"
echo "building the benchmark for GOARCH=$GOARCH" >&2
(cd "$ROOT/server" && CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build -trimpath -o "$WORK/cpubench" ./tests/cpubench)
docker run --rm --cpus 0.25 -v "$WORK:/bench:ro" "$IMAGE" /bench/cpubench >"$WORK/reading.json"

jq -e '.rounds_per_second > 0' "$WORK/reading.json" >/dev/null || {
  echo "::error::the benchmark wrote no reading: $(head -c 300 "$WORK/reading.json")" >&2
  exit 1
}
cp "$WORK/reading.json" "$OUT"
jq -r '"processor benchmark (\(.arch)): \(.rounds_per_second | floor) rounds a second at a quarter processor"' "$OUT"
