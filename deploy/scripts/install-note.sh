#!/usr/bin/env bash
# Prints the install note the agent installer writes, from an enrolment probe reply on stdin.
# Usage: install-note.sh NOTE_URL TOKEN < probe-reply.json
# Exit codes:
#   0  the note was printed
#   1  the reply carried no master key
#   2  an argument is missing

set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: install-note.sh NOTE_URL TOKEN < probe-reply.json" >&2
  exit 2
fi

jq -ce --arg url "$1" --arg token "$2" \
  'select((.ca_pem // "") != "") | {token: $token, url: $url, ca_pem: .ca_pem, fresh: false}' \
  || {
    echo "the enrolment probe returned no master key" >&2
    exit 1
  }
