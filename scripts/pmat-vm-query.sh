#!/usr/bin/env bash
# Read the most recent PMAT value from VictoriaMetrics so the nightly workflow
# can compute day-over-day regressions before publishing the current sample.
# Thin adapter over the shared read-back library scripts/lib/vm-query.sh.
#
# Usage: $0 <repo_score|below_bplus>
# Prints the scalar value, or nothing when history is absent/unavailable.

set -uo pipefail

FIELD="${1:?Usage: $0 <repo_score|below_bplus>}"
case "$FIELD" in
  repo_score) metric="pmat_repo_score" ;;
  below_bplus) metric="pmat_below_bplus" ;;
  *)
    echo "unknown field: $FIELD (want repo_score|below_bplus)" >&2
    exit 2
    ;;
esac

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/vm-query.sh
. "$SCRIPT_DIR/lib/vm-query.sh"

vm_query_latest "$metric" 'env="ci"'
