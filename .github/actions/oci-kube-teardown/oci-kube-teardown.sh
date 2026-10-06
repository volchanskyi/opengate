#!/usr/bin/env bash
# Removes the credentials oci-kube-setup wrote to the runner's home directory.
# Runs under if: always(), so it succeeds whether or not setup created the files.

set -euo pipefail

rm -rf "$HOME/.oci"
rm -f "$HOME/.kube/config"

echo "OCI credentials and kubeconfig removed"
