#!/usr/bin/env bash
# Read the previous night's PMAT value from VictoriaMetrics so the nightly
# workflow can compute day-over-day regressions before publishing the current
# sample: the latest reading of the newest date before tonight's, whatever code
# that night ran. Thin adapter over the shared read-back library
# scripts/lib/vm-query.sh.
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
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

# How far back the previous night is looked for.
LOOKBACK_DATES=7

vm_nightly_window "$metric" 'env="ci"' "$LOOKBACK_DATES" | awk -F'\t' 'NR == 1 { print $4 }'
