#!/usr/bin/env bash
# Prints image_changed=true|false and prev_sha=<sha|empty>, ready for $GITHUB_OUTPUT.
# The result is a function of git state and one registry read.
#
# Environment:
#   IMAGE     full image ref minus the tag
#   HEAD_SHA  commit being built
#   CRANE     path to the crane binary, default `crane`

set -euo pipefail

IMAGE="${IMAGE:-}"
HEAD_SHA="${HEAD_SHA:-}"
CRANE="${CRANE:-crane}"

if [ -z "$IMAGE" ] || [ -z "$HEAD_SHA" ]; then
  echo "usage: IMAGE=<ref> HEAD_SHA=<sha> $(basename "$0")" >&2
  exit 2
fi

# The commit that built :latest is the OCI revision label on its config.
# A failed registry read or a missing label forces a rebuild.
PREV_SHA=""
if config="$("$CRANE" config "${IMAGE}:latest" 2>/dev/null)"; then
  PREV_SHA="$(printf '%s' "$config" \
    | jq -r '.config.Labels["org.opencontainers.image.revision"] // empty')"
fi

echo "prev_sha=${PREV_SHA}"

if [ -z "$PREV_SHA" ]; then
  echo "image_changed=true"
  echo "no :latest revision label resolvable — building." >&2
  exit 0
fi

# A label naming a commit missing from this repo forces a rebuild; a diff against it is empty.
if ! git rev-parse --verify --quiet "${PREV_SHA}^{commit}" >/dev/null 2>&1; then
  echo "image_changed=true"
  echo "prev_sha ${PREV_SHA} not reachable in this repo — building." >&2
  exit 0
fi

# The pathspec lists the Dockerfile's COPY inputs: server/**, web/** and the Dockerfile itself.
if [ -n "$(git diff --name-only "$PREV_SHA" "$HEAD_SHA" -- \
  'server/**' 'web/**' 'Dockerfile')" ]; then
  echo "image_changed=true"
  echo "image inputs changed between ${PREV_SHA} and ${HEAD_SHA} — building." >&2
else
  echo "image_changed=false"
  echo "no image inputs changed between ${PREV_SHA} and ${HEAD_SHA} — skipping build." >&2
fi
