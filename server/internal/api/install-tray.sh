#!/usr/bin/env bash
# OpenGate Agent Tray Installer
# Installs the system tray companion for desktop machines.
#
# Usage: sudo bash install-tray.sh
#   or:  curl -sL https://<server>/api/v1/server/install-tray.sh | sudo bash
#
# The tray binary communicates with the mesh-agent service via Unix socket IPC.
# It must be installed AFTER the agent (install.sh).
set -euo pipefail

readonly INSTALL_DIR="/usr/local/bin"
readonly TRAY_BINARY="mesh-agent-tray"
readonly AGENT_SERVICE="mesh-agent"
readonly XDG_AUTOSTART_DIR="/etc/xdg/autostart"

# Runtime library packages required by the tray binary (dynamically linked).
# Debian/Ubuntu names → Fedora/RHEL names.
declare -A DEB_DEPS=(
    [libgtk-3-0]="GTK3 toolkit"
    [libglib2.0-0]="GLib runtime"
    [libgdk-pixbuf-2.0-0]="GDK-Pixbuf"
    [libdbus-1-3]="D-Bus"
    [libayatana-appindicator3-1]="System tray (SNI)"
    [libxdo3]="X keyboard automation"
)
declare -A RPM_DEPS=(
    [gtk3]="GTK3 toolkit"
    [glib2]="GLib runtime"
    [gdk-pixbuf2]="GDK-Pixbuf"
    [dbus-libs]="D-Bus"
    [libappindicator-gtk3]="System tray (SNI)"
    [xdotool]="X keyboard automation"
)

# --- Helpers ----------------------------------------------------------------

log()  { printf '[opengate-tray] %s\n' "$*"; return 0; }
fail() { printf '[opengate-tray] ERROR: %s\n' "$*" >&2; exit 1; return 1; }
warn() { printf '[opengate-tray] WARN: %s\n' "$*" >&2; return 0; }

# --- Pre-flight checks ------------------------------------------------------

[[ $EUID -eq 0 ]] || fail "This script must be run as root (use sudo)"

# Detect graphical desktop environment.
# The tray requires a display server (X11 or Wayland) and a desktop session.
has_desktop() {
    # Check 1: Is a display server installed?
    # Look for Xorg, Xwayland, or a Wayland compositor binary.
    local found_display=false
    for bin in Xorg Xwayland sway gnome-shell kwin_wayland mutter; do
        if command -v "$bin" &>/dev/null; then
            found_display=true
            break
        fi
    done

    # Check 2: Are any desktop sessions registered?
    # /usr/share/xsessions/ or /usr/share/wayland-sessions/ exist on desktop installs.
    if [[ -d /usr/share/xsessions ]] || [[ -d /usr/share/wayland-sessions ]]; then
        found_display=true
    fi

    # Check 3: Is this WSL? WSLg has X11 but it's a virtual display — no real tray support.
    if grep -qi microsoft /proc/version 2>/dev/null; then
        warn "WSL detected. System tray is not supported in WSL environments."
        return 1
    fi

    # Check 4: Is this a container?
    if [[ -f /.dockerenv ]] || [[ -f /run/.containerenv ]]; then
        warn "Container detected. System tray requires a desktop environment."
        return 1
    fi

    "$found_display" && return 0

    return 1
}

if ! has_desktop; then
    log "No desktop environment detected, skipping tray installation."
    exit 0
fi

log "Desktop environment detected, installing system tray..."

# Verify the agent is installed.
if ! systemctl is-enabled "$AGENT_SERVICE" &>/dev/null; then
    fail "mesh-agent service not found. Install the agent first (install.sh)."
fi

# --- Detect package manager and install missing deps -----------------------

install_deps() {
    if command -v apt-get &>/dev/null; then
        install_deb_deps
    elif command -v dnf &>/dev/null; then
        install_rpm_deps "dnf"
    elif command -v yum &>/dev/null; then
        install_rpm_deps "yum"
    else
        warn "Unknown package manager. Please install these libraries manually:"
        for pkg in "${!DEB_DEPS[@]}"; do
            warn "  - ${DEB_DEPS[$pkg]} ($pkg / ${RPM_DEPS[*]})"
        done
        return 1
    fi
}

