# The agent as the browser test stack runs it: one static binary on a bare
# Alpine, with no runtime dependencies of its own.
#
# The binary is built outside this image and staged into deploy/agent-bin/,
# because building it here would pay a full Rust compile on every stack
# bring-up — and because the cargo target tree the compiler writes into is tens
# of gigabytes and is excluded from the build context for that reason.
# `make agent-binary` stages it locally; CI's agent-binary job builds it once
# for the whole workflow and the e2e job downloads it to the same place.
#
# A containerized agent has no systemd, which the agent already detects: it
# falls back to a runtime with no service lifecycle. That is a supported shape
# rather than a workaround, and it is why Terminal and File Manager are the
# capabilities the browser specs exercise here.
FROM alpine:3.20

# hadolint ignore=DL3018
RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates \
    && addgroup -S opengate && adduser -S opengate -G opengate \
    && mkdir -p /var/lib/mesh-agent /var/log/mesh-agent \
    && chown opengate:opengate /var/lib/mesh-agent /var/log/mesh-agent

COPY deploy/agent-bin/mesh-agent /usr/local/bin/mesh-agent

# The agent reads the host it runs on and writes only its own data and log
# directories. It needs no privilege for either, and a machine in the test stack
# is a machine somebody could reach through the product, so it runs as an
# ordinary user.
USER opengate
ENV OPENGATE_DATA_DIR=/var/lib/mesh-agent
ENTRYPOINT ["/usr/local/bin/mesh-agent"]
