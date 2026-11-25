#!/usr/bin/env bash
# Downloads a release file, retrying every failed attempt: a host refuses once and answers next.
#
# Usage:  . scripts/lib/download.sh   then   download <url> <destination>

# download fetches url into destination; three retries five seconds apart follow any error.
download() {
  curl --fail --silent --show-error --location --retry 3 --retry-all-errors --retry-delay 5 \
    --connect-timeout 10 --max-time 300 -o "$2" "$1"
}
