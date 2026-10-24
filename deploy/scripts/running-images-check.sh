#!/usr/bin/env bash
# Reads the image of every pod in each namespace the project runs in and fails on one the
# repository does not declare, so the dependency check reads everything the cluster runs.
#
# Usage: deploy/scripts/running-images-check.sh
#
# Exit codes:
#   0  every running image is one the repository declares or builds
#   1  an image runs that the repository does not declare, or the cluster gave no answer
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/kubectl-retry.sh
. "$ROOT/scripts/lib/kubectl-retry.sh"

NAMESPACES=(opengate opengate-staging monitoring ingress-nginx cert-manager)
IMAGES='{range .items[*]}{range .spec.initContainers[*]}{.image}{"\n"}{end}'
IMAGES+='{range .spec.containers[*]}{.image}{"\n"}{end}{end}'

running=""
for namespace in "${NAMESPACES[@]}"; do
  if ! listed="$(kubectl_retry -n "$namespace" get pods -o jsonpath="$IMAGES")"; then
    echo "::error::running-images-check: the pods of $namespace could not be read" >&2
    exit 1
  fi
  running+="$listed"$'\n'
done

status=0
python3 "$ROOT/scripts/dependency-fixes" images-undeclared <<<"$running" || status=$?
if [ "$status" -ne 0 ]; then
  echo "::error::running-images-check: the cluster runs an image the repository does not declare" >&2
  exit 1
fi
echo "running-images-check: every image in ${NAMESPACES[*]} is one the repository declares"
