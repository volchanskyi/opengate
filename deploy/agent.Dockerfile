# One static agent binary staged into deploy/agent-bin/; the container has no systemd, so the
# agent runs without a service lifecycle.
FROM alpine:3.20

# hadolint ignore=DL3018
RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates \
    && addgroup -S opengate && adduser -S opengate -G opengate \
    && mkdir -p /var/lib/mesh-agent /var/log/mesh-agent \
    && chown opengate:opengate /var/lib/mesh-agent /var/log/mesh-agent

COPY deploy/agent-bin/mesh-agent /usr/local/bin/mesh-agent

# The agent writes only its data and log directories, so it runs unprivileged.
USER opengate
ENV OPENGATE_DATA_DIR=/var/lib/mesh-agent
ENTRYPOINT ["/usr/local/bin/mesh-agent"]
