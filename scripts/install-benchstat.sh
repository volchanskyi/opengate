#!/usr/bin/env bash
# Installs the pinned benchstat, which the benchmark nightly's re-run judges a slowdown with.
#
# Usage: install-benchstat.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tool-versions.sh
. "$HERE/lib/tool-versions.sh"

go install "golang.org/x/perf/cmd/benchstat@${TOOL_VERSION_BENCHSTAT}"
echo "benchstat $TOOL_VERSION_BENCHSTAT at $(go env GOPATH)/bin/benchstat"
