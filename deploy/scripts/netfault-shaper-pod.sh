#!/usr/bin/env bash
# Print the manifest for the link shaper the nightly network drill runs its
# machines through.
#
# The shaper sits between the drill's machines and the server: the machines dial
# the name on the server's certificate, a hostAliases entry points that name
# here instead of at the server pod, and every datagram is forwarded on to the
# server with whatever impairment the scenario has commanded. Enrolment does not
# come this way — it goes to the Service by its fully-qualified name over HTTP,
# which an /etc/hosts entry for the short name does not intercept — so this pod
# carries UDP and nothing else.
#
# Nothing here is privileged. The worker node ships no kernel network emulator,
# so every off-the-shelf impairment tool would need a privileged node agent on
# the one node that carries production; a forwarder in the path needs two
# ordinary sockets and no capability at all.
#
# The binary is not baked into an image: the drill builds it from the commit
# under test and copies it in. The container waits for /tmp/ready rather than
# for the binary, so a half-copied file is never executed.
#
# Usage:
#   MACHINE=drill-shaper RELEASE=… NODE_ARCH=… SERVER_POD_IP=… SHAPER_SEED=… \
#     deploy/scripts/netfault-shaper-pod.sh | kubectl -n NS apply -f -

set -euo pipefail

: "${MACHINE:?MACHINE is required}"
: "${RELEASE:?RELEASE is required}"
: "${NODE_ARCH:?NODE_ARCH is required}"
: "${SERVER_POD_IP:?SERVER_POD_IP is required}"
: "${SHAPER_SEED:?SHAPER_SEED is required}"

# The name on the machine-facing certificate. The machines dial it and it
# resolves here; the shaper forwards to the server pod's address, which is in no
# certificate and does not need to be — the shaper terminates nothing, and no
# key ever reaches it.
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
  # A shaper that dies stays dead and is visible as a failed pod. Restarting it
  # would give the scenario a fresh forwarder mid-phase, with empty counters and
  # new server-facing ports, and the run would read that as the product losing
  # its connection.
  restartPolicy: Never
  automountServiceAccountToken: false
  # The binary is built for one architecture. Without this a wrong-chip build
  # crash-loops; with it the pod stays Pending and says why.
  nodeSelector:
    kubernetes.io/arch: ${NODE_ARCH}
  # Where the forwarded datagrams go. The shaper resolves the server by the
  # same name its machines dial, so the address it forwards to is the server
  # pod itself — the Service carries the HTTP port only.
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
      # A forwarder copies datagrams between two sockets and holds no state
      # beyond one mapping per machine. The node is a single Always-Free worker
      # carrying production as well, and most of its processor is already
      # committed, so this asks for less than either machine pod beside it.
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
