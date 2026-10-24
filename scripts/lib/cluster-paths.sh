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

# cluster_rows prints each manifest row whose pin the cluster job installs, one a line.
cluster_rows() {
  printf '%s\n' TOOL_VERSION_INGRESS_NGINX_CHART TOOL_VERSION_CERT_MANAGER_CHART
}

# cluster_rows_changed FROM TO prints each cluster row whose pin differs between the two commits,
# and fails when none does.
cluster_rows_changed() {
  local from to row changed=1
  from="$(git show "$1:scripts/lib/tool-versions.sh" 2>/dev/null || true)"
  to="$(git show "$2:scripts/lib/tool-versions.sh" 2>/dev/null || true)"
  while IFS= read -r row; do
    if [ "$(grep -E "^export $row=" <<<"$from" || true)" != "$(grep -E "^export $row=" <<<"$to" || true)" ]; then
      printf '%s\n' "$row"
      changed=0
    fi
  done < <(cluster_rows)
  return "$changed"
}
