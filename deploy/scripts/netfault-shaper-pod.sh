#!/usr/bin/env bash

# Prints the manifest for the link shaper the nightly network drill runs its machines through.
# The shaper is unprivileged and carries only UDP; enrolment reaches the Service by its full name.

# The container waits for /tmp/ready, so a half-copied binary never runs.

# Usage:
#   MACHINE=drill-shaper RELEASE=… NODE_ARCH=… SERVER_POD_IP=… SHAPER_SEED=… \
#     deploy/scripts/netfault-shaper-pod.sh | kubectl -n NS apply -f -

set -euo pipefail

: "${MACHINE:?MACHINE is required}"
: "${RELEASE:?RELEASE is required}"
: "${NODE_ARCH:?NODE_ARCH is required}"
: "${SERVER_POD_IP:?SERVER_POD_IP is required}"
: "${SHAPER_SEED:?SHAPER_SEED is required}"

# The name on the machine-facing certificate; machines dial it and it resolves to the shaper.
SERVER_NAME="${RELEASE}-server"

cat <<POD
apiVersion: v1
kind: Pod
metadata:
  name: ${MACHINE}
  labels:
    app.kubernetes.io/instance: ${RELEASE}
    app.kubernetes.io/component: netfault-shaper
spec:
  # A shaper that dies stays down as a visible failed pod; a fresh forwarder mid-phase would bring
  # empty counters and new ports that the run would read as the product losing its connection.
  restartPolicy: Never
  automountServiceAccountToken: false
  # The binary targets one architecture, so on a wrong-chip node the pod stays Pending and says why.
  nodeSelector:
    kubernetes.io/arch: ${NODE_ARCH}
  # The shaper resolves the server by the name its machines dial, and the Service carries HTTP
  # only, so datagrams go to the server pod itself.
  hostAliases:
    - ip: ${SERVER_POD_IP}
      hostnames:
        - ${SERVER_NAME}
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    runAsGroup: 65532
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: shaper
      image: docker.io/library/alpine:3.21.3
      command:
        - /bin/sh
        - -c
        - 'while [ ! -f /tmp/ready ]; do sleep 1; done; exec /tmp/netfault -listen=:9090 -server=${SERVER_NAME}:9090 -control=:9091 -seed=${SHAPER_SEED}'
      ports:
        - name: quic
          containerPort: 9090
          protocol: UDP
        - name: control
          containerPort: 9091
          protocol: TCP
      # A forwarder holds one mapping per machine on a single Always-Free worker shared with
      # production, so it asks for less than either machine pod beside it.
      resources:
        requests:
          cpu: 50m
          memory: 64Mi
        limits:
          cpu: 300m
          memory: 192Mi
      securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop:
            - ALL
POD
