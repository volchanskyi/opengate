#!/usr/bin/env bash
# Copies each Helm release record a namespace keeps as a secret into the configmap Helm's configmap
# driver reads, once; a record already copied is left as it is.
#
# Usage: deploy/scripts/helm-release-migrate.sh NAMESPACE...
#
# Exit codes:
#   0  every record is in a configmap
#   1  a record could not be listed or copied
#   2  no namespace was given
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/kubectl-retry.sh
. "$ROOT/scripts/lib/kubectl-retry.sh"

if [ "$#" -eq 0 ]; then
  sed -n '5p' "${BASH_SOURCE[0]}" | sed 's/^# //' >&2
  exit 2
fi

for ns in "$@"; do
  listing="$(kubectl_retry -n "$ns" get secrets -l owner=helm -o json)"
  # The secret driver stores the release encoded twice; the configmap driver stores it once.
  records="$(jq -c --arg ns "$ns" '.items[] | select(.type == "helm.sh/release.v1") | {
    apiVersion: "v1", kind: "ConfigMap",
    metadata: {name: .metadata.name, namespace: $ns, labels: .metadata.labels},
    data: {release: (.data.release | @base64d)}
  }' <<<"$listing")"
  copied=0
  present=0
  while IFS= read -r record; do
    [ -n "$record" ] || continue
    status=0
    out="$(kubectl_retry --stdin -n "$ns" create -f - 2>&1 <<<"$record")" || status=$?
    if [ "$status" -eq 0 ]; then
      copied=$((copied + 1))
    elif grep -qF 'AlreadyExists' <<<"$out"; then
      present=$((present + 1))
    else
      echo "helm-release-migrate: copying $(jq -r '.metadata.name' <<<"$record") in $ns failed: $out" >&2
      exit 1
    fi
  done <<<"$records"
  echo "helm-release-migrate: $ns: $copied copied, $present already there"
done
