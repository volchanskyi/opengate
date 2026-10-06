#!/usr/bin/env bash
# Configure OCI credentials and install clients used to reach OKE.

set -euo pipefail

# shellcheck source=../../../scripts/lib/tool-versions.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)/scripts/lib/tool-versions.sh"

# Oracle's client warns on standard error at every call until the key file ends with this line.
OCI_KEY_LABEL="OCI_API_KEY"

install_oci_cli() {
  pip install --quiet "oci-cli==${TOOL_VERSION_OCI_CLI}"
}

configure_oci() {
  : "${OCI_TENANCY:?OCI_TENANCY is required}"
  : "${OCI_USER:?OCI_USER is required}"
  : "${OCI_FINGERPRINT:?OCI_FINGERPRINT is required}"
  : "${OCI_KEY:?OCI_KEY is required}"
  : "${OCI_REGION:?OCI_REGION is required}"

  mkdir -p "$HOME/.oci"
  printf '%s\n' "$OCI_KEY" >"$HOME/.oci/key.pem"
  if [ "$(tail -n 1 "$HOME/.oci/key.pem")" != "$OCI_KEY_LABEL" ]; then
    printf '%s\n' "$OCI_KEY_LABEL" >>"$HOME/.oci/key.pem"
  fi
  chmod 600 "$HOME/.oci/key.pem"
  cat >"$HOME/.oci/config" <<EOF
[DEFAULT]
tenancy=$OCI_TENANCY
user=$OCI_USER
fingerprint=$OCI_FINGERPRINT
key_file=$HOME/.oci/key.pem
region=$OCI_REGION
EOF
  chmod 600 "$HOME/.oci/config"
}

install_kube_tools() {
  local work_dir="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
  local kubectl_version

  kubectl_version="$(curl -fsSL https://dl.k8s.io/release/stable.txt)"
  curl -fsSL "https://dl.k8s.io/release/${kubectl_version}/bin/linux/amd64/kubectl" -o "$work_dir/kubectl"
  sudo install -m 0755 "$work_dir/kubectl" /usr/local/bin/kubectl

  curl -fsSL "https://get.helm.sh/helm-v${TOOL_VERSION_HELM}-linux-amd64.tar.gz" -o "$work_dir/helm.tgz"
  tar -xzf "$work_dir/helm.tgz" -C "$work_dir" linux-amd64/helm
  sudo install -m 0755 "$work_dir/linux-amd64/helm" /usr/local/bin/helm

  kubectl version --client
  helm version --short
}

fetch_kubeconfig() {
  local cluster_id="${CLUSTER_ID:-}"
  : "${OCI_REGION:?OCI_REGION is required}"

  if [ -z "$cluster_id" ]; then
    echo "::error::cluster-id (OKE_CLUSTER_ID) is empty"
    exit 1
  fi

  mkdir -p "$HOME/.kube"
  oci ce cluster create-kubeconfig \
    --cluster-id "$cluster_id" \
    --file "$HOME/.kube/config" \
    --region "$OCI_REGION" \
    --token-version 2.0.0 \
    --kube-endpoint PUBLIC_ENDPOINT
  chmod 600 "$HOME/.kube/config"
  kubectl cluster-info >/dev/null
  echo "kubeconfig ready"
}

case "${1:-}" in
  install-oci-cli) install_oci_cli ;;
  configure-oci) configure_oci ;;
  install-kube-tools) install_kube_tools ;;
  fetch-kubeconfig) fetch_kubeconfig ;;
  *)
    echo "usage: $0 {install-oci-cli|configure-oci|install-kube-tools|fetch-kubeconfig}" >&2
    exit 2
    ;;
esac