install_deb_deps() {
    local missing=()
    for pkg in "${!DEB_DEPS[@]}"; do
        # Handle Ubuntu 24.04+ t64 suffix (libgtk-3-0t64 etc.)
        if ! dpkg -l "$pkg" 2>/dev/null | grep -q "^ii" && \
           ! dpkg -l "${pkg}t64" 2>/dev/null | grep -q "^ii"; then
            missing+=("$pkg")
        fi
    done

    if [[ ${#missing[@]} -eq 0 ]]; then
        log "All runtime dependencies already installed"
        return 0
    fi

    log "Installing missing dependencies: ${missing[*]}"
    apt-get update -qq
    apt-get install -y -qq "${missing[@]}" || {
        fail "Failed to install: ${missing[*]}"
    }
}

install_rpm_deps() {
    local pm="$1"
    local missing=()
    for pkg in "${!RPM_DEPS[@]}"; do
        if ! rpm -q "$pkg" &>/dev/null; then
            missing+=("$pkg")
        fi
    done

    if [[ ${#missing[@]} -eq 0 ]]; then
        log "All runtime dependencies already installed"
        return 0
    fi

    log "Installing missing dependencies: ${missing[*]}"
    "$pm" install -y "${missing[@]}" || {
        fail "Failed to install: ${missing[*]}"
    }
}

install_deps

# --- Download tray binary ---------------------------------------------------

DOWNLOAD_URL=""

# Derive server URL from the agent's systemd unit.
SERVER_URL="${OPENGATE_SERVER:-}"
if [[ -z "$SERVER_URL" ]]; then
    UNIT_FILE="/etc/systemd/system/${AGENT_SERVICE}.service"
    if [[ -f "$UNIT_FILE" ]]; then
        SERVER_URL=$(grep -oP '(?<=--enroll-url )\S+' "$UNIT_FILE" || true)
    fi
fi

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) fail "Unsupported architecture: $ARCH" ;;
esac

# Try GitHub Releases.
GITHUB_REPO="${OPENGATE_GITHUB_REPO:-}"
if [[ -n "$GITHUB_REPO" ]]; then
    log "Looking for tray binary on GitHub (${GITHUB_REPO})..."
    ASSET_NAME="${TRAY_BINARY}-${OS}-${ARCH}"
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
    fi
fi

if [[ -z "$DOWNLOAD_URL" ]]; then
    fail "No tray binary found for ${OS}/${ARCH}. Set OPENGATE_GITHUB_REPO or download manually."
fi

log "Downloading tray from: ${DOWNLOAD_URL}"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

curl -fL --max-time 300 -o "${TMPDIR}/${TRAY_BINARY}" "$DOWNLOAD_URL" \
    || fail "Failed to download tray binary"

# --- Install binary ---------------------------------------------------------

install -m 0755 "${TMPDIR}/${TRAY_BINARY}" "${INSTALL_DIR}/${TRAY_BINARY}"
log "Installed: ${INSTALL_DIR}/${TRAY_BINARY}"

# --- Create XDG autostart entry ---------------------------------------------
# This makes the tray start automatically when any user logs into a graphical session.

mkdir -p "$XDG_AUTOSTART_DIR"
cat > "${XDG_AUTOSTART_DIR}/${TRAY_BINARY}.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=OpenGate Agent Tray
Comment=System tray for OpenGate remote management agent
Exec=${INSTALL_DIR}/${TRAY_BINARY}
Icon=network-server
Terminal=false
Categories=System;Monitor;
StartupNotify=false
X-GNOME-Autostart-enabled=true
DESKTOP

log "XDG autostart: ${XDG_AUTOSTART_DIR}/${TRAY_BINARY}.desktop"

# --- Ensure IPC socket directory permissions --------------------------------
# The agent creates /run/mesh-agent/tray.sock as root.
# Desktop users need read/write access. Use a group.

if ! getent group mesh-agent &>/dev/null; then
    groupadd --system mesh-agent
    log "Created system group: mesh-agent"
fi

# Add all human users (UID >= 1000) to the mesh-agent group.
while IFS=: read -r username _ uid _; do
    if [[ "$uid" -ge 1000 && "$uid" -lt 65534 ]]; then
        if ! id -nG "$username" | grep -qw mesh-agent; then
            usermod -aG mesh-agent "$username"
            log "Added $username to mesh-agent group"
        fi
    fi
done < /etc/passwd

log ""
log "Tray installed successfully!"
log "  Binary:    ${INSTALL_DIR}/${TRAY_BINARY}"
log "  Autostart: ${XDG_AUTOSTART_DIR}/${TRAY_BINARY}.desktop"
log ""
log "The tray will start automatically on next graphical login."
log "To start now: ${INSTALL_DIR}/${TRAY_BINARY} &"
