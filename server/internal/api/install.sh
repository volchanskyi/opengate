#!/usr/bin/env bash
# OpenGate Agent Installer
# Usage: curl -sL https://<server>/api/v1/server/install.sh | sudo bash -s -- <ENROLLMENT_TOKEN>
set -euo pipefail

readonly INSTALL_DIR="/usr/local/bin"
readonly CONFIG_DIR="/etc/opengate-agent"
readonly DATA_DIR="/var/lib/opengate-agent"
readonly SERVICE_NAME="mesh-agent"
readonly BINARY_NAME="mesh-agent"

# --- Helpers ----------------------------------------------------------------

log()  { printf '[opengate] %s\n' "$*"; return 0; }
fail() { printf '[opengate] ERROR: %s\n' "$*" >&2; exit 1; return 1; }

# --- Pre-flight checks ------------------------------------------------------

[[ $EUID -eq 0 ]] || fail "This script must be run as root (use sudo)"
command -v curl >/dev/null 2>&1 || fail "curl is required but not installed"
command -v systemctl >/dev/null 2>&1 || fail "systemd is required but not found"

# --- Parse arguments --------------------------------------------------------

ENROLLMENT_TOKEN="${1:-}"
[[ -n "$ENROLLMENT_TOKEN" ]] || fail "Usage: $0 <ENROLLMENT_TOKEN>"

# Derive the server URL from OPENGATE_SERVER env or from the download source.
if [[ -n "${OPENGATE_SERVER:-}" ]]; then
    SERVER_URL="${OPENGATE_SERVER}"
