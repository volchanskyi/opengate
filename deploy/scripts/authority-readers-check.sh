#!/usr/bin/env bash
# Proves the cluster rule guarding the master key: it is in place, it refuses every outside pod by
# name, and it admits the server's own pod, each tried as a server-side dry run that keeps nothing.
#
# Usage: deploy/scripts/authority-readers-check.sh
#
# Environment:
#   NAMESPACE  namespace of the release, default opengate
#   RELEASE    release name, default opengate
#
# Exit codes:
#   0  the rule is in place, refuses every outside pod and admits the server's own pod
#   1  the rule is missing or unbound, admits an outside pod, or refuses the server's own pod
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NAMESPACE="${NAMESPACE:-opengate}"
RELEASE="${RELEASE:-opengate}"
RULE="opengate-authority-readers"
SECRET="opengate-authority"

# shellcheck source=scripts/lib/kubectl-retry.sh
. "$ROOT/scripts/lib/kubectl-retry.sh"

say() { echo "authority-readers-check: $1"; }
failed=0
complain() {
  say "$1" >&2
  failed=1
}

kubectl_retry get validatingadmissionpolicy "$RULE" -o name >/dev/null \
  || {
    complain "the cluster has no rule $RULE"
    exit 1
  }
kubectl_retry get validatingadmissionpolicybinding "$RULE" -o name >/dev/null \
  || {
    complain "the rule $RULE is bound to nothing"
    exit 1
  }

deployment="$(kubectl_retry -n "$NAMESPACE" get deploy "$RELEASE-server" -o json)" \
  || {
    complain "no server deployment $RELEASE-server in $NAMESPACE to take the server's pod from"
    exit 1
  }
server="$(jq -c '.spec.template.spec' <<<"$deployment")"

# An outside pod meets the namespace's pod-security rules, so only the rule can refuse it.
outside="$(jq -cn '{
  serviceAccountName: "default",
  automountServiceAccountToken: false,
  securityContext: {runAsNonRoot: true, runAsUser: 65532, seccompProfile: {type: "RuntimeDefault"}},
  containers: [{
    name: "probe", image: "docker.io/library/busybox:1.36", command: ["true"],
    resources: {limits: {cpu: "10m", memory: "16Mi"}},
    securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true,
      capabilities: {drop: ["ALL"]}}
  }]
}')"

# try NAME SPEC WANT asks the cluster to admit a pod with SPEC, and holds the answer to WANT.
try() {
  local name="$1" spec="$2" want="$3" pod out status=0
  pod="$(jq -cn --argjson spec "$spec" \
    '{apiVersion: "v1", kind: "Pod", metadata: {generateName: "authority-check-"}, spec: $spec}')"
  out="$(kubectl_retry --stdin -n "$NAMESPACE" create --dry-run=server -f - -o name 2>&1 <<<"$pod")" \
    || status=$?
  if [ "$want" = admitted ]; then
    if [ "$status" -eq 0 ]; then say "admitted: $name"; else complain "$name is refused: $out"; fi
  elif [ "$status" -eq 0 ]; then
    complain "admitted, and the rule should refuse it: $name"
  elif grep -qF -- "$RULE" <<<"$out"; then
    say "refused by the rule: $name"
  else
    complain "$name: refused, but by something other than $RULE: $out"
  fi
}

try "the server's own pod" "$server" admitted
try "another account on the server's image" "$(jq -c '.serviceAccountName = "default"' <<<"$server")" refused
try "the server's account on another image" \
  "$(jq -c '(.containers[], (.initContainers // [])[]).image = "docker.io/library/busybox:1.36"' <<<"$server")" refused
try "a secret volume" "$(jq -c --arg s "$SECRET" \
  '.volumes = [{name: "key", secret: {secretName: $s}}]
   | .containers[0].volumeMounts = [{name: "key", mountPath: "/key", readOnly: true}]' <<<"$outside")" refused
try "a projected volume" "$(jq -c --arg s "$SECRET" \
  '.volumes = [{name: "key", projected: {sources: [{secret: {name: $s}}]}}]
   | .containers[0].volumeMounts = [{name: "key", mountPath: "/key", readOnly: true}]' <<<"$outside")" refused
try "a variable read from the secret" "$(jq -c --arg s "$SECRET" \
  '.containers[0].env = [{name: "KEY", valueFrom: {secretKeyRef: {name: $s, key: "ca.key"}}}]' <<<"$outside")" refused
try "every variable from the secret" "$(jq -c --arg s "$SECRET" \
  '.containers[0].envFrom = [{secretRef: {name: $s}}]' <<<"$outside")" refused

exit "$failed"
