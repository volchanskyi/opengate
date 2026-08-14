#!/usr/bin/env bash
# post-push-clean-caches.sh — best-effort reclaim of large, regenerable LOCAL
# caches after a successful push. Invoked by .claude/hooks/git-post-commit.sh
# right after the auto-push succeeds (git has no native post-push hook), and
# safe to run by hand: `.claude/hooks/post-push-clean-caches.sh`.
#
# WHY: the dev machine accumulates regenerable caches that pressure the WSL disk
# — the Rust agent/target build cache reaches tens of GB, every `make e2e` /
# gauntlet run does `up --build` and lays down a fresh set of Docker build-cache
# records, and testcontainers runs leave orphaned anonymous Postgres data
# volumes behind. Production runs on OKE, so nothing local here is precious.
# Everything cleared is a rebuild, never a re-download:
#   - cargo clean (agent workspace)  — tens of GB
#   - go clean -cache                — host Go build cache (usually negligible)
#   - docker volume prune -f         — drops UNUSED anonymous volumes only
#                                      (Docker 23+; named volumes are untouched)
#   - docker builder prune -af       — the whole build cache; co-equal with cargo
#                                      as the largest consumer
#   - docker image prune -f          — DANGLING images only
# It deliberately never clears the cargo registry, the Go module cache, or
# tagged images (`image prune -a` / `system prune -a`) — those force
# re-downloads, not just rebuilds.
#
# Best-effort by construction: never under CI, skippable via an opt-out env var,
# every step guarded on tool presence, and a missing tool or a failed prune is a
# non-fatal no-op (exit 0 regardless). The push already happened; nothing here
# can undo it.
set -uo pipefail

# Opt-out for engineers/agents who want to keep their local build cache.
if [ -n "${OPENGATE_SKIP_CACHE_CLEAN:-}" ]; then
  exit 0
fi

# Never run inside CI or other non-interactive automation — those runners need
# their caches and are torn down anyway.
if [ -n "${CI:-}" ] || [ -n "${GITHUB_ACTIONS:-}" ]; then
  exit 0
fi

# Resolve the repo root: explicit first arg (used by the post-commit caller) or
# the enclosing work tree. Bail quietly if neither is available.
root="${1:-$(git rev-parse --show-toplevel 2>/dev/null || true)}"
[ -n "$root" ] || exit 0

# 1. Rust build cache — the dominant consumer. Scoped to the agent workspace.
if command -v cargo >/dev/null 2>&1 && [ -f "$root/agent/Cargo.toml" ]; then
  (cd "$root/agent" && cargo clean) \
    && echo "cache-clean: cargo clean (agent/target)" \
    || echo "cache-clean: cargo clean failed (ignored)"
fi

# 2. Host Go build cache — usually tiny (Go builds run in containers), but free.
if command -v go >/dev/null 2>&1; then
  go clean -cache \
    && echo "cache-clean: go clean -cache" \
    || echo "cache-clean: go clean -cache failed (ignored)"
fi

# 3. Docker. Three separate reclaims — `volume prune` alone leaves the build
#    cache untouched, and the build cache is where the bulk of the disk goes.
if command -v docker >/dev/null 2>&1; then
  #  a. Orphaned anonymous volumes — the throwaway test databases. Only removes
  #     volumes not attached to any container; named volumes are spared.
  docker volume prune -f >/dev/null 2>&1 \
    && echo "cache-clean: docker volume prune -f (orphaned test volumes)" \
    || echo "cache-clean: docker volume prune failed (ignored)"

  #  b. The whole builder cache. `-a` covers cache still referenced by a tagged
  #     image, which is the majority of it after repeated `up --build` runs;
  #     every entry is reproducible by rebuilding.
  docker builder prune -af >/dev/null 2>&1 \
    && echo "cache-clean: docker builder prune -af (build cache)" \
    || echo "cache-clean: docker builder prune failed (ignored)"

  #  c. Dangling images left behind by rebuilds. Bare `-f` keeps every TAGGED
  #     image, so registry-pulled base images are never re-downloaded.
  docker image prune -f >/dev/null 2>&1 \
    && echo "cache-clean: docker image prune -f (dangling images)" \
    || echo "cache-clean: docker image prune failed (ignored)"
fi

exit 0
