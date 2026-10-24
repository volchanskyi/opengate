#!/usr/bin/env bash
# Drives the smoke run against a sound stub edge, one serving the exposition and a silent one;
# only the first passes, and its console sign-in only with an HttpOnly cookie and CSRF refusal.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SMOKE="$REPO_ROOT/deploy/scripts/smoke-test.sh"

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

command -v python3 >/dev/null 2>&1 \
  || {
    echo "python3 is required to stand up the stub edge" >&2
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

# The stub mirrors the ingress: one catch-all rule sends every unrouted path to the SPA.

cat >"$WORK/edge.py" <<'PYEOF'
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MODE = sys.argv[1]
PORT_FILE = sys.argv[2]

SPA = b'<!doctype html>\n<html><head><title>web</title></head>\n<body><div id="root"></div></body></html>\n'
EXPOSITION = (
    b"# HELP opengate_http_requests_total Total HTTP requests.\n"
    b"# TYPE opengate_http_requests_total counter\n"
    b'opengate_http_requests_total{code="200"} 42\n'
)
# The same registry before it has answered a single request. A labelled counter
# publishes no sample until one of its label sets is incremented, so this is
# what the exposition looks like for the first moments of a server's life — and
# a check keyed on the request counter reads it as no exposition at all.
QUIET_EXPOSITION = (
    b"# HELP opengate_relay_active_sessions Number of active relay sessions.\n"
    b"# TYPE opengate_relay_active_sessions gauge\n"
    b"opengate_relay_active_sessions 0\n"
    b"# HELP go_goroutines Number of goroutines that currently exist.\n"
    b"# TYPE go_goroutines gauge\n"
    b"go_goroutines 24\n"
)
# The same breach in front of a server that has been running long enough to
# have a real registry. Size is the whole point of this one: the leaked series
# is on the first line, and everything after it is the hundreds of kilobytes a
# fleet's exposition actually runs to. A reader that stops at the match and
# leaves the rest of the body unread reports this edge as sealed.
EXPOSITION_LARGE = EXPOSITION + (
    b'go_gc_duration_seconds{quantile="0"} 0.0001 padding padding padding\n' * 8000
)
PROFILER = b"<html><body>Types of profiles available:<br>heap<br>goroutine</body></html>\n"


SESSION = {"live": False}


class Edge(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _signed_in(self):
        return SESSION["live"] and "og_session=stub-session" in (self.headers.get("Cookie") or "")

    def log_message(self, *args):
        pass

    def _send(self, status, body, ctype="text/html; charset=utf-8", cookie=None):
        self.send_response(status)
        if cookie:
            self.send_header("Set-Cookie", cookie)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path == "/api/v1/health":
            self._send(200, b'{"status":"ok"}', "application/json")
        elif path == "/api/v1/auth/session":
            if self._signed_in():
                self._send(200, b'{"csrf_token":"stub-csrf"}', "application/json")
            else:
                self._send(401, b'{"error":"missing authorization"}', "application/json")
        elif path == "/api/v1/sites":
            self._send(200, b"[]", "application/json")
        elif path == "/vite.svg":
            self._send(200, b"<svg xmlns='http://www.w3.org/2000/svg'></svg>", "image/svg+xml")
        elif path == "/metrics":
            # A breached edge routes the internal listener; a sound one has no
            # rule for the path and falls through to the SPA. "quiet" is a
            # breached edge in front of a server that has answered nothing yet.
            if MODE == "breached":
                self._send(200, EXPOSITION, "text/plain; charset=utf-8")
            elif MODE == "breached-large":
                self._send(200, EXPOSITION_LARGE, "text/plain; charset=utf-8")
            elif MODE == "quiet":
                self._send(200, QUIET_EXPOSITION, "text/plain; charset=utf-8")
            else:
                self._send(200, SPA, "text/html")
        elif path == "/debug/pprof/":
            self._send(
                200,
                PROFILER if MODE in ("breached", "breached-large", "quiet") else SPA,
            )
        else:
            self._send(200, SPA)

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        self.rfile.read(length)
        path = self.path.split("?", 1)[0]
        if path == "/api/v1/auth/register":
            self._send(201, b'{"token":"stub-bearer-value"}', "application/json")
        elif path == "/api/v1/auth/session":
            SESSION["live"] = True
            flags = "Path=/; SameSite=Strict" if MODE == "readable-cookie" else "Path=/; HttpOnly; SameSite=Strict"
            self._send(200, b'{"csrf_token":"stub-csrf"}', "application/json", f"og_session=stub-session; {flags}")
        else:
            self._send(200, SPA)

    def do_DELETE(self):
        if self.path.split("?", 1)[0] != "/api/v1/auth/session":
            self._send(200, SPA)
        elif not self._signed_in():
            self._send(401, b'{"error":"missing authorization"}', "application/json")
        elif self.headers.get("X-CSRF-Token") != "stub-csrf" and MODE != "no-csrf":
            self._send(403, b'{"error":"missing or wrong X-CSRF-Token"}', "application/json")
        else:
            SESSION["live"] = False
            self._send(204, b"", "application/json", "og_session=; Path=/; Max-Age=0; HttpOnly")


server = ThreadingHTTPServer(("127.0.0.1", 0), Edge)
with open(PORT_FILE, "w", encoding="utf-8") as fh:
    fh.write(str(server.server_address[1]))
server.serve_forever()
PYEOF

# start_stub MODE stops the running stub and starts one in MODE, setting STUB_PID and STUB_PORT.
# Called as a plain command in this shell, so the PID it sets is the one the next start stops.
start_stub() {
  local mode="$1"
  stop_stub
  rm -f "$WORK/port"
  python3 "$WORK/edge.py" "$mode" "$WORK/port" >/dev/null 2>>"$WORK/stub.err" &
  STUB_PID=$!
  for _ in $(seq 1 100); do
    if [ -s "$WORK/port" ]; then
      STUB_PORT="$(cat "$WORK/port")"
      return 0
    fi
    sleep 0.1
  done
  echo "stub edge never reported a port" >&2
  return 1
}

# The port is taken as the stub takes one, then released, so nothing listens on it.
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

# SMOKE_SUMMARY names the file the run treats as its job summary; unset, the run has none.
run_smoke() {
  local out="$1"
  shift
  GITHUB_STEP_SUMMARY="${SMOKE_SUMMARY:-}" bash "$SMOKE" "$@" >"$out" 2>&1
}

check_line() {
  grep -qF "$2: $3" "$1"
}

# summary_has FILE TEXT names TEXT as a line of the summary FILE.
summary_has() {
  [ -f "$1" ] && grep -qxF -- "$2" "$1"
}

echo "smoke test through the edge:"

start_stub sound
PORT="$STUB_PORT"
OUT="$WORK/sound.log"
if SMOKE_SUMMARY="$WORK/sound.md" run_smoke "$OUT" --domain edge.test --mode staging \
  --scheme http --edge-address "127.0.0.1:${PORT}"; then
  pass "a run through a sound edge passes"
else
  fail "a run through a sound edge must pass (see $OUT)"
  cat "$OUT" >&2
fi

if check_line "$OUT" PASS "GET /metrics through the ingress is not the exposition" \
  && check_line "$OUT" PASS "GET /debug/pprof/ through the ingress is not the profiler"; then
  pass "the boundary checks pass against an edge that keeps the boundary"
else
  fail "the boundary checks must pass against an edge that keeps the boundary"
fi

# The name resolves nowhere; only --edge-address can have reached the stub, so a
# passing health check is the flag working.
if check_line "$OUT" PASS "GET /api/v1/health returns 200"; then
  pass "--edge-address reaches an edge whose host has no public record"
else
  fail "--edge-address must reach the edge behind a name DNS cannot resolve"
fi

CONSOLE_CHECKS=(
  "GET /api/v1/auth/session without a cookie returns 401"
  "POST /api/v1/auth/session signs the console in with an HttpOnly cookie"
  "GET /api/v1/auth/session answers the cookie alone with 200"
  "DELETE /api/v1/auth/session without X-CSRF-Token is refused with 403"
  "DELETE /api/v1/auth/session with X-CSRF-Token signs the console out"
)
console_passed=yes
for console_check in "${CONSOLE_CHECKS[@]}"; do
  check_line "$OUT" PASS "$console_check" || console_passed=no
done
if [ "$console_passed" = yes ] \
  && check_line "$OUT" PASS "POST /api/v1/auth/register returns 201 and a script's bearer token" \
  && check_line "$OUT" PASS "GET /api/v1/sites with the script's bearer token returns 200"; then
  pass "the console signs in by cookie and out with its value, and a script by its bearer token"
else
  fail "a sound edge must pass every console sign-in check and both script sign-in checks (see $OUT)"
fi

if summary_has "$WORK/sound.md" "### Smoke tests: staging, through the public edge" \
  && summary_has "$WORK/sound.md" "**Result:** pass: every check passed." \
  && summary_has "$WORK/sound.md" "| GET /api/v1/health returns 200 | pass |" \
  && summary_has "$WORK/sound.md" "| GET /metrics through the ingress is not the exposition | pass |"; then
  pass "the job summary lists every check of a passing run"
else
  fail "the job summary must name the run and list every check as pass (see $WORK/sound.md)"
fi

start_stub breached
PORT="$STUB_PORT"
OUT="$WORK/breached.log"
if SMOKE_SUMMARY="$WORK/breached.md" run_smoke "$OUT" --domain edge.test --mode staging \
  --scheme http --edge-address "127.0.0.1:${PORT}"; then
  fail "a run through an edge that serves the exposition must not pass"
else
  pass "a run through an edge that serves the exposition fails"
fi

if summary_has "$WORK/breached.md" "**Result:** FAIL: 2 of 14 checks failed." \
  && summary_has "$WORK/breached.md" "| GET /metrics through the ingress is not the exposition | FAIL |" \
  && summary_has "$WORK/breached.md" "| GET /api/v1/health returns 200 | pass |"; then
  pass "the job summary names the checks a failing run failed"
else
  fail "the job summary must count and mark the failed checks (see $WORK/breached.md)"
fi

if check_line "$OUT" FAIL "GET /metrics through the ingress is not the exposition"; then
  pass "the exposition on the edge is reported as a failure"
else
  fail "an edge serving the exposition must fail its boundary check"
fi

if check_line "$OUT" FAIL "GET /debug/pprof/ through the ingress is not the profiler"; then
  pass "the profiler on the edge is reported as a failure"
else
  fail "an edge serving the profiler must fail its boundary check"
fi

# A cookie a page script can read, or a change taken without the session's value, fails the run.

start_stub readable-cookie
PORT="$STUB_PORT"
OUT="$WORK/readable-cookie.log"
if run_smoke "$OUT" --domain edge.test --mode staging --scheme http --edge-address "127.0.0.1:${PORT}"; then
  fail "a sign-in cookie a page script could read must fail the run"
elif check_line "$OUT" FAIL "POST /api/v1/auth/session signs the console in with an HttpOnly cookie"; then
  pass "a sign-in cookie a page script could read fails the console sign-in check"
else
  fail "a sign-in cookie without HttpOnly must be named by the sign-in check (see $OUT)"
fi

start_stub no-csrf
PORT="$STUB_PORT"
OUT="$WORK/no-csrf.log"
if run_smoke "$OUT" --domain edge.test --mode staging --scheme http --edge-address "127.0.0.1:${PORT}"; then
  fail "a server that takes a change without the session's value must fail the run"
elif check_line "$OUT" FAIL "DELETE /api/v1/auth/session without X-CSRF-Token is refused with 403"; then
  pass "a change taken without the session's value fails the refusal check"
else
  fail "a change taken without X-CSRF-Token must be named by the refusal check (see $OUT)"
fi

# The verdict holds for a large exposition, where a reader that stops at the match loses it.

start_stub breached-large
PORT="$STUB_PORT"
OUT="$WORK/breached-large.log"
if run_smoke "$OUT" --domain edge.test --mode staging \
  --scheme http --edge-address "127.0.0.1:${PORT}"; then
  fail "a run through an edge serving a full-size exposition must not pass"
else
  pass "a run through an edge serving a full-size exposition fails"
fi

if check_line "$OUT" FAIL "GET /metrics through the ingress is not the exposition"; then
  pass "a full-size exposition on the edge is reported as a failure"
else
  fail "an edge serving a full-size exposition must fail its boundary check"
fi

# Nothing listens, so every request comes back empty and an absence-shaped check must not pass.

stop_stub
DEAD_PORT="$(closed_port)"
OUT="$WORK/refused.log"
if run_smoke "$OUT" --domain edge.test --mode staging \
  --scheme http --edge-address "127.0.0.1:${DEAD_PORT}"; then
  fail "a run that reached no edge at all must not pass"
else
  pass "a run that reached no edge at all fails"
fi

if check_line "$OUT" PASS "GET /metrics through the ingress is not the exposition"; then
  fail "the exposition boundary reported a pass on a request nothing answered"
else
  pass "the exposition boundary does not pass on a request nothing answered"
fi

if check_line "$OUT" PASS "GET /debug/pprof/ through the ingress is not the profiler"; then
  fail "the profiler boundary reported a pass on a request nothing answered"
else
  pass "the profiler boundary does not pass on a request nothing answered"
fi

if check_line "$OUT" PASS "GET /ws/relay route exists (non-404)"; then
  fail "the relay route reported a pass on a request nothing answered"
else
  pass "the relay route does not pass on a request nothing answered"
fi

# A domain run told nothing asks for https, which the plain-HTTP stub cannot serve.

start_stub sound
PORT="$STUB_PORT"
OUT="$WORK/scheme.log"
if run_smoke "$OUT" --domain edge.test --mode staging \
  --edge-address "127.0.0.1:${PORT}"; then
  fail "a domain run with no --scheme must ask for https, and must not reach a plain-HTTP edge"
else
  pass "a domain run with no --scheme asks for https"
fi

if check_line "$OUT" PASS "GET /metrics through the ingress is not the exposition"; then
  fail "the exposition boundary reported a pass on a handshake that failed"
else
  pass "the exposition boundary does not pass on a handshake that failed"
fi

OUT="$WORK/misuse.log"
if run_smoke "$OUT" --host 127.0.0.1 --port 1 --metrics-port 2 \
  --mode local --edge-address 127.0.0.1; then
  fail "--edge-address without --domain must be refused"
else
  if grep -qF -- "--edge-address names the edge for --domain" "$OUT"; then
    pass "--edge-address without --domain is refused by name"
  else
    fail "--edge-address without --domain must say why it was refused"
  fi
fi

# Through the edge an exposition on /metrics is the breach; on the forwarded internal port it is
# expected, so the --host run passes on responses the --domain run fails.

start_stub breached
PORT="$STUB_PORT"
OUT="$WORK/forwarded.log"
if run_smoke "$OUT" --host 127.0.0.1 --port "$PORT" --metrics-port "$PORT" \
  --mode local --scheme http; then
  pass "a run against the forwarded listener passes"
else
  fail "a run against the forwarded listener must pass (see $OUT)"
  cat "$OUT" >&2
fi

if check_line "$OUT" PASS "GET /metrics returns Prometheus metrics" \
  && check_line "$OUT" PASS "GET /debug/pprof/ returns the profiler index"; then
  pass "the forwarded run reads the exposition and the profiler off the listener"
else
  fail "the forwarded run must read the exposition and the profiler off the listener"
fi

# A fresh server's exposition carries gauges and no request counters, and both runs must
# still recognise it.

start_stub quiet
PORT="$STUB_PORT"
OUT="$WORK/quiet-forwarded.log"
if run_smoke "$OUT" --host 127.0.0.1 --port "$PORT" --metrics-port "$PORT" \
  --mode local --scheme http; then
  pass "the forwarded run passes against a server that has answered nothing yet"
else
  fail "the forwarded run must pass against a server that has answered nothing yet (see $OUT)"
  cat "$OUT" >&2
fi

if check_line "$OUT" PASS "GET /metrics returns Prometheus metrics"; then
  pass "the exposition is recognised before any request has been counted"
else
  fail "the exposition must be recognised before any request has been counted"
fi

OUT="$WORK/quiet-edge.log"
if run_smoke "$OUT" --domain edge.test --mode staging \
  --scheme http --edge-address "127.0.0.1:${PORT}"; then
  fail "an edge serving a freshly started server's exposition must not pass"
else
  pass "an edge serving a freshly started server's exposition fails"
fi

if check_line "$OUT" FAIL "GET /metrics through the ingress is not the exposition"; then
  pass "the boundary catches an exposition with no request counter in it"
else
  fail "the boundary must catch an exposition with no request counter in it"
fi

stop_stub
if pgrep -f "$WORK/edge.py" >/dev/null; then
  fail "a stub edge this run started still holds its port: [$(pgrep -af "$WORK/edge.py" || true)]"
else
  pass "every stub edge this run started is stopped"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
