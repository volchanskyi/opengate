#!/usr/bin/env bash
# Install the pinned release binaries of the linters `make lint-deploy` and the
# gauntlet run, into ~/.local/bin, from the same release URLs CI downloads.
#
# These had been installed once by hand, a release or more off the versions CI
# runs, and nothing compared the two until the gauntlet's prerequisite phase
# started asking (scripts/lib/toolchain-parity.sh). This is the command that
# answers it. Versions come from scripts/lib/tool-versions.sh.
#
# Usage: install-release-tools.sh [tool...]
#   tools: gitleaks hadolint helm kubeconform conftest tflint trivy actionlint
#   With no tool named, installs all of them. Each is asked its version after
#   the install, and a copy that does not answer with the pin fails the run.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tool-versions.sh
. "$SCRIPT_DIR/lib/tool-versions.sh"

BIN_DIR="${RELEASE_TOOLS_BIN_DIR:-$HOME/.local/bin}"
ALL_TOOLS=(gitleaks hadolint helm kubeconform conftest tflint trivy actionlint)

log() { printf '[install-release-tools] %s\n' "$1" >&2; }

case "$(uname -s):$(uname -m)" in
  Linux:x86_64 | Linux:amd64) arch=amd64 ;;
  Linux:aarch64 | Linux:arm64) arch=arm64 ;;
  *)
    log "unsupported platform $(uname -s)/$(uname -m); install the pinned versions from scripts/lib/tool-versions.sh by hand."
    exit 1
    ;;
esac

# release_url TOOL — the archive or binary for this platform, and the path of
# the binary inside an archive (empty for a bare binary).
release_url() {
  local v
  case "$1" in
    gitleaks)
      v="$TOOL_VERSION_GITLEAKS"
      [ "$arch" = amd64 ] && a=x64 || a=arm64
      printf 'https://github.com/gitleaks/gitleaks/releases/download/v%s/gitleaks_%s_linux_%s.tar.gz gitleaks' "$v" "$v" "$a"
      ;;
    hadolint)
      [ "$arch" = amd64 ] && a=x86_64 || a=arm64
      printf 'https://github.com/hadolint/hadolint/releases/download/v%s/hadolint-Linux-%s -' "$TOOL_VERSION_HADOLINT" "$a"
      ;;
    helm)
      printf 'https://get.helm.sh/helm-v%s-linux-%s.tar.gz linux-%s/helm' "$TOOL_VERSION_HELM" "$arch" "$arch"
      ;;
    kubeconform)
      printf 'https://github.com/yannh/kubeconform/releases/download/v%s/kubeconform-linux-%s.tar.gz kubeconform' "$TOOL_VERSION_KUBECONFORM" "$arch"
      ;;
    conftest)
      v="$TOOL_VERSION_CONFTEST"
      [ "$arch" = amd64 ] && a=x86_64 || a=arm64
      printf 'https://github.com/open-policy-agent/conftest/releases/download/v%s/conftest_%s_Linux_%s.tar.gz conftest' "$v" "$v" "$a"
      ;;
    tflint)
      printf 'https://github.com/terraform-linters/tflint/releases/download/v%s/tflint_linux_%s.zip tflint' "$TOOL_VERSION_TFLINT" "$arch"
      ;;
    trivy)
      v="$TOOL_VERSION_TRIVY"
      [ "$arch" = amd64 ] && a=64bit || a=ARM64
      printf 'https://github.com/aquasecurity/trivy/releases/download/v%s/trivy_%s_Linux-%s.tar.gz trivy' "$v" "$v" "$a"
      ;;
    actionlint)
      v="$TOOL_VERSION_ACTIONLINT"
      printf 'https://github.com/rhysd/actionlint/releases/download/v%s/actionlint_%s_linux_%s.tar.gz actionlint' "$v" "$v" "$arch"
      ;;
    *)
      return 1
      ;;
  esac
}

install_one() {
  local tool="$1" spec url inner work
  spec="$(release_url "$tool")" || {
    log "$tool is not a release tool this installs (known: ${ALL_TOOLS[*]})."
    return 1
  }
  url="${spec% *}"
  inner="${spec##* }"
  work="$(mktemp -d)"
  log "$tool: $url"
  curl --fail --silent --show-error --location --retry 3 --retry-delay 5 \
    --connect-timeout 10 --max-time 300 -o "$work/download" "$url"
  case "$url" in
    *.tar.gz) tar -xzf "$work/download" -C "$work" "$inner" ;;
    *.zip) unzip -q -o "$work/download" "$inner" -d "$work" ;;
    *) inner=download ;;
  esac
  mkdir -p "$BIN_DIR"
  install -m 0755 "$work/$inner" "$BIN_DIR/$tool"
  rm -rf "$work"
  # Asked the way the gauntlet asks it, of the copy PATH resolves.
  if ! PATH="$BIN_DIR:$PATH" "$SCRIPT_DIR/require-tool.sh" "$tool"; then
    log "$tool was installed to $BIN_DIR but does not answer with its pin."
    return 1
  fi
}

tools=("$@")
[ "${#tools[@]}" -gt 0 ] || tools=("${ALL_TOOLS[@]}")
for tool in "${tools[@]}"; do
  install_one "$tool"
done
case ":$PATH:" in
  *":$BIN_DIR:"*) : ;;
  *) log "$BIN_DIR is not on PATH; put it ahead of /usr/local/bin and /usr/bin." ;;
esac
