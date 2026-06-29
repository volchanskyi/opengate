#!/usr/bin/env bash
# post-push-clean-caches.sh — best-effort reclaim of large, regenerable LOCAL
# caches after a successful push. Invoked by .claude/hooks/git-post-commit.sh
# right after the auto-push succeeds (git has no native post-push hook), and
# safe to run by hand: `.claude/hooks/post-push-clean-caches.sh`.
#
# WHY: the dev machine accumulates regenerable caches that pressure the WSL disk
# — the Rust agent/target build cache grows to ~12G, and testcontainers /
# `make e2e` runs leave orphaned anonymous Postgres data volumes behind.
# Production runs on OKE, so nothing local here is precious. Everything cleared
# is a rebuild, never a re-download:
#   - cargo clean (agent workspace)  — the big one
#   - go clean -cache                — host Go build cache (usually negligible)
#   - docker volume prune -f         — drops UNUSED anonymous volumes only
#                                      (Docker 23+; named volumes are untouched)
# It deliberately never clears the cargo registry or the Go module cache —
# those force re-downloads, not just rebuilds.
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

# 3. Orphaned anonymous Docker volumes — the throwaway test databases. Only
#    removes volumes not attached to any container; named volumes are spared.
if command -v docker >/dev/null 2>&1; then
  docker volume prune -f >/dev/null 2>&1 \
    && echo "cache-clean: docker volume prune -f (orphaned test volumes)" \
    || echo "cache-clean: docker volume prune failed (ignored)"
fi

exit 0
