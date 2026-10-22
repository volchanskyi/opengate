#!/usr/bin/env bash
# The repository paths whose change sends a deploy through the approval-gated cluster job, which
# applies everything a routine deploy's login cannot write.

# cluster_paths prints each path on a line of its own.
cluster_paths() {
  printf '%s\n' \
    deploy/cluster \
    deploy/ingress-nginx \
    deploy/helm/monitoring/templates/cluster-rbac.yaml \
    deploy/helm/opengate/templates/clusterissuer.yaml \
    deploy/scripts/cluster-apply.sh \
    deploy/scripts/helm-release-migrate.sh \
    deploy/scripts/ingress-controller-apply.sh \
    scripts/lib/cluster-paths.sh
}
