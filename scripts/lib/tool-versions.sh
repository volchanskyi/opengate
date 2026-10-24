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
export TOOL_VERSION_GITLEAKS="8.21.4"
export TOOL_VERSION_ACTIONLINT="1.7.12"
export TOOL_VERSION_PMAT="3.17.0"
export TOOL_VERSION_GO_ARCH_LINT="1.19.0"
export TOOL_VERSION_CARGO_AUDIT="0.22.2"
export TOOL_VERSION_CARGO_DENY="0.19.6"
export TOOL_VERSION_CARGO_MODULES="0.26.0"
export TOOL_VERSION_GOVULNCHECK="1.8.0"
export TOOL_VERSION_OAPI_CODEGEN="2.7.2"
# The maintainer's age key was made with this age; both sides build zstd from its release source.
export TOOL_VERSION_AGE="1.3.2"
export TOOL_VERSION_ZSTD="1.5.7"
export TOOL_VERSION_HADOLINT="2.12.0"
export TOOL_VERSION_HELM="3.20.2"
# The ingress controller's chart, installed by a production deploy and read back nightly.
export TOOL_VERSION_INGRESS_NGINX_CHART="4.15.1"
# The certificate issuer's chart, installed by the cluster job.
export TOOL_VERSION_CERT_MANAGER_CHART="v1.20.4"
export TOOL_VERSION_KUBECONFORM="0.6.7"
export TOOL_VERSION_CONFTEST="0.55.0"
export TOOL_VERSION_CHECKOV="3.3.16"
export TOOL_VERSION_TFLINT="0.64.0"
export TOOL_VERSION_TRIVY="0.72.0"
export TOOL_VERSION_STATICCHECK="0.8.2"
# A commit: gosec's newest release reads only the export data Go 1.27.1 and older write.
export TOOL_VERSION_GOSEC="2.29.1-0.20261012130337-71577268350e"
export TOOL_VERSION_CARGO_NEXTEST="0.9.149"
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
# benchstat publishes no tagged releases, so its pin is a commit.
export TOOL_VERSION_BENCHSTAT="v0.0.0-20261009192801-be2c69fb417e"
# The cross-build needs cross's main-branch tree, so its pin is a commit.
export TOOL_VERSION_CROSS="8c1a8aa4b661711f4b7b6ac07c2e8929ce2f7d27"
export TOOL_VERSION_LHCI="0.14.0"

# Where each pinned release and its advisories are published, read by scripts/dependency-fixes.
export TOOL_SOURCE_SHELLCHECK="git:koalaman/shellcheck@v{v}"
export TOOL_SOURCE_SHFMT="Go:mvdan.cc/sh/v3"
export TOOL_SOURCE_SEMGREP="PyPI:semgrep"
export TOOL_SOURCE_JQ="git:jqlang/jq@jq-{v}"
export TOOL_SOURCE_YAMLLINT="PyPI:yamllint"
export TOOL_SOURCE_GITLEAKS="Go:github.com/zricethezav/gitleaks/v8"
export TOOL_SOURCE_ACTIONLINT="Go:github.com/rhysd/actionlint"
export TOOL_SOURCE_PMAT="crates.io:pmat"
export TOOL_SOURCE_GO_ARCH_LINT="Go:github.com/fe3dback/go-arch-lint"
export TOOL_SOURCE_CARGO_AUDIT="crates.io:cargo-audit"
export TOOL_SOURCE_CARGO_DENY="crates.io:cargo-deny"
export TOOL_SOURCE_CARGO_MODULES="crates.io:cargo-modules"
export TOOL_SOURCE_GOVULNCHECK="Go:golang.org/x/vuln"
export TOOL_SOURCE_OAPI_CODEGEN="Go:github.com/oapi-codegen/oapi-codegen/v2"
export TOOL_SOURCE_AGE="Go:filippo.io/age"
export TOOL_SOURCE_ZSTD="git:facebook/zstd@v{v}"
export TOOL_SOURCE_HADOLINT="git:hadolint/hadolint@v{v}"
export TOOL_SOURCE_HELM="Go:helm.sh/helm/v3"
export TOOL_SOURCE_INGRESS_NGINX_CHART="chart:https://kubernetes.github.io/ingress-nginx#ingress-nginx"
export TOOL_SOURCE_CERT_MANAGER_CHART="chart:https://charts.jetstack.io#cert-manager"
export TOOL_SOURCE_KUBECONFORM="Go:github.com/yannh/kubeconform"
export TOOL_SOURCE_CONFTEST="Go:github.com/open-policy-agent/conftest"
export TOOL_SOURCE_CHECKOV="PyPI:checkov"
export TOOL_SOURCE_TFLINT="Go:github.com/terraform-linters/tflint"
export TOOL_SOURCE_TRIVY="Go:github.com/aquasecurity/trivy"
export TOOL_SOURCE_STATICCHECK="Go:honnef.co/go/tools"
export TOOL_SOURCE_GOSEC="Go:github.com/securego/gosec/v2"
export TOOL_SOURCE_CARGO_NEXTEST="crates.io:cargo-nextest"
export TOOL_SOURCE_CARGO_LLVM_COV="crates.io:cargo-llvm-cov"
export TOOL_SOURCE_SONAR_SCANNER="Maven:org.sonarsource.scanner.cli:sonar-scanner-cli"
export TOOL_SOURCE_SONAR_SCANNER_IMAGE="image:docker.io/sonarsource/sonar-scanner-cli"
export TOOL_SOURCE_OCI_CLI="PyPI:oci-cli:oci"
export TOOL_SOURCE_K6="Go:go.k6.io/k6"
export TOOL_SOURCE_CARGO_MUTANTS="crates.io:cargo-mutants"
export TOOL_SOURCE_GREMLINS="Go:github.com/go-gremlins/gremlins"
export TOOL_SOURCE_CARGO_FUZZ="crates.io:cargo-fuzz"
export TOOL_SOURCE_VIEWCORE="Go:golang.org/x/debug"
export TOOL_SOURCE_BENCHSTAT="Go:golang.org/x/perf"
export TOOL_SOURCE_CROSS="git-rev:cross-rs/cross"
export TOOL_SOURCE_LHCI="npm:@lhci/cli"

# CI asks for these as channels; scripts/lib/toolchain-parity.sh refuses a workstation behind them.
export TOOL_CHANNEL_RUST="stable"
export TOOL_CHANNEL_RUST_NIGHTLY="nightly"
export TOOL_MAJOR_NODE="24"

# tool_version NAME prints the pinned version for a manifest key, or nothing.
tool_version() {
  local key="TOOL_VERSION_$1"
  printf '%s' "${!key-}"
}
