#!/usr/bin/env bash
# Scans the Go module once against a vulnerability database fetched first, with retries on
# the fetch alone; a failed scan is a finding or a crash and is never retried.
#
# Environment:
#   GOVULN_DB_URL   the database archive (default https://vuln.go.dev/vulndb.zip)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DB_URL="${GOVULN_DB_URL:-https://vuln.go.dev/vulndb.zip}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

curl --fail --silent --show-error --location \
  --retry 5 --retry-all-errors --retry-delay 5 \
  --connect-timeout 20 --max-time 300 \
  -o "$WORK/vulndb.zip" "$DB_URL"
unzip -q "$WORK/vulndb.zip" -d "$WORK/db"
[ -f "$WORK/db/index/db.json" ] || {
  echo "::error::the vulnerability database from $DB_URL has no index/db.json, so it is not a database govulncheck can read." >&2
  exit 1
}

cd "$ROOT/server"
govulncheck -db "file://$WORK/db" ./...
