#!/usr/bin/env bash
# Writes the kubelet's image clean-up settings, then runs OKE's own start-up with the kubelet
# reading them. imageMaximumGCAge exists only as a settings-file field.

set -euo pipefail

mkdir -p /etc/kubernetes/kubelet.conf.d
# Clean-up starts below the default imagefs eviction line, which on this node is the root disk.
cat >/etc/kubernetes/kubelet.conf.d/50-image-gc.conf <<'JSON'
{
  "apiVersion": "kubelet.config.k8s.io/v1beta1",
  "kind": "KubeletConfiguration",
  "imageMaximumGCAge": "168h",
  "imageGCHighThresholdPercent": 80,
  "imageGCLowThresholdPercent": 75
}
JSON

# The instance metadata service answers on IPv4, and on IPv6 in an IPv6-only subnet.
oke_init_script="$(curl --fail -m 5 -H "Authorization: Bearer Oracle" -L0 \
  http://169.254.169.254/opc/v2/instance/metadata/oke_init_script)" \
  || oke_init_script="$(curl --fail -m 5 -g -H "Authorization: Bearer Oracle" -L0 \
    'http://[fd00:c1::a9fe:a9fe]/opc/v2/instance/metadata/oke_init_script')"
base64 --decode <<<"$oke_init_script" >/var/run/oke-init.sh
bash /var/run/oke-init.sh --kubelet-extra-args "--config-dir=/etc/kubernetes/kubelet.conf.d"
