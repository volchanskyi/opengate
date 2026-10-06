#!/usr/bin/env bash
# Decides whether the agent release build runs for a v* tag, from the agent/ diff against the
# previous v* tag. Prints agent_changed=true|false and prev_tag=<vX.Y.Z|empty> for $GITHUB_OUTPUT.

set -euo pipefail

TAG="${1:-}"

if [ -z "$TAG" ]; then
  echo "usage: $(basename "$0") <vX.Y.Z>" >&2
  exit 2
fi

if ! git rev-parse "$TAG" >/dev/null 2>&1; then
  echo "error: tag '$TAG' not found in this repository" >&2
  exit 3
fi

# Describing from the tag's parent finds the previous v* tag; a first release has none.
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

# Adds, modifications and deletions under agent/ all count as a change.
if [ -n "$(git diff --name-only "$PREV" "$TAG" -- 'agent/**')" ]; then
  echo "agent_changed=true"
  echo "agent/ changed between ${PREV} and ${TAG} — building." >&2
else
  echo "agent_changed=false"
  echo "no agent/ changes between ${PREV} and ${TAG} — skipping build." >&2
fi
