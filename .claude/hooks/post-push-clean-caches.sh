#!/usr/bin/env bash
# Reclaims regenerable local caches; each step is best-effort and the script always exits 0.
# The cargo registry, the Go module cache and tagged images stay, since clearing them re-downloads.
set -uo pipefail

if [ -n "${OPENGATE_SKIP_CACHE_CLEAN:-}" ]; then
  exit 0
fi

if [ -n "${CI:-}" ] || [ -n "${GITHUB_ACTIONS:-}" ]; then
  exit 0
fi

root="${1:-$(git rev-parse --show-toplevel 2>/dev/null || true)}"
[ -n "$root" ] || exit 0

if command -v cargo >/dev/null 2>&1 && [ -f "$root/agent/Cargo.toml" ]; then
  (cd "$root/agent" && cargo clean) \
    && echo "cache-clean: cargo clean (agent/target)" \
    || echo "cache-clean: cargo clean failed (ignored)"
fi

if command -v go >/dev/null 2>&1; then
  go clean -cache \
    && echo "cache-clean: go clean -cache" \
    || echo "cache-clean: go clean -cache failed (ignored)"
fi

# Three Docker reclaims: `volume prune` alone leaves the build cache, where most of the disk goes.
if command -v docker >/dev/null 2>&1; then
  # Removes orphaned anonymous volumes; named volumes are spared.
  docker volume prune -f >/dev/null 2>&1 \
    && echo "cache-clean: docker volume prune -f (orphaned test volumes)" \
    || echo "cache-clean: docker volume prune failed (ignored)"

  # The `-a` flag also covers build cache still referenced by a tagged image.
  docker builder prune -af >/dev/null 2>&1 \
    && echo "cache-clean: docker builder prune -af (build cache)" \
    || echo "cache-clean: docker builder prune failed (ignored)"

  # Bare `-f` removes dangling images only and keeps every tagged image.
  docker image prune -f >/dev/null 2>&1 \
    && echo "cache-clean: docker image prune -f (dangling images)" \
    || echo "cache-clean: docker image prune failed (ignored)"
fi

exit 0
