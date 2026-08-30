#!/usr/bin/env bash
# Assert that a mutation shard's tool wrote the report it was asked for.
#
# A shard's tool step swallows the tool's exit code on purpose: a surviving
# mutant is not a build failure, so the step cannot use the exit status to say
# whether any work happened. That leaves the report as the only evidence, and
# the step that ran the tool is the one place where the tool's own output is
# still on screen next to the answer. Read it back here rather than leaving the
# absence to be discovered by the artifact upload, whose message names the
# artifact and not the baseline suite that refused.
#
# Usage: scripts/assert-mutation-report.sh <tool> <report-path>

set -euo pipefail

TOOL="${1:-}"
REPORT="${2:-}"
if [ -z "$TOOL" ] || [ -z "$REPORT" ]; then
  echo "usage: scripts/assert-mutation-report.sh <tool> <report-path>" >&2
  exit 2
fi

if [ ! -s "$REPORT" ]; then
  echo "::error::assert-mutation-report: ${TOOL} wrote no report at ${REPORT}, so this shard measured nothing." \
    "${TOOL} reports a surviving mutant with a non-zero exit too, which is why the step above does not read its status;" \
    "the usual cause of an empty report is the baseline test suite failing before mutation starts, named in the ${TOOL} output above." >&2
  exit 1
fi

echo "assert-mutation-report: ${TOOL} wrote ${REPORT}"
