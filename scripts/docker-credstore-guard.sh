#!/usr/bin/env bash
# docker-credstore-guard.sh — print a DOCKER_CONFIG dir that is safe for
# anonymous pulls of public images, working around a broken credential helper.
#
# A ~/.docker/config.json "credsStore" that points at a helper which cannot
# execute makes `docker build`/`pull` fail even for PUBLIC images, because
# docker invokes the helper before falling back to anonymous access. The classic
# offender is WSL's docker-credential-desktop.exe, which fork/exec-fails with
# "exec format error" when Docker Desktop's WSL integration is not wired up.
#
# This guard probes the configured helper. If it is missing or non-functional,
# it writes a sanitized copy of config.json (credsStore + credHelpers stripped,
# every other key preserved) into a cache dir and prints that dir on stdout, for
# use as DOCKER_CONFIG. If the helper works, or none is configured, it prints the
# existing config dir unchanged. It always exits 0 — a guard must never block.
#
# Usage:
#   DOCKER_CONFIG="$(scripts/docker-credstore-guard.sh)" docker compose ...
set -euo pipefail

src_dir="${DOCKER_CONFIG:-$HOME/.docker}"
cfg="$src_dir/config.json"

emit_src() {
  printf '%s\n' "$src_dir"
  exit 0
}

# No config or no credsStore → nothing to sanitize.
[ -f "$cfg" ] || emit_src
store="$(sed -n 's/.*"credsStore"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$cfg" | head -1)"
[ -n "$store" ] || emit_src

# Probe the helper: it must be resolvable AND run natively. `list` is a
# read-only, no-argument verb every standard helper implements; a broken
# Windows .exe under WSL fails here with "exec format error".
helper="docker-credential-$store"
if command -v "$helper" >/dev/null 2>&1 && printf '' | "$helper" list >/dev/null 2>&1; then
  emit_src
fi

# Helper broken: emit a sanitized config dir (credsStore + credHelpers dropped,
# everything else — auths, proxies, plugins — preserved). Public images then
# pull anonymously; private auths in config.json still work.
out_dir="${XDG_CACHE_HOME:-$HOME/.cache}/opengate/docker-clean"
mkdir -p "$out_dir"
python3 - "$cfg" "$out_dir/config.json" <<'PY'
import json, sys
src, dst = sys.argv[1], sys.argv[2]
try:
    cfg = json.load(open(src))
except Exception:
    cfg = {}
cfg.pop("credsStore", None)
cfg.pop("credHelpers", None)
json.dump(cfg, open(dst, "w"))
PY
printf '%s\n' "$out_dir"
