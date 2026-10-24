#!/usr/bin/env bash
# Drives scripts/telegram-alert.sh against a stub Telegram that accepts, refuses the token,
# refuses the chat, answers 200 with ok:false, or is absent; only the first may succeed.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ALERT="$REPO_ROOT/scripts/telegram-alert.sh"

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

[ -f "$ALERT" ] || {
  echo "FAIL: $ALERT missing" >&2
  exit 1
}

command -v python3 >/dev/null 2>&1 \
  || {
    echo "python3 is required to stand up the stub Telegram" >&2
    exit 1
  }

WORK="$(mktemp -d)"
STUB_PID=""
STUB_PORT=""
# stop_stub stops the running stub, if any, and waits for its port to close.
stop_stub() {
  if [ -n "$STUB_PID" ]; then
    kill "$STUB_PID" 2>/dev/null || true
    wait "$STUB_PID" 2>/dev/null || true
  fi
  STUB_PID=""
}
# shellcheck disable=SC2329 # invoked by the EXIT trap below
cleanup() {
  stop_stub
  rm -rf "$WORK"
}
trap cleanup EXIT

cat >"$WORK/telegram.py" <<'PYEOF'
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs

MODE = sys.argv[1]
PORT_FILE = sys.argv[2]
SENT_FILE = sys.argv[3]


