#!/usr/bin/env bash
# Remove the credentials oci-kube-setup wrote to the runner's home directory.
#
# Runs with if: always(), so it must succeed whether or not the setup action got
# far enough to create the files.

set -euo pipefail

rm -rf "$HOME/.oci"
rm -f "$HOME/.kube/config"

echo "OCI credentials and kubeconfig removed"
