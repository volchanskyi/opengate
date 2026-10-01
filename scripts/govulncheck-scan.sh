#!/usr/bin/env bash
# govulncheck-scan.sh — one scan of the Go module against the vulnerability
# database, the same way on the workstation and in CI.
#
# The database is fetched first, as one file, with retries on the fetch alone.
# The scan then runs once against that local copy. A scan that fails is a
# finding or a crash, and neither is something to retry: a loop retrying the
# whole scan let a scanner that crashed under the module's Go pass on whichever
# attempt happened not to, and the network fetch it existed for is the only
# part that is ever transient.
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
