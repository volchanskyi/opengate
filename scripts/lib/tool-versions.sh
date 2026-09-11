#!/usr/bin/env bash
# tool-versions.sh — the one place a tool's version is written down.
#
# Sourced by the install scripts, by the gauntlet's prerequisite phase, and by
# scripts/tests/tool-version-parity.test.sh. NOT executable on its own — this is
# a table of shell variables.
#
# Why it exists: a version that is written down twice is a version that will
# differ. Two of them already had.
#
#   * jq. Nobody had ever chosen one. The workstation carried whatever the
#     distribution shipped (1.6) and CI carried whatever the runner image did
#     (1.7.1), and the two render numbers differently: 1.6 canonicalises the
#     17.700 a drill wrote to 17.7 while 1.7 keeps the literal. A test asserting
#     on that reading passed on the workstation and failed on every CI run, and
#     the diff said nothing about jq because no file in the repository mentioned
#     it.
#
#   * Go. server/go.mod's toolchain directive was bumped to clear a fresh stdlib
#     advisory; the gauntlet honoured it through GOTOOLCHAIN=auto and went
#     green, while CI's Security Audit job pinned its own go-version and went on
#     scanning the vulnerable patch.
#
# Both have the same shape: one fact, two homes, nothing reading both. So the
# fact lives here once, the installs read it, and
# scripts/tests/tool-version-parity.test.sh holds every other copy — every
# workflow pin, every install line — equal to it.
#
# Adding a tool: add its row, install it from this variable, and the parity test
# will require the workflows to agree. A tool nothing pins is a tool that has
# chosen a version on your behalf.

# --- the runner image --------------------------------------------------------
#
# The layer under every row below. `ubuntu-latest` is a moving tag that decides
# jq, python3, curl, git and coreutils for all of CI without anybody choosing
# them, and moves to the next LTS on GitHub's schedule rather than ours. Naming
# the image makes every one of those a decision that lands in a diff.
export TOOL_RUNNER_IMAGE="ubuntu-24.04"

# --- run by the workstation AND by CI ----------------------------------------
#
# Drift in this half is the expensive kind: the gauntlet and the pipeline run the
# same check against different tools, so the gate is green here and red there
# with nothing in the change to explain either.
export TOOL_VERSION_SHELLCHECK="0.11.0"
export TOOL_VERSION_SHFMT="3.13.1"
export TOOL_VERSION_SEMGREP="1.108.0"
export TOOL_VERSION_JQ="1.7.1"
export TOOL_VERSION_YAMLLINT="1.38.0"
export TOOL_VERSION_GITLEAKS="8.21.2"
export TOOL_VERSION_ACTIONLINT="1.7.12"
export TOOL_VERSION_PMAT="3.17.0"
export TOOL_VERSION_GO_ARCH_LINT="1.15.0"
export TOOL_VERSION_CARGO_AUDIT="0.22.1"
export TOOL_VERSION_CARGO_DENY="0.19.6"
export TOOL_VERSION_CARGO_MODULES="0.26.0"
export TOOL_VERSION_GOVULNCHECK="1.1.4"
export TOOL_VERSION_OAPI_CODEGEN="2.6.0"

# --- run by CI alone ---------------------------------------------------------
#
# These cannot produce the skew above, because the workstation never runs them.
# They are pinned for the other reason: a tool that resolves itself at run time
# is a tool whose output can change on a night nobody touched the repository,
# and the failure surfaces as a finding in a job whose subject is something else.
export TOOL_VERSION_HADOLINT="2.12.0"
export TOOL_VERSION_HELM="3.16.3"
export TOOL_VERSION_KUBECONFORM="0.6.7"
export TOOL_VERSION_CONFTEST="0.55.0"
export TOOL_VERSION_CHECKOV="3.3.16"
export TOOL_VERSION_TFLINT="0.64.0"
export TOOL_VERSION_TRIVY="0.70.0"
export TOOL_VERSION_K6="v1.6.1"
export TOOL_VERSION_CARGO_MUTANTS="27.0.0"
export TOOL_VERSION_GREMLINS="0.6.0"
# viewcore reads a core dump as a Go heap, which is how the endurance run follows
# what holds a leaked object rather than where it was allocated. It has no
# tagged releases, so the pin is the commit — which is the same statement every
# other row makes, spelled the way this module publishes versions.
export TOOL_VERSION_VIEWCORE="v0.0.0-20260908162731-ac862fd6552b"

# --- deliberately floating ---------------------------------------------------
#
# Three toolchains float on purpose, because CI asks for a channel rather than a
# release and a pinned workstation would be the thing out of step. They are held
# level a different way: scripts/lib/toolchain-parity.sh refuses to run the
# gauntlet on a machine whose rustup, node or go has fallen behind what CI would
# resolve today. Go is the exception inside the exception — server/go.mod's
# toolchain directive pins it exactly, and
# scripts/tests/ci-govulncheck-go-version.test.sh holds every workflow's
# go-version equal to that.
export TOOL_CHANNEL_RUST="stable"
export TOOL_CHANNEL_RUST_NIGHTLY="nightly"
export TOOL_MAJOR_NODE="24"

# tool_version NAME — the pinned version for a manifest key, or empty.
# Keeps callers from spelling a variable name that does not exist and reading
# the empty string as agreement.
tool_version() {
  local key="TOOL_VERSION_$1"
  printf '%s' "${!key-}"
}
