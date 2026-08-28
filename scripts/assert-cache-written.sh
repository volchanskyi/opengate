#!/usr/bin/env bash
# Assert that a cache key exists, by asking the cache API for it.
#
# A refused cache save prints a warning and exits zero, so the step that made
# it is reported as a success and the entry it never wrote is missed until
# somebody goes looking. Reading the key back is the only evidence that stands.
#
# Usage: scripts/assert-cache-written.sh <key-or-fragment>
#
# Matches any key containing the fragment, because an action that computes its
# own key does not tell the caller what it settled on; the fragment names the
# part the caller chose. Needs GITHUB_REPOSITORY and a gh token carrying
# `actions: read`.

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

if printf '%s\n' "$KEYS" | grep -qF -- "$FRAGMENT"; then
  echo "assert-cache-written: a cache key carries '$FRAGMENT'"
  exit 0
fi

echo "::error::assert-cache-written: no cache key carries '$FRAGMENT'." \
  "The save was refused and the step that made it reported success." >&2
exit 1
