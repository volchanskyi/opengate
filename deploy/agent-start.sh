#!/usr/bin/env bash
# Starts the agent in a container, first copying a mounted install note into the data folder as
# the installer writes it: owner-only, and only before the machine has an identity.

set -euo pipefail

NOTE=/run/opengate/install-note.json
DATA_DIR="${OPENGATE_DATA_DIR:-/var/lib/mesh-agent}"

if [[ -f "$NOTE" && ! -f "$DATA_DIR/device_id.txt" && ! -f "$DATA_DIR/install-note.json" ]]; then
  cp "$NOTE" "$DATA_DIR/.install-note.tmp"
  chmod 0600 "$DATA_DIR/.install-note.tmp"
  mv -f "$DATA_DIR/.install-note.tmp" "$DATA_DIR/install-note.json"
fi

exec /usr/local/bin/mesh-agent "$@"
