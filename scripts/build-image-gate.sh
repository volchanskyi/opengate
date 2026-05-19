#!/usr/bin/env bash
# Decides whether `.github/workflows/build-image.yml` should run its full
# multi-arch build for a given main-branch commit. Prints two key=value
# lines suitable for appending to $GITHUB_OUTPUT:
#
#     image_changed=true|false
#     prev_sha=<full-sha|empty>
#
# Returns 0 on a clean decision, non-zero on usage / git errors. The
# workflow skips `build-and-push` and instead `crane copy`s :latest onto
# the new sha- tag when the diff against the SHA that built :latest
# contains no entries under server/**, web/**, or Dockerfile. CD's
# digest-equality gate then short-circuits a no-op redeploy. See:
#   - .claude/plans/path-gate-build-image.md
#   - .claude/plans/archive/path-gate-agent-release.md (precedent)
#
# Inputs (env vars):
#   IMAGE       Required. Full image ref minus tag, e.g.
#               "ghcr.io/volchanskyi/opengate-server".
#   HEAD_SHA    Required. Commit being built (workflow_run.head_sha,
#               github.sha, etc.).
#   CRANE       Optional. Path to the crane binary. Defaults to `crane`.
#
# Idempotent: pure function of git state + a single registry read. Manual
# workflow_dispatch re-runs produce the same decision.
#
# Tested by scripts/tests/build-image-gate.test.sh.

set -euo pipefail

IMAGE="${IMAGE:-}"
HEAD_SHA="${HEAD_SHA:-}"
CRANE="${CRANE:-crane}"

if [ -z "$IMAGE" ] || [ -z "$HEAD_SHA" ]; then
  echo "usage: IMAGE=<ref> HEAD_SHA=<sha> $(basename "$0")" >&2
  exit 2
fi

# Resolve the commit SHA used to build :latest via the OCI image config.
# docker/metadata-action@v6 stamps `org.opencontainers.image.revision`
# automatically — we don't need any extra workflow plumbing.
#
# Fail-open: if the registry read fails (no :latest yet, transient blip
# while the gate runs but `set -e` not yet tripping the workflow) OR the
# label is absent (image pushed by hand without metadata), force a
# rebuild rather than silently skipping. Safer than skipping a real
# image change against a phantom baseline.
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

# Force-push / rebase guard: the label can point at a commit no longer
# reachable in this repo. Treat unknown commit as "rebuild" rather than
# diffing against a phantom baseline (which silently reports nothing).
if ! git rev-parse --verify --quiet "${PREV_SHA}^{commit}" >/dev/null 2>&1; then
  echo "image_changed=true"
  echo "prev_sha ${PREV_SHA} not reachable in this repo — building." >&2
  exit 0
fi

# Image-input pathspec — keep in sync with the Dockerfile's COPY layers.
# - server/** and web/** cover the build stages' inputs.
# - top-level Dockerfile rebuilds the final stage on its own changes.
# - agent/**, deploy/**, docs/**, .github/** are intentionally NOT image
#   inputs and must not appear here.
if [ -n "$(git diff --name-only "$PREV_SHA" "$HEAD_SHA" -- \
            'server/**' 'web/**' 'Dockerfile')" ]; then
  echo "image_changed=true"
  echo "image inputs changed between ${PREV_SHA} and ${HEAD_SHA} — building." >&2
else
  echo "image_changed=false"
  echo "no image inputs changed between ${PREV_SHA} and ${HEAD_SHA} — skipping build." >&2
fi
