#!/usr/bin/env bash
# Refuses a tool that is missing or at a version other than its pin and prints the install command.
#
# Usage: require-tool.sh <tool>
#
# Exit codes:
#   0  present at the pin
#   1  missing or drifted, the install command is printed
#   2  a tool with no pin here
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tool-versions.sh
. "$HERE/lib/tool-versions.sh"

manifest_key() {
  case "$1" in
    age-keygen) printf 'AGE' ;;
    *) printf '%s' "$1" | tr 'a-z-' 'A-Z_' ;;
  esac
}

# go_module_version reads the module version from a Go binary's build record, which `go install`
# builds stamp even when their own version flag prints `dev`.
go_module_version() {
  local path
  path="$(command -v "$1")" || return 0
  go version -m "$path" 2>/dev/null | awk '$1 == "mod" { sub(/^v/, "", $3); print $3 }'
}

# installed_version TOOL — what the copy this machine resolves says it is, or
# nothing when there is no copy. Each tool is asked the way it words its answer.
installed_version() {
  command -v "$1" >/dev/null 2>&1 || return 0
  case "$1" in
    jq) jq --version 2>/dev/null | sed -n '1{s/^jq-//;p;}' ;;
    shellcheck) shellcheck --version 2>/dev/null | awk '/^version:/ { print $2 }' ;;
    shfmt) shfmt --version 2>/dev/null | sed -n '1{s/^v//;p;}' ;;
    age | age-keygen) "$1" --version 2>/dev/null | sed -n '1{s/^v//;p;}' ;;
    zstd) zstd --version 2>/dev/null | sed -n 's/.* v\([0-9][0-9.]*\),.*/\1/p' ;;
    govulncheck | staticcheck | gosec | go-arch-lint | oapi-codegen | gremlins)
      go_module_version "$1"
      ;;
    # A cargo subcommand expects its own name first, as cargo would pass it.
    cargo-*) "$1" "${1#cargo-}" --version 2>/dev/null | awk 'NR == 1 { print $2 }' ;;
    checkov) checkov --version 2>/dev/null | sed -n 1p ;;
    yamllint) yamllint --version 2>/dev/null | awk 'NR == 1 { print $2 }' ;;
    conftest) conftest --version 2>/dev/null | sed -n 's/^Conftest: //p' ;;
    gitleaks) gitleaks version 2>/dev/null | sed -n '1{s/^v//;p;}' ;;
    hadolint) hadolint --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | sed -n 1p ;;
    helm) helm version --short 2>/dev/null | sed -n '1{s/^v//;s/+.*//;p;}' ;;
    kubeconform) kubeconform -v 2>/dev/null | sed -n '1{s/^v//;p;}' ;;
    tflint) tflint --version 2>/dev/null | sed -n 's/^TFLint version //p' ;;
    trivy) trivy --version 2>/dev/null | sed -n 's/^Version: //p' ;;
    actionlint) actionlint --version 2>/dev/null | sed -n '1{s/^v//;p;}' ;;
    # semgrep prints an upgrade notice above its version when one exists.
    semgrep) semgrep --version 2>/dev/null | sed -n '$p' ;;
    pmat) pmat --version 2>/dev/null | awk 'NR == 1 { print $2 }' ;;
  esac
}

# install_command prints how to install the tool at its pinned version and refuses an unknown tool.
install_command() {
  case "$1" in
    jq | shellcheck | shfmt)
      printf 'scripts/install-shell-tools.sh'
      ;;
    # These open the endurance run's encrypted dump; the installer reads their manifest pins.
    age | age-keygen | zstd)
      printf 'scripts/install-dump-tools.sh   # age %s, zstd %s' "$TOOL_VERSION_AGE" "$TOOL_VERSION_ZSTD"
      ;;
    semgrep)
      printf 'scripts/install-semgrep.sh'
      ;;
    gitleaks | hadolint | helm | kubeconform | conftest | tflint | trivy | actionlint)
      printf 'scripts/install-release-tools.sh %s' "$1"
      ;;
    govulncheck)
      printf 'go install golang.org/x/vuln/cmd/govulncheck@v%s' "$TOOL_VERSION_GOVULNCHECK"
      ;;
    staticcheck)
      printf 'go install honnef.co/go/tools/cmd/staticcheck@v%s' "$TOOL_VERSION_STATICCHECK"
      ;;
    gosec)
      printf 'go install github.com/securego/gosec/v2/cmd/gosec@v%s' "$TOOL_VERSION_GOSEC"
      ;;
    go-arch-lint)
      printf 'go install github.com/fe3dback/go-arch-lint@v%s' "$TOOL_VERSION_GO_ARCH_LINT"
      ;;
    oapi-codegen)
      printf 'go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v%s' "$TOOL_VERSION_OAPI_CODEGEN"
      ;;
    gremlins)
      printf 'go install github.com/go-gremlins/gremlins/cmd/gremlins@v%s' "$TOOL_VERSION_GREMLINS"
      ;;
    cargo-audit | cargo-deny | cargo-modules | cargo-nextest | cargo-llvm-cov | cargo-fuzz | cargo-mutants | pmat)
      printf 'cargo install --locked --version %s %s' "$(tool_version "$(manifest_key "$1")")" "$1"
      ;;
    yamllint)
      printf 'pip install --user yamllint==%s' "$TOOL_VERSION_YAMLLINT"
      ;;
    checkov)
      printf 'pipx install checkov==%s --force' "$TOOL_VERSION_CHECKOV"
      ;;
    *)
      return 1
      ;;
  esac
}

main() {
  if [ "$#" -ne 1 ]; then
    echo "usage: $0 <tool>" >&2
    return 2
  fi

  local tool="$1" command want got
  want="$(tool_version "$(manifest_key "$tool")")"
  if [ -z "$want" ] || ! command="$(install_command "$tool")"; then
    echo "ERROR: $tool has no pinned version in scripts/lib/tool-versions.sh." >&2
    echo "  Add its row there before asking for it here — a tool nothing pins is one" >&2
    echo "  that has chosen a version on your behalf." >&2
    return 2
  fi

  # A tool that reports no version fails with the install command.
  got="$(installed_version "$tool" || true)"
  if [ -z "$got" ]; then
    if command -v "$tool" >/dev/null 2>&1; then
      echo "ERROR: $tool is installed but did not say which version it is; scripts/lib/tool-versions.sh pins $want. Install the pinned version:" >&2
    else
      echo "ERROR: $tool not found. Install the pinned version:" >&2
    fi
    echo "  $command" >&2
    return 1
  fi
  if [ "${got#v}" != "${want#v}" ]; then
    echo "ERROR: $tool is $got, but scripts/lib/tool-versions.sh pins $want. Install the pinned version:" >&2
    echo "  $command" >&2
    return 1
  fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
