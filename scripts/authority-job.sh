#!/usr/bin/env bash
# Runs one opengate-authority command as a job inside the cluster, on the running server's
# image, and prints its output; the keys stay in the cluster and the job's objects are deleted.
#
# Usage: scripts/authority-job.sh <command> [flags]   e.g. layout -phase changeover
#
# Environment:
#   NAMESPACE              namespace of the release, default opengate
#   RELEASE                release name, default opengate
#   VALUES                 values file, default the production overlay
#   IMAGE_TAG              image tag, default the tag the running server uses
#   AUTHORITY_JOB_TIMEOUT  seconds to wait for the job, default 150
#   AUTHORITY_JOB_POLL     seconds between status reads, default 2
#
# Exit codes:
#   0  the command succeeded
#   1  the command failed or did not finish
#   2  no command was given, or no image tag could be found
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART="$ROOT/deploy/helm/opengate"
NAMESPACE="${NAMESPACE:-opengate}"
RELEASE="${RELEASE:-opengate}"
VALUES="${VALUES:-$CHART/values-production.yaml}"
TIMEOUT="${AUTHORITY_JOB_TIMEOUT:-150}"
POLL="${AUTHORITY_JOB_POLL:-2}"

# shellcheck source=scripts/lib/kubectl-retry.sh
. "$ROOT/scripts/lib/kubectl-retry.sh"

if [ "$#" -eq 0 ]; then
  sed -n '5p' "${BASH_SOURCE[0]}" | sed 's/^# //' >&2
  exit 2
fi

tag="${IMAGE_TAG:-}"
if [ -z "$tag" ]; then
  image="$(kubectl_retry -n "$NAMESPACE" get deploy "$RELEASE-server" \
    -o jsonpath='{.spec.template.spec.containers[?(@.name=="server")].image}' 2>/dev/null || true)"
  tag="${image##*:}"
  [ "$tag" != "$image" ] || tag=""
fi
if [ -z "$tag" ]; then
  echo "authority-job: no server is running in $NAMESPACE to take the image from; set IMAGE_TAG" >&2
  exit 2
fi

WORK="$(mktemp -d)"
manifest="$WORK/job.yaml"
helm template "$RELEASE" "$CHART" --namespace "$NAMESPACE" -f "$VALUES" \
  --set image.tag="$tag" --set authority.job.enabled=true \
  --set-json "authority.job.args=$(jq -cn '$ARGS.positional' --args -- "$@")" \
  --show-only templates/authority-tool-job.yaml >"$manifest"

trap 'kubectl_retry -n "$NAMESPACE" delete -f "$manifest" --ignore-not-found --wait=false >/dev/null || true
rm -rf "$WORK"' EXIT

# A finished job's command cannot be changed, so one left by an earlier run goes first.
kubectl_retry -n "$NAMESPACE" delete -f "$manifest" --ignore-not-found --wait=true >/dev/null
applied="$(kubectl_retry -n "$NAMESPACE" apply -f "$manifest" -o name)"
job="$(awk '$1 ~ /^job\.batch\// { print $1; exit }' <<<"$applied")"

outcome="unfinished"
waited=0
while :; do
  status="$(kubectl_retry -n "$NAMESPACE" get "$job" -o jsonpath='{.status.succeeded}/{.status.failed}')"
  case "$status" in
    [1-9]*/*) outcome="succeeded" ;;
    */[1-9]*) outcome="failed" ;;
  esac
  [ "$outcome" = "unfinished" ] && [ "$waited" -lt "$TIMEOUT" ] || break
  sleep "$POLL"
  waited=$((waited + POLL))
  [ "$POLL" -gt 0 ] || waited=$((waited + 1))
done

kubectl_retry -n "$NAMESPACE" logs "$job" --all-containers || true
case "$outcome" in
  succeeded) exit 0 ;;
  failed)
    echo "authority-job: $1 failed; the output above names the step" >&2
    exit 1
    ;;
  *)
    echo "authority-job: $1 did not finish within ${TIMEOUT}s" >&2
    exit 1
    ;;
esac
