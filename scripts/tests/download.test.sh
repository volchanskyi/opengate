#!/usr/bin/env bash
# Holds the install scripts' downloads to retrying a refusal the host answers next time.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LIB="$REPO_ROOT/scripts/lib/download.sh"
INSTALLERS=(install-shell-tools.sh install-release-tools.sh install-dump-tools.sh)

PASS=0
FAIL=0
FAILURES=()

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}

fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}

[ -f "$LIB" ] || {
  echo "FAIL: $LIB missing" >&2
  exit 1
}

WORK="$(mktemp -d)"
SERVER_PID=""
cleanup() {
  [ -z "$SERVER_PID" ] || kill "$SERVER_PID" 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

# GitHub's release host answered 403 to one download and 200 minutes later; this host answers
# /flaky with 403 once and then the file, /gone with 404 always, and counts every request.
cat >"$WORK/host.py" <<'HOST'
import http.server
import sys

work = sys.argv[1]
seen = {}


class Host(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        seen[self.path] = seen.get(self.path, 0) + 1
        with open(f"{work}/count{self.path.replace('/', '-')}", "w", encoding="utf-8") as out:
            out.write(str(seen[self.path]))
        if self.path == "/flaky" and seen[self.path] > 1:
            body = b"release bytes\n"
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        self.send_response(403 if self.path == "/flaky" else 404)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def log_message(self, *_):
        pass


server = http.server.HTTPServer(("127.0.0.1", 0), Host)
with open(f"{work}/port", "w", encoding="utf-8") as out:
    out.write(str(server.server_address[1]))
server.serve_forever()
HOST
python3 "$WORK/host.py" "$WORK" &
SERVER_PID=$!
for _ in $(seq 1 50); do
  [ -s "$WORK/port" ] && break
  sleep 0.1
done
PORT="$(cat "$WORK/port")"

if bash -c 'set -euo pipefail; . "$1"; download "$2" "$3"' _ "$LIB" \
  "http://127.0.0.1:$PORT/flaky" "$WORK/flaky.out" 2>"$WORK/flaky.err" \
  && [ "$(cat "$WORK/flaky.out")" = "release bytes" ]; then
  pass "a download refused once is fetched on the next attempt"
else
  fail "a download refused once was not fetched: $(cat "$WORK/flaky.err")"
fi
if [ "$(cat "$WORK/count-flaky")" = 2 ]; then
  pass "the refused download was asked twice"
else
  fail "the refused download was asked $(cat "$WORK/count-flaky") times"
fi

if bash -c 'set -euo pipefail; . "$1"; download "$2" "$3"' _ "$LIB" \
  "http://127.0.0.1:$PORT/gone" "$WORK/gone.out" 2>/dev/null; then
  fail "a download the host never has reported success"
else
  pass "a download the host never has fails"
fi
if [ "$(cat "$WORK/count-gone")" = 4 ]; then
  pass "a download the host never has is asked four times, then given up"
else
  fail "a download the host never has was asked $(cat "$WORK/count-gone") times"
fi

for installer in "${INSTALLERS[@]}"; do
  body="$(cat "$REPO_ROOT/scripts/$installer")"
  if grep -qE '^[[:space:]]*curl[[:space:]]' <<<"$body"; then
    fail "$installer calls curl itself"
  elif grep -qF 'lib/download.sh' <<<"$body" && grep -qE '^[[:space:]]*download[[:space:]]' <<<"$body"; then
    pass "$installer downloads through the shared helper"
  else
    fail "$installer does not download through the shared helper"
  fi
done

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
