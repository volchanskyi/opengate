#!/usr/bin/env bash

# Prints the manifest for one machine pod the staging browser suite reads against.
# Pods run in the namespace because agents speak QUIC over UDP, which port-forward cannot carry.

# Usage:
#   MACHINE=agent-a RELEASE=… NODE_ARCH=… SERVER_POD_IP=… ENROLMENT_SECRET=… \
#     deploy/scripts/e2e-machine-pod.sh | kubectl -n NS apply -f -

set -euo pipefail

: "${MACHINE:?MACHINE is required}"
: "${RELEASE:?RELEASE is required}"
: "${NODE_ARCH:?NODE_ARCH is required}"
: "${SERVER_POD_IP:?SERVER_POD_IP is required}"
: "${ENROLMENT_SECRET:?ENROLMENT_SECRET is required}"

# The chart signs the server for this Service name, which the agent dials and verifies.
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
  # A dead machine stays down as a visible failed pod, so the suite never waits on a crash loop.
  restartPolicy: Never
  automountServiceAccountToken: false
  # The binary targets one architecture, so on a wrong-chip node the pod stays Pending and says why.
  nodeSelector:
    kubernetes.io/arch: ${NODE_ARCH}
  # The Service carries the HTTP port only, so QUIC goes to the server pod itself while the
  # name stays the one on the certificate.
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
        # The note is copied owner-only before the first start, as the installer leaves it.
        - >-
          while [ ! -f /tmp/ready ]; do sleep 1; done;
          mkdir -p /tmp/agent;
          if [ ! -f /tmp/agent/device_id.txt ]; then
          cp /run/opengate/install-note.json /tmp/agent/install-note.json;
          chmod 0600 /tmp/agent/install-note.json; fi;
          exec /tmp/mesh-agent
      env:
        - name: OPENGATE_SERVER_ADDR
          value: ${SERVER_NAME}:9090
        - name: OPENGATE_DATA_DIR
          value: /tmp/agent
        # The non-root container cannot write /var/log, so logs rotate here, where the server reads
        # a machine's logs back from on staging too.
        - name: OPENGATE_LOG_DIR
          value: /tmp/agent-logs
        - name: OPENGATE_SERVER_CA
          value: /tmp/agent/ca.pem
        - name: RUST_LOG
          value: info
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
      # The install note holds a token minted per run that expires within the hour.
      volumeMounts:
        - name: install-note
          mountPath: /run/opengate
          readOnly: true
  volumes:
    - name: install-note
      secret:
        secretName: ${ENROLMENT_SECRET}
        items:
          - key: note
            path: install-note.json
POD
