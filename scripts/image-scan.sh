#!/usr/bin/env bash
# Scans one image with the pinned Trivy for high and critical holes a release fixes, and refuses
# each that scripts/lib/vulnerability-exceptions.json does not name.
#
# Usage: scripts/image-scan.sh IMAGE [--platform PLATFORM]
#
# Environment:
#   TRIVY_USERNAME, TRIVY_PASSWORD   registry credentials Trivy reads for a private image
#
# Exit codes:
#   0  no high or critical hole with a fix outside a live exception
#   1  such a hole, or an exception that lapsed
#   2  the image could not be scanned, or the report or the exception list does not read
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ATTEMPTS=3

usage() {
  echo "Usage: scripts/image-scan.sh IMAGE [--platform PLATFORM]" >&2
  exit 2
}

[ "$#" -ge 1 ] || usage
IMAGE="$1"
shift
PLATFORM_FLAGS=()
case "$#:${1:-}" in
  0:) ;;
  2:--platform) PLATFORM_FLAGS=(--platform "$2") ;;
  *) usage ;;
esac

"$ROOT/scripts/require-tool.sh" trivy >&2 || exit 2

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
REPORT="$WORK/report.json"

# Findings never fail the scanner here, so a failed run is a scan that did not happen, and is
# retried: its database download is the step that drops.
scanned=""
for attempt in $(seq 1 "$ATTEMPTS"); do
  if trivy image --quiet --scanners vuln --format json --severity HIGH,CRITICAL --ignore-unfixed \
    --timeout 30m "${PLATFORM_FLAGS[@]}" --output "$REPORT" "$IMAGE" 2>"$WORK/trivy.err"; then
    scanned=yes
    break
  fi
  echo "::warning::image scan of $IMAGE, attempt $attempt/$ATTEMPTS: $(tail -n 1 "$WORK/trivy.err")" >&2
  if [ "$attempt" -lt "$ATTEMPTS" ]; then
    sleep $((attempt * 15))
  fi
done
[ -n "$scanned" ] || {
  echo "::error::image scan: $IMAGE could not be scanned after $ATTEMPTS attempts" >&2
  exit 2
}

python3 "$ROOT/scripts/dependency-fixes" image-findings "$REPORT"
