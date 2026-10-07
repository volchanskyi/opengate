#!/usr/bin/env bash
# The one place a tool's version is written; sourced by the install scripts, the gauntlet and
# scripts/tests/tool-version-parity.test.sh, which holds every other copy equal to it.

export TOOL_RUNNER_IMAGE="ubuntu-24.04"

# Run by the gauntlet and by CI, and held on both sides: CI by the parity test, the workstation by
# scripts/lib/toolchain-parity.sh.
export TOOL_VERSION_SHELLCHECK="0.11.0"
export TOOL_VERSION_SHFMT="3.13.1"
export TOOL_VERSION_SEMGREP="1.108.0"
export TOOL_VERSION_JQ="1.7.1"
export TOOL_VERSION_YAMLLINT="1.38.0"
export TOOL_VERSION_GITLEAKS="8.21.2"
export TOOL_VERSION_ACTIONLINT="1.7.12"
export TOOL_VERSION_PMAT="3.17.0"
export TOOL_VERSION_GO_ARCH_LINT="1.19.0"
export TOOL_VERSION_CARGO_AUDIT="0.22.1"
export TOOL_VERSION_CARGO_DENY="0.19.6"
export TOOL_VERSION_CARGO_MODULES="0.26.0"
export TOOL_VERSION_GOVULNCHECK="1.8.0"
export TOOL_VERSION_OAPI_CODEGEN="2.6.0"
# The maintainer's age key was made with this age; both sides build zstd from its release source.
export TOOL_VERSION_AGE="1.3.2"
export TOOL_VERSION_ZSTD="1.5.7"
export TOOL_VERSION_HADOLINT="2.12.0"
export TOOL_VERSION_HELM="3.16.3"
export TOOL_VERSION_KUBECONFORM="0.6.7"
export TOOL_VERSION_CONFTEST="0.55.0"
export TOOL_VERSION_CHECKOV="3.3.16"
export TOOL_VERSION_TFLINT="0.64.0"
export TOOL_VERSION_TRIVY="0.70.0"
export TOOL_VERSION_STATICCHECK="0.8.1"
export TOOL_VERSION_GOSEC="2.29.0"
export TOOL_VERSION_CARGO_NEXTEST="0.9.129"
export TOOL_VERSION_CARGO_LLVM_COV="0.8.5"
# The scan action takes the CLI version; make sonar and the CI fallback take the image, which
# bundles the same CLI.
export TOOL_VERSION_SONAR_SCANNER="8.1.0.6389"
export TOOL_VERSION_SONAR_SCANNER_IMAGE="12.2.0.4256_8.1.0"

# Run by CI and by hand only.
export TOOL_VERSION_OCI_CLI="3.94.1"
export TOOL_VERSION_K6="v1.6.1"
export TOOL_VERSION_CARGO_MUTANTS="27.0.0"
export TOOL_VERSION_GREMLINS="0.6.0"
export TOOL_VERSION_CARGO_FUZZ="0.13.2"
# viewcore publishes no tagged releases, so its pin is a commit.
export TOOL_VERSION_VIEWCORE="v0.0.0-20260908162731-ac862fd6552b"

# CI asks for these as channels; scripts/lib/toolchain-parity.sh refuses a workstation behind them.
export TOOL_CHANNEL_RUST="stable"
export TOOL_CHANNEL_RUST_NIGHTLY="nightly"
export TOOL_MAJOR_NODE="24"

# tool_version NAME prints the pinned version for a manifest key, or nothing.
tool_version() {
  local key="TOOL_VERSION_$1"
  printf '%s' "${!key-}"
}
