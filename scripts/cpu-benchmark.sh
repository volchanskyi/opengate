#!/usr/bin/env bash
# Runs the fixed processor benchmark at a quarter processor and writes its reading, so two machines'
# processors compare on the same work.
#
# Usage:
#   scripts/cpu-benchmark.sh runner OUT    (this machine, a container held to a quarter processor)
#   scripts/cpu-benchmark.sh cluster OUT   (once, the production node: a pod in staging's namespace)
#
# Environment:
#   CPU_BENCHMARK_NAMESPACE   namespace the cluster run's pod starts in (default opengate-staging)
#
# Exit codes:
#   0  the reading was written
#   1  the benchmark did not run, or wrote no reading
#   2  usage
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="docker.io/library/alpine:3.21.9"
NAMESPACE="${CPU_BENCHMARK_NAMESPACE:-opengate-staging}"

usage() {
  echo "Usage: scripts/cpu-benchmark.sh runner|cluster OUT" >&2
  exit 2
}

[ "$#" -eq 2 ] || usage
MODE="$1"
OUT="$2"
case "$MODE" in runner | cluster) ;; *) usage ;; esac

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

build() { # GOARCH
  echo "building the benchmark for GOARCH=$1" >&2
  (cd "$ROOT/server" && CGO_ENABLED=0 GOOS=linux GOARCH="$1" go build -trimpath -o "$WORK/cpubench" ./tests/cpubench)
}

if [ "$MODE" = runner ]; then
  build "$(cd "$ROOT/server" && go env GOARCH)"
  docker run --rm --cpus 0.25 -v "$WORK:/bench:ro" "$IMAGE" /bench/cpubench >"$WORK/reading.json"
else
  build arm64
  POD="cpubench-$(date -u +%Y%m%d%H%M%S)"
  # Not retried: a run that failed midway is started again from the top by hand.
  cleanup_pod() { kubectl -n "$NAMESPACE" delete pod "$POD" --ignore-not-found --wait=false >/dev/null || true; }
  trap 'cleanup_pod; rm -rf "$WORK"' EXIT
  kubectl -n "$NAMESPACE" run "$POD" --image="$IMAGE" --restart=Never \
    --overrides='{"spec":{"containers":[{"name":"'"$POD"'","image":"'"$IMAGE"'","command":["sleep","900"],"resources":{"requests":{"cpu":"25m","memory":"32Mi"},"limits":{"cpu":"250m","memory":"64Mi"}}}]}}' \
    --command -- sleep 900 >/dev/null
  kubectl -n "$NAMESPACE" wait --for=condition=Ready "pod/$POD" --timeout=180s >/dev/null
  kubectl -n "$NAMESPACE" cp "$WORK/cpubench" "$POD:/tmp/cpubench"
  kubectl -n "$NAMESPACE" exec "$POD" -- chmod 0755 /tmp/cpubench
  kubectl -n "$NAMESPACE" exec "$POD" -- /tmp/cpubench >"$WORK/reading.json"
fi

jq -e '.rounds_per_second > 0' "$WORK/reading.json" >/dev/null || {
  echo "::error::the benchmark wrote no reading: $(head -c 300 "$WORK/reading.json")" >&2
  exit 1
}
cp "$WORK/reading.json" "$OUT"
jq -r '"processor benchmark (\(.arch)): \(.rounds_per_second | floor) rounds a second at a quarter processor"' "$OUT"
