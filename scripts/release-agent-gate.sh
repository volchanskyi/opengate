#!/usr/bin/env bash
# Decides whether `.github/workflows/release-agent.yml` should run its
# matrix build for a given v* tag. Prints two key=value lines suitable for
# appending to $GITHUB_OUTPUT:
#
#     agent_changed=true|false
#     prev_tag=<vX.Y.Z|empty>
#
# Returns 0 on a clean decision, non-zero on usage / git errors. The
# workflow uses an absent agent/** diff against the previous v* tag as the
# signal to skip the build (saves ~15-20 min CI wall-clock for the 86% of
# releases that don't touch agent/). See:
#   - ADR-005 (agent auto-update)
#   - .claude/plans/path-gate-agent-release.md
#
# Idempotent: pure function of git state. Manual workflow_dispatch re-runs
# produce the same decision as auto-dispatch from `auto-tag` in ci.yml.
#
# Tested by scripts/tests/release-agent-gate.test.sh.

set -euo pipefail

TAG="${1:-}"

if [ -z "$TAG" ]; then
  echo "usage: $(basename "$0") <vX.Y.Z>" >&2
  exit 2
fi

# Validate the tag exists in this repo; refuse to silently default.
if ! git rev-parse "$TAG" >/dev/null 2>&1; then
  echo "error: tag '$TAG' not found in this repository" >&2
  exit 3
fi

# Previous v* tag reachable from the parent of this tag's commit. `^` skips
# the tagged commit itself so `describe` returns the prior v*, never the
# current one. First-ever release path: no parent → empty PREV → force build.
PREV=""
if PARENT="$(git rev-parse --verify "${TAG}^" 2>/dev/null)"; then
  PREV="$(git describe --tags --abbrev=0 --match 'v*' "$PARENT" 2>/dev/null || true)"
fi

echo "prev_tag=${PREV}"

if [ -z "$PREV" ]; then
  echo "agent_changed=true"
  echo "no previous v* tag — first release, building." >&2
  exit 0
fi

# `-- 'agent/**'` matches anything under agent/, including deep subdirs.
# `--diff-filter` left at default so adds/modifies/deletes all count.
if [ -n "$(git diff --name-only "$PREV" "$TAG" -- 'agent/**')" ]; then
  echo "agent_changed=true"
  echo "agent/ changed between ${PREV} and ${TAG} — building." >&2
else
  echo "agent_changed=false"
  echo "no agent/ changes between ${PREV} and ${TAG} — skipping build." >&2
fi