class Telegram(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def _send(self, status, payload):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.endswith("/getMe"):
            if MODE == "bad-token":
                self._send(401, {"ok": False, "description": "Unauthorized"})
            else:
                self._send(200, {"ok": True, "result": {"username": "stub_bot"}})
            return
        self._send(404, {"ok": False})

    def do_POST(self):
        if not self.path.endswith("/sendMessage"):
            self._send(404, {"ok": False})
            return
        length = int(self.headers.get("Content-Length") or 0)
        fields = parse_qs(self.rfile.read(length).decode())
        with open(SENT_FILE, "w", encoding="utf-8") as fh:
            json.dump({k: v[0] for k, v in fields.items()}, fh)
        if MODE == "bad-chat":
            self._send(400, {"ok": False, "description": "chat not found"})
        elif MODE == "ok-false":
            # A 200 that is still a refusal. Telegram answers this way when the
            # bot was removed from the chat it is addressing.
            self._send(200, {"ok": False, "description": "bot was kicked"})
        else:
            self._send(200, {"ok": True, "result": {"message_id": 7}})


server = ThreadingHTTPServer(("127.0.0.1", 0), Telegram)
with open(PORT_FILE, "w", encoding="utf-8") as fh:
    fh.write(str(server.server_address[1]))
server.serve_forever()
PYEOF

# start_stub MODE stops the running stub and starts one in MODE, setting STUB_PID and STUB_PORT.
# Called as a plain command in this shell, so the PID it sets is the one the next start stops.
start_stub() {
  local mode="$1"
  stop_stub
  rm -f "$WORK/port" "$WORK/sent.json"
  python3 "$WORK/telegram.py" "$mode" "$WORK/port" "$WORK/sent.json" >/dev/null 2>>"$WORK/stub.err" &
  STUB_PID=$!
  local _
  for _ in $(seq 1 100); do
    if [ -s "$WORK/port" ]; then
      STUB_PORT="$(cat "$WORK/port")"
      return 0
    fi
    sleep 0.1
  done
  echo "stub Telegram never reported a port" >&2
  return 1
}

# Binds an ephemeral port and releases it, giving a port nothing listens on.
closed_port() {
  python3 - <<'PYEOF'
import socket

s = socket.socket()
s.bind(("127.0.0.1", 0))
port = s.getsockname()[1]
s.close()
print(port)
PYEOF
}

# Usage: run_alert OUT_FILE PORT MESSAGE; returns the send's exit status.
run_alert() {
  local out="$1" port="$2" message="$3"
  TELEGRAM_API_BASE="http://127.0.0.1:${port}" \
    TELEGRAM_BOT_TOKEN="stub-token" \
    TELEGRAM_CHAT_ID="-1001234567890" \
    bash "$ALERT" "$message" >"$out" 2>&1
}

sent_field() {
  jq -r --arg k "$1" '.[$k] // ""' "$WORK/sent.json" 2>/dev/null || printf ''
}

echo "telegram alert delivery:"

start_stub accept
PORT="$STUB_PORT"
OUT="$WORK/accept.out"
if run_alert "$OUT" "$PORT" "Load-test regression on dev"; then
  pass "an accepted message reports success"
else
  fail "an accepted message reports success (rc=$?, out=[$(cat "$OUT")])"
fi
if [ "$(sent_field chat_id)" = "-1001234567890" ]; then
  pass "the chat id reaches Telegram as the literal it was given"
else
  fail "the chat id reaches Telegram as the literal it was given (got [$(sent_field chat_id)])"
fi
if grep -qF "Load-test regression on dev" <<<"$(sent_field text)"; then
  pass "the message body is what the caller asked to send"
else
  fail "the message body is what the caller asked to send (got [$(sent_field text)])"
fi

start_stub bad-token
PORT="$STUB_PORT"
OUT="$WORK/bad-token.out"
if run_alert "$OUT" "$PORT" "anything"; then
  fail "a refused bot token fails the send"
else
  pass "a refused bot token fails the send"
fi
if grep -qF "::error::" "$OUT"; then
  pass "and says so as an annotation"
else
  fail "and says so as an annotation (out=[$(cat "$OUT")])"
fi

start_stub bad-chat
PORT="$STUB_PORT"
OUT="$WORK/bad-chat.out"
if run_alert "$OUT" "$PORT" "anything"; then
  fail "a refused chat fails the send"
else
  pass "a refused chat fails the send"
fi

start_stub ok-false
PORT="$STUB_PORT"
OUT="$WORK/ok-false.out"
if run_alert "$OUT" "$PORT" "anything"; then
  fail "a 200 carrying ok:false fails the send"
else
  pass "a 200 carrying ok:false fails the send"
fi

DEAD_PORT="$(closed_port)"
OUT="$WORK/dead.out"
if run_alert "$OUT" "$DEAD_PORT" "anything"; then
  fail "a send that reaches nothing fails"
else
  pass "a send that reaches nothing fails"
fi

start_stub accept
PORT="$STUB_PORT"
OUT="$WORK/no-token.out"
if TELEGRAM_API_BASE="http://127.0.0.1:${PORT}" \
  TELEGRAM_BOT_TOKEN="" \
  TELEGRAM_CHAT_ID="-1001234567890" \
  bash "$ALERT" "anything" >"$OUT" 2>&1; then
  fail "an absent bot token fails rather than reporting a quiet night"
else
  pass "an absent bot token fails rather than reporting a quiet night"
fi
OUT="$WORK/no-chat.out"
if TELEGRAM_API_BASE="http://127.0.0.1:${PORT}" \
  TELEGRAM_BOT_TOKEN="stub-token" \
  TELEGRAM_CHAT_ID="" \
  bash "$ALERT" "anything" >"$OUT" 2>&1; then
  fail "an absent chat id fails rather than reporting a quiet night"
else
  pass "an absent chat id fails rather than reporting a quiet night"
fi

OUT="$WORK/empty.out"
if run_alert "$OUT" "$PORT" ""; then
  fail "an empty message is refused rather than delivered"
else
  pass "an empty message is refused rather than delivered"
fi

start_stub accept
PORT="$STUB_PORT"
OUT="$WORK/long.out"
LONG="$(python3 -c 'print("drift " * 2000, end="")')"
if run_alert "$OUT" "$PORT" "$LONG"; then
  pass "a message past the ceiling is delivered"
else
  fail "a message past the ceiling is delivered (out=[$(cat "$OUT")])"
fi
SENT_LEN="$(python3 -c 'import json,sys; print(len(json.load(open(sys.argv[1]))["text"]))' "$WORK/sent.json" 2>/dev/null || echo 0)"
if [ "$SENT_LEN" -gt 0 ] && [ "$SENT_LEN" -le 4096 ]; then
  pass "and arrives inside Telegram's 4096-character ceiling (len=$SENT_LEN)"
else
  fail "and arrives inside Telegram's 4096-character ceiling (len=$SENT_LEN)"
fi

stop_stub
if pgrep -f "$WORK/telegram.py" >/dev/null; then
  fail "a stub Telegram this run started still holds its port: [$(pgrep -af "$WORK/telegram.py" || true)]"
else
  pass "every stub Telegram this run started is stopped"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n'
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f"; done
  exit 1
fi