else
    # When piped via curl, try to extract the server from /proc.
    # Fallback: user must set OPENGATE_SERVER.
    CMDLINE=$(tr '\0' ' ' < /proc/$PPID/cmdline 2>/dev/null || true)
    if [[ "$CMDLINE" =~ https?://([^/]+) ]]; then
        SERVER_URL="${BASH_REMATCH[0]%%/api/*}"
    else
        fail "Cannot determine server URL. Set OPENGATE_SERVER=https://your-server"
    fi
fi

log "Server: ${SERVER_URL}"

# --- Detect platform -------------------------------------------------------

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) fail "Unsupported architecture: $ARCH" ;;
esac

[[ "$OS" == "linux" ]] || fail "Unsupported OS: $OS (only linux is supported)"

log "Platform: ${OS}/${ARCH}"

# --- Validate enrollment token before downloading ----------------------------

log "Validating enrollment token..."
ENROLL_RESPONSE=$(curl -sf --max-time 30 \
    -X POST "${SERVER_URL}/api/v1/enroll/${ENROLLMENT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{"csr_pem":""}') \
    || fail "Enrollment failed. Check token validity and server URL."

SERVER_ADDR=$(echo "$ENROLL_RESPONSE" | grep -oP '"server_addr"\s*:\s*"\K[^"]*')

[[ -n "$SERVER_ADDR" ]] || fail "No server address in enrollment response"

log "Server QUIC address: ${SERVER_ADDR}"

# --- Resolve binary download URL and SHA256 ---------------------------------

DOWNLOAD_URL=""
EXPECTED_SHA256=""

# Strategy 1: Try GitHub Releases if OPENGATE_GITHUB_REPO is set.
GITHUB_REPO="${OPENGATE_GITHUB_REPO:-}"
if [[ -n "$GITHUB_REPO" ]]; then
    log "Fetching latest release from GitHub (${GITHUB_REPO})..."
    ASSET_NAME="mesh-agent-${OS}-${ARCH}"
    GH_RELEASE=$(curl -sf --max-time 30 \
        "https://api.github.com/repos/${GITHUB_REPO}/releases/latest") || true

    if [[ -n "$GH_RELEASE" ]]; then
        DOWNLOAD_URL=$(echo "$GH_RELEASE" | python3 -c "
import json, sys
release = json.load(sys.stdin)
for a in release.get('assets', []):
    if a['name'] == '${ASSET_NAME}':
        print(a['browser_download_url'])
        sys.exit(0)
sys.exit(1)
" 2>/dev/null) || true

        SHA256_URL=$(echo "$GH_RELEASE" | python3 -c "
import json, sys
release = json.load(sys.stdin)
for a in release.get('assets', []):
    if a['name'] == '${ASSET_NAME}.sha256':
        print(a['browser_download_url'])
        sys.exit(0)
sys.exit(1)
" 2>/dev/null) || true

        if [[ -n "$SHA256_URL" ]]; then
            EXPECTED_SHA256=$(curl -sfL --max-time 15 "$SHA256_URL" | awk '{print $1}') || true
        fi
    fi

    if [[ -n "$DOWNLOAD_URL" ]]; then
        log "Resolved binary from GitHub Releases"
    else
        log "GitHub Releases lookup failed, falling back to server manifests"
    fi
fi

# Strategy 2: Fall back to server manifests.
if [[ -z "$DOWNLOAD_URL" ]]; then
    log "Fetching agent manifest for ${OS}/${ARCH} from server..."
    MANIFESTS=$(curl -sf --max-time 30 \
        "${SERVER_URL}/api/v1/updates/manifests") || true

    if [[ -n "$MANIFESTS" ]]; then
        DOWNLOAD_URL=$(echo "$MANIFESTS" | python3 -c "
import json, sys
manifests = json.load(sys.stdin)
for m in manifests:
    if m.get('os') == '${OS}' and m.get('arch') == '${ARCH}':
        print(m['url'])
        sys.exit(0)
sys.exit(1)
" 2>/dev/null) || true

        if [[ -z "$EXPECTED_SHA256" ]]; then
            EXPECTED_SHA256=$(echo "$MANIFESTS" | python3 -c "
import json, sys
manifests = json.load(sys.stdin)
for m in manifests:
    if m.get('os') == '${OS}' and m.get('arch') == '${ARCH}':
        print(m['sha256'])
        sys.exit(0)
sys.exit(1)
" 2>/dev/null) || true
        fi
    fi
fi

[[ -n "$DOWNLOAD_URL" ]] || fail "No agent binary found for ${OS}/${ARCH}. Set OPENGATE_GITHUB_REPO or ask your admin to publish a manifest."

log "Downloading agent from: ${DOWNLOAD_URL}"

# --- Download binary --------------------------------------------------------

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

curl -fL --max-time 300 -o "${TMPDIR}/${BINARY_NAME}" "$DOWNLOAD_URL" \
    || fail "Failed to download agent binary"

# Verify SHA256 if available.
if [[ -n "$EXPECTED_SHA256" ]]; then
    ACTUAL_SHA256=$(sha256sum "${TMPDIR}/${BINARY_NAME}" | awk '{print $1}')
    if [[ "$ACTUAL_SHA256" != "$EXPECTED_SHA256" ]]; then
        fail "SHA256 mismatch: expected ${EXPECTED_SHA256}, got ${ACTUAL_SHA256}"
    fi
    log "SHA256 verified"
fi

# --- Install ----------------------------------------------------------------

log "Installing agent..."

# Binary
install -m 0755 "${TMPDIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"

# Config + data directories
mkdir -p "$CONFIG_DIR"
mkdir -p "$DATA_DIR"

# --- Systemd service --------------------------------------------------------
# On first boot the agent uses --enroll-url and --enroll-token to obtain a
# CA-signed certificate via CSR enrollment. On subsequent restarts, the agent
# loads the saved identity from DATA_DIR and ignores the enrollment flags.

cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=OpenGate Agent
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=${INSTALL_DIR}/${BINARY_NAME} \\
  --server-addr ${SERVER_ADDR} \\
  --server-ca ${CONFIG_DIR}/ca.pem \\
  --data-dir ${DATA_DIR} \\
  --enroll-url ${SERVER_URL} \\
  --enroll-token ${ENROLLMENT_TOKEN}
Restart=on-failure
RestartForceExitStatus=42
User=root

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now "${SERVICE_NAME}"

log "Agent installed and started successfully!"
log "  Binary:  ${INSTALL_DIR}/${BINARY_NAME}"
log "  Config:  ${CONFIG_DIR}/"
log "  Data:    ${DATA_DIR}/"
log "  Service: ${SERVICE_NAME}.service"
log ""
log "Check status: systemctl status ${SERVICE_NAME}"

# --- Auto-detect desktop and install system tray ----------------------------
# On desktop machines: install tray binary, runtime deps, and XDG autostart.
# On headless/CLI/WSL/container: skip silently. No manual flags needed.

log ""
log "Detecting desktop environment..."

TRAY_SCRIPT_URL="${SERVER_URL}/api/v1/server/install-tray.sh"
if curl -sf --max-time 10 -o /dev/null "$TRAY_SCRIPT_URL"; then
    curl -sf --max-time 10 "$TRAY_SCRIPT_URL" | bash
else
    log "Tray installer not available on server, skipping desktop integration."
fi
