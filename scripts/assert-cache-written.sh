#!/usr/bin/env bash
# Asserts a cache key exists by asking the cache API, since a refused save exits zero.
# Matches a key containing the fragment; needs GITHUB_REPOSITORY and a gh token with actions: read.
# Usage: scripts/assert-cache-written.sh <key-or-fragment>

set -euo pipefail

FRAGMENT="${1:-}"
if [ -z "$FRAGMENT" ]; then
  echo "usage: scripts/assert-cache-written.sh <key-or-fragment>" >&2
  exit 2
fi

REPO="${GITHUB_REPOSITORY:-}"
if [ -z "$REPO" ]; then
  echo "::error::assert-cache-written: GITHUB_REPOSITORY is unset, so there is no cache list to ask for." >&2
  exit 2
fi

# Read to the end before narrowing: a pipe closed early takes gh down with it,
# and pipefail would then read a present key as an absent one.
if ! KEYS="$(gh api --paginate "/repos/$REPO/actions/caches" --jq '.actions_caches[].key')"; then
  echo "::error::assert-cache-written: could not read the cache list for $REPO." \
    "A guard that cannot ask does not answer yes." >&2
  exit 1
fi

if grep -qF -- "$FRAGMENT" <<<"$KEYS"; then
  echo "assert-cache-written: a cache key carries '$FRAGMENT'"
  exit 0
fi

echo "::error::assert-cache-written: no cache key carries '$FRAGMENT'." \
  "The save was refused and the step that made it reported success." >&2
exit 1
