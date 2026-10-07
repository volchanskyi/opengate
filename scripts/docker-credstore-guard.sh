#!/usr/bin/env bash
# Prints a DOCKER_CONFIG dir safe for anonymous pulls: the existing one when its credential
# helper works, else a sanitized copy without credsStore and credHelpers.
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

# The helper must resolve and run natively; `list` is the read-only verb every helper has.
helper="docker-credential-$store"
if command -v "$helper" >/dev/null 2>&1 && printf '' | "$helper" list >/dev/null 2>&1; then
  emit_src
fi

# A broken helper gets a sanitized copy that keeps auths, proxies and plugins.
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
if [ -d "$src_dir/cli-plugins" ]; then
  mkdir -p "$out_dir/cli-plugins"
  for plugin in "$src_dir"/cli-plugins/*; do
    [ -x "$plugin" ] || continue
    ln -sf "$(readlink -f "$plugin")" "$out_dir/cli-plugins/$(basename "$plugin")"
  done
fi
printf '%s\n' "$out_dir"
