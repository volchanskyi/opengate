#!/usr/bin/env bash
# Reads the platform of every node in the cluster and fails on an image the deploy installs that
# has no build for one of them, so a deploy refuses before it changes anything.
#
# Usage: deploy/scripts/deploy-images-platform-check.sh
#
# Exit codes:
#   0  every image the deploy installs has a build for every node's platform
#   1  an image has no build for a node's platform, or the cluster named no node
#   2  a registry gave no answer the check can read
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/kubectl-retry.sh
. "$ROOT/scripts/lib/kubectl-retry.sh"

NODES='{range .items[*]}{.status.nodeInfo.operatingSystem}/{.status.nodeInfo.architecture}{"\n"}{end}'
if ! listed="$(kubectl_retry get nodes -o jsonpath="$NODES")" || [ -z "${listed//[[:space:]]/}" ]; then
  echo "::error::deploy-images-platform-check: the cluster's nodes could not be read" >&2
  exit 1
fi
mapfile -t platforms < <(sort -u <<<"$listed" | sed '/^[[:space:]]*$/d')

status=0
python3 "$ROOT/scripts/dependency-fixes" images-unbuilt "${platforms[@]}" || status=$?
if [ "$status" -ne 0 ]; then
  echo "::error::deploy-images-platform-check: an image the deploy installs has no build for ${platforms[*]}," \
    "or its registry gave no answer" >&2
  exit "$status"
fi
