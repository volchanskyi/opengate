#!/usr/bin/env bash
# Print the manifest for one machine the staging browser suite reads against.
#
# The suite's specs open a terminal, list files and read hardware inventory on
# machines named agent-a and agent-b. Locally those are containers
# docker-compose.test.yml starts. On staging they are pods, created here, and a
# pod's hostname is its name — which is what web/e2e/helpers/enrolled-machine.ts
# looks the machine up by.
#
# They run in the namespace rather than on the runner because an agent speaks
# QUIC over UDP and `kubectl port-forward` carries TCP only. That is the same
# conclusion the nightly load run reached for its own fleet.
#
# The binary is not baked into an image: the deploy job builds it from the
# commit it is deploying and copies it in, so the machines and the server they
# enrol into are always the same version of the product. The container waits
# for /tmp/ready rather than for the binary, so a half-copied file is never
# executed.
#
# Usage:
#   MACHINE=agent-a RELEASE=… NODE_ARCH=… SERVER_POD_IP=… ENROLMENT_SECRET=… \
#     deploy/scripts/e2e-machine-pod.sh | kubectl -n NS apply -f -

set -euo pipefail

: "${MACHINE:?MACHINE is required}"
: "${RELEASE:?RELEASE is required}"
: "${NODE_ARCH:?NODE_ARCH is required}"
: "${SERVER_POD_IP:?SERVER_POD_IP is required}"
: "${ENROLMENT_SECRET:?ENROLMENT_SECRET is required}"

# The name on the machine-facing certificate. The chart signs the server for its
# own Service name, and the agent takes its TLS name from the host half of the
# address it is given with no way to be told another — so the name it dials and
# the name it verifies are this one and the same.
SERVER_NAME="${RELEASE}-server"

cat <<POD
apiVersion: v1
kind: Pod
metadata:
  name: ${MACHINE}
  labels:
    app.kubernetes.io/instance: ${RELEASE}
    app.kubernetes.io/component: e2e-machine
spec:
  # A machine that dies stays dead and is visible as a failed pod, rather than
  # crash-looping quietly while the suite times out on an empty fleet.
  restartPolicy: Never
  automountServiceAccountToken: false
  # The binary is built for one architecture. Without this a wrong-chip build
  # crash-loops; with it the pod stays Pending and says why.
  nodeSelector:
    kubernetes.io/arch: ${NODE_ARCH}
  # The Service carries the HTTP port only, so the QUIC packets are addressed to
  # the server pod itself — the path the nightly load run already carries a
  # hundred agents over — while the name stays the one on the certificate.
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
    - name: agent
      image: docker.io/library/alpine:3.21.3
      command:
        - /bin/sh
        - -c
        - 'while [ ! -f /tmp/ready ]; do sleep 1; done; exec /tmp/mesh-agent'
      env:
        - name: OPENGATE_SERVER_ADDR
          value: ${SERVER_NAME}:9090
        - name: OPENGATE_ENROLL_URL
          value: http://${SERVER_NAME}:8080
        - name: OPENGATE_DATA_DIR
          value: /tmp/agent
        - name: OPENGATE_SERVER_CA
          value: /tmp/agent/ca.pem
        - name: RUST_LOG
          value: info
        # Minted per run through the public enrolment endpoint and expiring
        # within the hour. It is in no checkout, and it reaches the machine
        # through a Secret rather than through this manifest.
        - name: OPENGATE_ENROLL_TOKEN
          valueFrom:
            secretKeyRef:
              name: ${ENROLMENT_SECRET}
              key: token
      # The node is a single Always-Free worker carrying production as well, so
      # a machine here asks for very little and is capped well below it.
      resources:
        requests:
          cpu: 25m
          memory: 64Mi
        limits:
          cpu: 300m
          memory: 256Mi
      securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop:
            - ALL
POD
