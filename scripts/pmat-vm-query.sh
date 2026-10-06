#!/usr/bin/env bash
# Prints the latest PMAT value of the newest earlier date from VictoriaMetrics, or nothing.
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
#
# Usage: $0 <repo_score|below_bplus>

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

# The number of dates searched back for the previous night.
LOOKBACK_DATES=7

vm_nightly_window "$metric" 'env="ci"' "$LOOKBACK_DATES" | awk -F'\t' 'NR == 1 { print $4 }'
